package server

import (
	"context"
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/grandcat/zeroconf"
	"github.com/spf13/viper"
)

func init() {
	gob.Register(FileMetadata{})
	gob.Register(SyncMessage{})
}

type Connection struct {
	conn    net.Conn
	encoder *gob.Encoder
	decoder *gob.Decoder
}

type FileMetadata struct {
	Path      string    `json:"path"`
	Size      int64     `json:"size"`
	ModTime   time.Time `json:"mod_time"`
	CheckSum  string    `json:"checksum"`
	IsDeleted bool      `json:"is_deleted"`
}

type SyncMessage struct {
	Type     string       `json:"type"`
	DeviceID string       `json:"device_id"`
	Metadata FileMetadata `json:"metadata"`
	Data     []byte       `json:"data,omitempty"`
}

type SyncServer struct {
	deviceID     string
	port         int
	syncDir      string
	peers        map[string]string
	metadata     map[string]FileMetadata
	connections  map[string]*Connection
	metadataLock sync.RWMutex
	connLock     sync.RWMutex
	watcher      *fsnotify.Watcher
}

func NewSyncServer(deviceID string, port int, syncDir string) *SyncServer {
	return &SyncServer{
		deviceID:    deviceID,
		port:        port,
		syncDir:     syncDir,
		peers:       make(map[string]string),
		metadata:    make(map[string]FileMetadata),
		connections: make(map[string]*Connection),
	}
}

func (s *SyncServer) Start(ctx context.Context) error {
	var err error
	s.watcher, err = fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create file watcher: %w", err)
	}
	defer s.watcher.Close()

	go s.watchFileChanges()

	// Add the sync directory and all subdirectories to the watcher
	if err := s.addDirsToWatcher(s.syncDir); err != nil {
		return fmt.Errorf("failed to add directories to watcher: %w", err)
	}

	if err := s.initializeMetadata(); err != nil {
		return fmt.Errorf("failed to initialize metadata: %w", err)
	}

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", s.port))
	if err != nil {
		return fmt.Errorf("failed to start TCP server: %w", err)
	}
	defer listener.Close()

	log.Printf("Server listening on port %d", s.port)

	go s.connectToPeers(ctx)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Failed to accept connection: %v", err)
			continue
		}

		// TODO: Why in windows happends connection from 50xxx ports?
		// Extract the IP address from the remote address and local address
		remoteAddr, _, _ := net.SplitHostPort(conn.RemoteAddr().String())
		localAddr, _, _ := net.SplitHostPort(conn.LocalAddr().String())

		// Check if the connection is from the same host by comparing IP addresses
		if remoteAddr == localAddr {
			// Skip connection from the same host
			log.Printf("Rejected connection from the same host: %s", conn.RemoteAddr().String())
			conn.Close()
			continue
		}

		fmt.Printf("YEP I ACCEPT CONNECTION: %s\n", conn.RemoteAddr().String())

		go s.handleConnection(conn)
	}
}

// Add directories recursively to the watcher, respecting ignore patterns
func (s *SyncServer) addDirsToWatcher(root string) error {
	// Get ignore patterns from config
	ignorePatterns := viper.GetStringSlice("ignore_patterns")

	// Walk the directory tree
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip the path if it should be ignored
		if shouldIgnoreFile(path, ignorePatterns) {
			if info.IsDir() {
				log.Printf("Ignoring directory: %s", path)
				return filepath.SkipDir
			}
			log.Printf("Ignoring file: %s", path)
			return nil
		}

		// Add directory to watcher
		if info.IsDir() {
			if err := s.watcher.Add(path); err != nil {
				return fmt.Errorf("failed to watch directory %s: %w", path, err)
			}
			log.Printf("Watching directory: %s", path)
		}
		return nil
	})
}

// Update watchFileChanges to also check ignore patterns
func (s *SyncServer) watchFileChanges() {
	ignorePatterns := viper.GetStringSlice("ignore_patterns")

	for {
		select {
		case event, ok := <-s.watcher.Events:
			if !ok {
				return
			}

			if shouldIgnoreFile(event.Name, ignorePatterns) {
				log.Printf("Ignoring event for: %s", event.Name)
				continue
			}

			relPath, err := filepath.Rel(s.syncDir, event.Name)
			if err != nil {
				log.Printf("Failed to get relative path for %s: %v", event.Name, err)
				continue
			}

			// Normalize path to use forward slashes for consistency across platforms
			relPath = filepath.ToSlash(relPath)

			log.Printf("File event: %s %s", event.Op.String(), relPath)

			// Handle each type of file event
			switch {
			case event.Op&fsnotify.Create == fsnotify.Create:
				// If it's a directory, add it to the watcher (if not ignored)
				info, err := os.Stat(event.Name)
				if err == nil && info.IsDir() {
					if !shouldIgnoreFile(event.Name, ignorePatterns) {
						s.watcher.Add(event.Name)
						log.Printf("Added new directory to watch: %s", event.Name)
					} else {
						log.Printf("Ignoring new directory: %s", event.Name)
					}
				} else if err == nil {
					// It's a file, update metadata
					s.updateFileMetadata(relPath, false)
				}

			case event.Op&fsnotify.Write == fsnotify.Write:
				// Update metadata for modified files
				s.updateFileMetadata(relPath, false)

			case event.Op&fsnotify.Remove == fsnotify.Remove,
				event.Op&fsnotify.Rename == fsnotify.Rename:
				// Mark file as deleted in metadata
				s.metadataLock.Lock()
				if meta, exists := s.metadata[relPath]; exists {
					meta.IsDeleted = true
					s.metadata[relPath] = meta
					log.Printf("Marked file as deleted: %s", relPath)
					// Broadcast delete to peers
					s.broadcastMetadata(meta)
				}
				s.metadataLock.Unlock()
			}

		case err, ok := <-s.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("File watcher error: %v", err)
		}
	}
}

// Helper function to check if a file should be ignored
func shouldIgnoreFile(path string, ignorePatterns []string) bool {
	// Get base name for matching against patterns
	base := filepath.Base(path)

	for _, pattern := range ignorePatterns {
		matched, err := filepath.Match(pattern, base)
		if err != nil {
			log.Printf("Invalid pattern %s: %v", pattern, err)
			continue
		}

		if matched {
			return true
		}

		// Also check for directory matches (for directories like node_modules)
		if strings.Contains(path, string(filepath.Separator)+pattern+string(filepath.Separator)) ||
			strings.HasSuffix(path, string(filepath.Separator)+pattern) {
			return true
		}
	}
	return false
}

// Update file metadata and broadcast to peers
func (s *SyncServer) updateFileMetadata(relPath string, isDeleted bool) {
	fullPath := filepath.Join(s.syncDir, relPath)
	info, err := os.Stat(fullPath)
	if err != nil {
		log.Printf("Failed to get file info for %s: %v", fullPath, err)
		return
	}

	if !info.IsDir() {
		checksum, err := calculateChecksum(fullPath)
		if err != nil {
			log.Printf("Failed to calculate checksum for %s: %v", fullPath, err)
			return
		}

		metadata := FileMetadata{
			Path:      relPath,
			Size:      info.Size(),
			ModTime:   info.ModTime(),
			CheckSum:  checksum,
			IsDeleted: isDeleted,
		}

		// Update local metadata
		s.metadataLock.Lock()
		s.metadata[relPath] = metadata
		s.metadataLock.Unlock()

		log.Printf("Updated metadata for %s", relPath)

		// Broadcast metadata update to peers
		s.broadcastMetadata(metadata)
	}
}

// Update initializeMetadata to also respect ignore patterns
func (s *SyncServer) initializeMetadata() error {
	// Get ignore patterns from config
	ignorePatterns := viper.GetStringSlice("ignore_patterns")

	// Create a file matcher function
	shouldIgnore := func(path string) bool {
		// Get base name for matching against patterns
		base := filepath.Base(path)

		// Check if the path matches any ignore pattern
		for _, pattern := range ignorePatterns {
			matched, err := filepath.Match(pattern, base)
			if err != nil {
				log.Printf("Invalid pattern %s: %v", pattern, err)
				continue
			}

			if matched {
				return true
			}

			// Also check for directory matches (for directories like node_modules)
			if strings.Contains(path, string(filepath.Separator)+pattern+string(filepath.Separator)) ||
				strings.HasSuffix(path, string(filepath.Separator)+pattern) {
				return true
			}
		}
		return false
	}

	return filepath.Walk(s.syncDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip the path if it should be ignored
		if shouldIgnore(path) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if !info.IsDir() {
			relPath, err := filepath.Rel(s.syncDir, path)
			if err != nil {
				return err
			}

			// Normalize path to use forward slashes for consistency across platforms
			relPath = filepath.ToSlash(relPath)

			checksum, err := calculateChecksum(path)
			if err != nil {
				return err
			}

			s.metadataLock.Lock()
			s.metadata[relPath] = FileMetadata{
				Path:     relPath,
				Size:     info.Size(),
				ModTime:  info.ModTime(),
				CheckSum: checksum,
			}
			s.metadataLock.Unlock()
		}
		return nil
	})
}

func (s *SyncServer) connectToPeers(ctx context.Context) {
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		log.Fatalf("Failed to initialize mDNS resolver: %v", err)
	}

	entries := make(chan *zeroconf.ServiceEntry)
	go s.discoverPeers(entries)

	// Start browsing for peers using mDNS
	if err := resolver.Browse(ctx, "_peer._tcp", "local.", entries); err != nil {
		log.Fatalf("Failed to browse mDNS services: %v", err)
	}

	// Monitor peers and attempt connections periodically
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("Stopping peer discovery")
			return
		case <-ticker.C:
			s.connectToDiscoveredPeers()
		}
	}
}

// discoverPeers listens for mDNS service entries and updates the peer list.
func (s *SyncServer) discoverPeers(entries chan *zeroconf.ServiceEntry) {
	for entry := range entries {
		if len(entry.AddrIPv4) == 0 {
			continue
		}

		fmt.Println("entry", entry)

		peerID := entry.Instance

		// Ignore self-discovery
		if peerID == s.deviceID {
			log.Printf("Ignoring self: %s", peerID)
			continue
		}

		address := net.JoinHostPort(entry.AddrIPv4[0].String(), fmt.Sprintf("%d", entry.Port))
		log.Printf("Discovered Peer: %s at %s", peerID, address)

		s.connLock.Lock()
		if _, exists := s.peers[peerID]; !exists {
			s.peers[peerID] = address
		}
		s.connLock.Unlock()
	}
}

// connectToDiscoveredPeers iterates over discovered peers and attempts connections.
func (s *SyncServer) connectToDiscoveredPeers() {
	s.connLock.RLock()
	peers := s.peers
	s.connLock.RUnlock()

	for peerID, addr := range peers {
		go s.establishConnection(peerID, addr)
	}
}

// establishConnection checks for existing connections and attempts to connect to a peer.
func (s *SyncServer) establishConnection(peerID, address string) {
	s.connLock.RLock()
	if _, exists := s.connections[peerID]; exists {
		s.connLock.RUnlock()
		return
	}
	s.connLock.RUnlock()

	conn, err := net.DialTimeout("tcp", address, 10*time.Second)
	if err != nil {
		log.Printf("Failed to connect to peer %s (%s): %v", peerID, address, err)
		return
	}

	log.Printf("Connected to peer %s at %s", peerID, address)
	s.handleConnection(conn)
}

func (s *SyncServer) handleConnection(conn net.Conn) {
	defer conn.Close()

	decoder := gob.NewDecoder(conn)
	encoder := gob.NewEncoder(conn)

	// Send initial handshake
	handshake := SyncMessage{
		Type:     "handshake",
		DeviceID: s.deviceID,
	}
	log.Println("Sending handshake")
	if err := encoder.Encode(handshake); err != nil {
		log.Printf("Handshake failed: %v", err)
		return
	}

	// Wait for handshake response to get peer's device ID
	var handshakeResponse SyncMessage
	log.Println("Waiting for handshake response")
	if err := decoder.Decode(&handshakeResponse); err != nil {
		log.Printf("Handshake response failed: %v", err)
		return
	}

	if handshakeResponse.Type != "handshake" {
		log.Printf("Expected handshake, got %s", handshakeResponse.Type)
		return
	}

	peerID := handshakeResponse.DeviceID
	log.Printf("Handshake successful with peer: %s", peerID)

	// Check if we already have a connection to this peer
	var duplicateConnection bool
	s.connLock.Lock()
	if _, exists := s.connections[peerID]; exists {
		duplicateConnection = true
	} else {
		s.connections[peerID] = &Connection{
			conn:    conn,
			encoder: encoder,
			decoder: decoder,
		}
		log.Printf("Established connection with peer: %s", peerID)
	}
	s.connLock.Unlock()

	// Handle duplicate connection case
	if duplicateConnection {
		log.Printf("Duplicate connection to peer %s, closing new connection", peerID)
		return
	}

	// Send initial metadata after successful handshake
	log.Println("Sending initial metadata")
	s.sendInitialMetadata(peerID)

	// Handle incoming messages
	for {
		var msg SyncMessage
		// log.Println("Waiting for incoming message")
		if err := decoder.Decode(&msg); err != nil {
			if err.Error() != "EOF" {
				log.Printf("Error decoding message from %s: %v", peerID, err)
			}
			break
		}

		switch msg.Type {
		case "metadata":
			s.handleMetadataUpdate(msg)
		case "file_request":
			s.handleFileRequest(msg)
		case "file_data":
			s.handleFileData(msg)
		default:
			log.Printf("Unknown message type from %s: %s", peerID, msg.Type)
		}
	}

	// Remove connection when the loop exits
	s.connLock.Lock()
	delete(s.connections, peerID)
	s.connLock.Unlock()
	log.Printf("Connection closed with peer: %s", peerID)
}

func (s *SyncServer) handleMetadataUpdate(msg SyncMessage) {
	s.metadataLock.Lock()
	defer s.metadataLock.Unlock()

	localMeta, exists := s.metadata[msg.Metadata.Path]
	if !exists || localMeta.ModTime.Before(msg.Metadata.ModTime) {
		if msg.Metadata.IsDeleted {
			delete(s.metadata, msg.Metadata.Path)
			os.Remove(filepath.Join(s.syncDir, msg.Metadata.Path))
		} else {
			s.requestFile(msg.DeviceID, msg.Metadata)
		}
	}
}

func (s *SyncServer) handleFileRequest(msg SyncMessage) {
	s.connLock.RLock()
	connection, exists := s.connections[msg.DeviceID]
	s.connLock.RUnlock()

	if !exists {
		log.Printf("No connection to device %s", msg.DeviceID)
		return
	}

	path := filepath.Join(s.syncDir, msg.Metadata.Path)
	data, err := os.ReadFile(path)
	if err != nil {
		log.Printf("Failed to read file %s: %v", path, err)
		return
	}

	resposne := SyncMessage{
		Type:     "file_data",
		DeviceID: s.deviceID,
		Metadata: msg.Metadata,
		Data:     data,
	}

	if err := connection.encoder.Encode(resposne); err != nil {
		log.Printf("Failed to send file data: %v", err)
	}
}

func (s *SyncServer) handleFileData(msg SyncMessage) {
	path := filepath.Join(s.syncDir, msg.Metadata.Path)

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		log.Printf("Failed to create directory for %s: %v", path, err)
		return
	}

	match, err := s.verifyChecksum(path, msg.Metadata.CheckSum)
	if err == nil && match {
		// File already exists and matches checksum, no need to write
		return
	}

	// sanitizedPath := sanitizeFilename(path)
	if err := os.WriteFile(path, msg.Data, 0644); err != nil {
		log.Printf("Failed to write file %s: %v", path, err)
		return
	}

	match, err = s.verifyChecksum(path, msg.Metadata.CheckSum)
	if err != nil || !match {
		log.Printf("Checksum mismatch for file %s", msg.Metadata.Path)
		os.Remove(path)
		s.requestFile(msg.DeviceID, msg.Metadata)
		return
	}

	s.metadataLock.Lock()
	s.metadata[msg.Metadata.Path] = msg.Metadata
	s.metadataLock.Unlock()

	// We only broadcast metadata if WE made the change
	// Avoids endless synchronization loops
	if msg.DeviceID == s.deviceID {
		s.broadcastMetadata(msg.Metadata)
	}
}

func sanitizeFilename(filename string) string {
	invalidChars := []string{"?", ":", "*", "<", ">", "|", "\"", "\\", ";"}
	for _, char := range invalidChars {
		filename = strings.ReplaceAll(filename, char, "_") // Replace invalid characters with underscores
	}
	filename = strings.ReplaceAll(filename, " ", "_") // Replace spaces with underscores
	return filename
}

func (s *SyncServer) requestFile(deviceID string, metadata FileMetadata) {
	s.connLock.RLock()
	connection, exists := s.connections[deviceID]
	s.connLock.RUnlock()

	if !exists {
		log.Printf("No connection to device %s", deviceID)
		return
	}

	request := SyncMessage{
		Type:     "file_request",
		DeviceID: s.deviceID,
		Metadata: metadata,
	}

	if err := connection.encoder.Encode(request); err != nil {
		log.Printf("Failed to  send file request: %v", err)
	}
}

func (s *SyncServer) sendInitialMetadata(peerID string) {
	s.connLock.RLock()
	connection, exists := s.connections[peerID]
	s.connLock.RUnlock()

	if !exists {
		log.Printf("No connection to device %s", peerID)
		return
	}

	s.metadataLock.RLock()
	defer s.metadataLock.RUnlock()

	for _, metadata := range s.metadata {
		msg := SyncMessage{
			Type:     "metadata",
			DeviceID: s.deviceID,
			Metadata: metadata,
		}
		if err := connection.encoder.Encode(msg); err != nil {
			log.Printf("Failed to send initial metadata: %v", err)
			return
		}
	}
}

func (s *SyncServer) broadcastMetadata(metadata FileMetadata) {
	msg := SyncMessage{
		Type:     "metadata",
		DeviceID: s.deviceID,
		Metadata: metadata,
	}

	s.connLock.RLock()
	defer s.connLock.RUnlock()

	for _, connection := range s.connections {
		if err := connection.encoder.Encode(msg); err != nil {
			log.Printf("Failed to broadcast metadata: %v", err)
		}
	}
}

func calculateChecksum(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	hash := sha256.New()
	buf := make([]byte, 1024*1024)

	for {
		n, err := file.Read(buf)
		if n > 0 {
			if _, err := hash.Write(buf[:n]); err != nil {
				return "", fmt.Errorf("failed to write to hash: %w", err)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("failed to read file: %w", err)
		}
	}

	checksum := hex.EncodeToString(hash.Sum(nil))
	return checksum, nil
}

func (s *SyncServer) verifyChecksum(path string, expectedChecksum string) (bool, error) {
	actualChecksum, err := calculateChecksum(path)
	if err != nil {
		return false, err
	}
	return actualChecksum == expectedChecksum, nil
}

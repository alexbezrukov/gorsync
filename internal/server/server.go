package server

import (
	"context"
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"gorsync/internal/client"
	"gorsync/internal/memstore"
	"gorsync/internal/model"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
)

func init() {
	gob.Register(model.FileMetadata{})
	gob.Register(model.SyncMessage{})
}

type SyncServer struct {
	deviceID     string
	port         int
	syncDir      string
	metadata     map[string]model.FileMetadata
	connections  map[string]*Connection
	metadataLock sync.RWMutex
	connLock     sync.RWMutex
	watcher      *fsnotify.Watcher
	// discovery       *discovery.Discovery
	listener net.Listener
	memstore *memstore.MemStore
	client   *client.WebSocketClient
}

type Connection struct {
	conn    net.Conn
	encoder *gob.Encoder
	decoder *gob.Decoder
}

func NewSyncServer(deviceID string, port int, syncDir string, relayServerAddr string) *SyncServer {
	return &SyncServer{
		deviceID:    deviceID,
		port:        port,
		syncDir:     syncDir,
		metadata:    make(map[string]model.FileMetadata),
		connections: make(map[string]*Connection),
		// discovery:       discovery,
		memstore: memstore.NewMemStore(),
		client:   client.NewWebSocketClient(relayServerAddr),
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

	if err := s.connectToRelayServer(ctx); err != nil {
		return fmt.Errorf("failed to connect to relay server: %w", err)
	}

	s.listener, err = net.Listen("tcp", fmt.Sprintf(":%d", s.port))
	if err != nil {
		return fmt.Errorf("failed to start TCP server: %w", err)
	}
	defer s.listener.Close()

	log.Printf("Sync server listening on port %d", s.port)

	go s.connectToPeers(ctx)

	for {
		select {
		case <-ctx.Done():
			log.Println("Shutting down sync server...")
			return nil
		default:
			conn, err := s.listener.Accept()
			if err != nil {
				if ctx.Err() != nil {
					return nil // Graceful shutdown
				}
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
}

func (s *SyncServer) Stop() {
	if s.watcher != nil {
		s.watcher.Close()
	}

	if s.listener != nil {
		s.listener.Close()
	}

	log.Println("Sync server stopped")
}

func (s *SyncServer) connectToRelayServer(ctx context.Context) error {
	s.client.Connect()

	go s.client.HandleMessages(ctx, s.processMessage)

	if err := s.getPeers(); err != nil {
		return fmt.Errorf("failed to get peers from relay: %w", err)
	}

	return nil
}

// registerWithRelay registers this device with the relay server
func (s *SyncServer) getPeers() error {
	registerMsg := model.Message{
		Type:      model.MsgTypePeerRequest,
		DeviceID:  s.deviceID,
		Timestamp: time.Now(),
	}

	return s.client.SendMessage(registerMsg)
}

func (s *SyncServer) processMessage(msg model.Message) {
	switch msg.Type {
	case model.MsgTypePeerRequest:
		var peers []*model.Device

		if err := json.Unmarshal(msg.Payload, &peers); err != nil {
			log.Printf("Error parsing peer list: %v", err)
			return
		}

		fmt.Println("peers", peers)

		for _, peer := range peers {
			s.memstore.SaveDevice(peer)
		}
	}
}

func (s *SyncServer) connectToPeers(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("Stopping peer discovery")
			return
		case <-ticker.C:
			devices := s.memstore.GetDevices()

			s.connLock.Lock()

			for _, device := range devices {
				peerID := device.ID

				if peerID == s.deviceID {
					continue
				}

				address := fmt.Sprintf("%s:%d", device.Address, device.Port)
				for _, peer := range devices {
					// If peer is new, log and connect
					if peer.Address != address {
						log.Printf("Discovered peer: %s at %s", peerID, address)
						peer.Address = address
						go s.establishConnection(peerID, address)
					}
				}

			}

			s.connLock.Unlock()
		}
	}
}

// establishConnection checks for existing connections and attempts to connect to a peer.
func (s *SyncServer) establishConnection(peerID, address string) bool {
	// Check if connection already exists
	s.connLock.RLock()
	if _, exists := s.connections[peerID]; exists {
		s.connLock.RUnlock()
		return true // Connection already exists, considered successful
	}
	s.connLock.RUnlock()

	// Attempt connection
	conn, err := net.DialTimeout("tcp", address, 10*time.Second)
	if err != nil {
		log.Printf("Failed to connect to peer %s (%s): %v", peerID, address, err)
		return false
	}

	log.Printf("Connected to peer %s at %s", peerID, address)

	go s.handleConnection(conn)

	return true
}

func (s *SyncServer) handleConnection(conn net.Conn) {
	defer conn.Close()

	encoder := gob.NewEncoder(conn)
	decoder := gob.NewDecoder(conn)

	// Send handshake first
	handshakeMsg := model.SyncMessage{
		Type:     "handshake",
		DeviceID: s.deviceID,
	}
	if err := encoder.Encode(handshakeMsg); err != nil {
		log.Printf("Failed to send handshake: %v", err)
		return
	}

	// Wait for peer's handshake response
	var response model.SyncMessage
	if err := decoder.Decode(&response); err != nil {
		log.Printf("Failed to receive handshake response: %v", err)
		return
	}

	if response.Type != "handshake" {
		log.Printf("Expected handshake, got %s", response.Type)
		return
	}

	peerID := response.DeviceID
	log.Printf("Handshake successful with peer: %s", peerID)

	// Store the connection
	s.connLock.Lock()
	s.connections[peerID] = &Connection{
		conn:    conn,
		encoder: encoder,
		decoder: decoder,
	}
	s.connLock.Unlock()

	// Send initial metadata
	log.Println("Sending initial metadata to", peerID)
	s.sendInitialMetadata(peerID)

	// Handle incoming messages
	for {
		var msg model.SyncMessage
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

func (s *SyncServer) handleMetadataUpdate(msg model.SyncMessage) {
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

			// log.Printf("File event: %s %s", event.Op.String(), relPath)

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

		metadata := model.FileMetadata{
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

		// log.Printf("Updated metadata for %s", relPath)

		// Broadcast metadata update to peers
		s.broadcastMetadata(metadata)
	}
}

// Update initializeMetadata to also respect ignore patterns
func (s *SyncServer) initializeMetadata() error {
	// Get ignore patterns from config
	ignorePatterns := viper.GetStringSlice("ignore_patterns")

	return filepath.Walk(s.syncDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip the path if it should be ignored
		if shouldIgnoreFile(path, ignorePatterns) {
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
			s.metadata[relPath] = model.FileMetadata{
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

func (s *SyncServer) handleFileRequest(msg model.SyncMessage) {
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

	response := model.SyncMessage{
		Type:     "file_data",
		DeviceID: s.deviceID,
		Metadata: msg.Metadata,
		Data:     data,
	}

	if err := connection.encoder.Encode(response); err != nil {
		log.Printf("Failed to send file data: %v", err)
	}
}

func (s *SyncServer) handleFileData(msg model.SyncMessage) {
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

	// match, err = s.verifyChecksum(path, msg.Metadata.CheckSum)
	// if err != nil || !match {
	// 	log.Printf("Checksum mismatch for file %s", msg.Metadata.Path)
	// 	os.Remove(path)
	// 	s.requestFile(msg.DeviceID, msg.Metadata)
	// 	return
	// }

	s.metadataLock.Lock()
	s.metadata[msg.Metadata.Path] = msg.Metadata
	s.metadataLock.Unlock()

	// We only broadcast metadata if WE made the change
	// Avoids endless synchronization loops
	if msg.DeviceID == s.deviceID {
		s.broadcastMetadata(msg.Metadata)
	}
}

func (s *SyncServer) requestFile(deviceID string, metadata model.FileMetadata) {
	s.connLock.RLock()
	connection, exists := s.connections[deviceID]
	s.connLock.RUnlock()

	if !exists {
		log.Printf("No connection to device %s", deviceID)
		return
	}

	request := model.SyncMessage{
		Type:     "file_request",
		DeviceID: s.deviceID,
		Metadata: metadata,
	}

	if err := connection.encoder.Encode(request); err != nil {
		log.Printf("Failed to  send file request: %v", err)
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
		msg := model.SyncMessage{
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

func (s *SyncServer) broadcastMetadata(metadata model.FileMetadata) {
	msg := model.SyncMessage{
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

// func (s *SyncServer) connectToPeers(ctx context.Context) {
// 	ticker := time.NewTicker(3 * time.Second)
// 	defer ticker.Stop()

// 	// Track connection attempts to avoid duplicate attempts
// 	connectionAttempts := make(map[string]time.Time)
// 	connectionTimeout := 30 * time.Second

// 	for {
// 		select {
// 		case <-ctx.Done():
// 			log.Println("Stopping peer discovery")
// 			return
// 		case <-ticker.C:
// 			services := s.discovery.GetServicesByName("file-syncer")

// 			// Create a set of current peers for detecting disconnections
// 			activePeers := make(map[string]bool)

// 			s.connLock.Lock()

// 			// Process discovered peers
// 			for _, service := range services {
// 				peerID := service.ID

// 				// Mark as active
// 				activePeers[peerID] = true

// 				// Ignore self-discovery
// 				if peerID == s.deviceID {
// 					continue
// 				}

// 				address := fmt.Sprintf("%s:%d", service.Address, service.Port)

// 				// Check if it's a new peer or address has changed
// 				currentAddr, exists := s.peers[peerID]
// 				if !exists || currentAddr != address {
// 					var peerStatus string
// 					if exists {
// 						peerStatus = "updated"
// 					} else {
// 						peerStatus = "new"
// 					}
// 					log.Printf("Discovered %s peer: %s at %s", peerStatus, peerID, address)
// 					s.peers[peerID] = address

// 					// Clear previous connection attempt if address changed
// 					delete(connectionAttempts, peerID)
// 				}
// 			}

// 			// Detect and remove peers that are no longer available
// 			for peerID := range s.peers {
// 				if peerID != s.deviceID && !activePeers[peerID] {
// 					log.Printf("Peer %s is no longer available, removing", peerID)
// 					delete(s.peers, peerID)
// 					delete(connectionAttempts, peerID)
// 					// Consider closing existing connections here if you track them
// 				}
// 			}

// 			// Launch connection attempts with rate limiting
// 			now := time.Now()
// 			maxConcurrentAttempts := 3 // Limit concurrent connection attempts
// 			activeAttempts := 0

// 			for peerID, addr := range s.peers {
// 				// Skip if we've recently attempted to connect
// 				lastAttempt, hasAttempted := connectionAttempts[peerID]
// 				if hasAttempted && now.Sub(lastAttempt) < connectionTimeout {
// 					continue
// 				}

// 				// Limit concurrent connection attempts
// 				if activeAttempts >= maxConcurrentAttempts {
// 					break
// 				}

// 				// Record connection attempt time
// 				connectionAttempts[peerID] = now
// 				activeAttempts++

// 				go func(id string, address string) {
// 					success := s.establishConnection(id, address)

// 					// Update the attempt time based on result
// 					s.connLock.Lock()
// 					if !success {
// 						// If failed, set a shorter retry interval
// 						connectionAttempts[id] = now.Add(-connectionTimeout + (5 * time.Second))
// 					} else {
// 						// If successful, no need to retry for a longer period
// 						connectionAttempts[id] = now
// 					}
// 					s.connLock.Unlock()
// 				}(peerID, addr)
// 			}

// 			s.connLock.Unlock()
// 		}
// 	}
// }

package server

import (
	"bufio"
	"encoding/hex"
	"errors"
	"fmt"
	"gorsync/pkg/utils"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Event struct {
	Type    string // e.g., "CREATE_DIR", "CREATE_FILE", "SEND_FILE_CONTENT"
	Payload string // Directory or file name, or file content
}

const chunkSize = 1024 * 1024 // 1MB chunk size for file transfer

// Memory store to hold file hashes
var fileHashStore = struct {
	sync.RWMutex
	hashes map[string]string
}{hashes: make(map[string]string)}

func Start(address, destDir string) error {
	utils.InitLogger()

	// Ensure the destination directory exists
	if _, err := os.Stat(destDir); os.IsNotExist(err) {
		if err := os.MkdirAll(destDir, 0755); err != nil {
			return fmt.Errorf("failed to create destination directory: %v", err)
		}
	}

	// Precompute hashes for files in the directory
	if err := calculateAndStoreHashes(destDir); err != nil {
		return fmt.Errorf("failed to calculate file hashes: %v", err)
	}

	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("failed to start server: %v", err)
	}
	defer listener.Close()

	fmt.Printf("Server listening on %s and writing to destination directory: %s\n", address, destDir)

	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Printf("connection error: %v\n", err)
			continue
		}
		go handleConnection(conn, destDir)
	}
}

func handleConnection(conn net.Conn, destDir string) {
	defer conn.Close()

	// Set a deadline for the connection to detect inactivity
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	scanner := bufio.NewScanner(conn)
	buf := make([]byte, 0, chunkSize) // Increase buffer size (1 MB)
	scanner.Buffer(buf, 10*1024*1024) // Set the maximum size (10 MB)

	for scanner.Scan() {
		// Set a deadline for the connection to detect inactivity
		_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

		line := scanner.Text()
		parts := strings.SplitN(line, "|", 2)
		if len(parts) != 2 {
			log.Printf("invalid message format: %s\n", line)
			continue
		}

		event := Event{
			Type:    parts[0],
			Payload: parts[1],
		}

		// Process the event (add to queue or handle directly)
		processEvent(event, destDir, conn)
	}

	// Handle end of connection or timeout
	if err := scanner.Err(); err != nil {
		if errors.Is(err, io.EOF) {
			log.Println("Client disconnected; shutting down server.")
		} else if os.IsTimeout(err) {
			log.Println("Connection timed out due to inactivity.")
		} else {
			log.Printf("Connection error: %v\n", err)
		}
		os.Exit(0) // Exit the server
	} else {
		log.Println("Scanner stopped without error.")
		os.Exit(0)
	}
}

func processEvent(event Event, destDir string, conn net.Conn) {
	switch event.Type {
	case "CREATE_DIR":
		destPath := filepath.Join(destDir, event.Payload)
		if _, err := os.Stat(destPath); err != nil {
			if os.IsNotExist(err) {
				if mkdirErr := os.MkdirAll(destPath, 0755); mkdirErr != nil {
					log.Printf("Failed to create directory: %s, error: %v\n", destPath, mkdirErr)
				}
			} else {
				log.Printf("Error checking directory: %s, error: %v\n", destPath, err)
			}
		}
	case "CHECK_HASH":
		// Extract client hash
		parts := strings.SplitN(event.Payload, "|", 2)
		if len(parts) != 2 {
			log.Printf("Invalid CHECK_HASH message: %s\n", event.Payload)
			return
		}

		relPath := parts[0]
		hash := parts[1]
		clientHash := strings.TrimSpace(hash)

		// Compare with memory store
		fileHashStore.RLock()
		serverHash, exists := fileHashStore.hashes[filepath.Join(destDir, relPath)]
		fileHashStore.RUnlock()

		if exists && serverHash == clientHash {
			// Send "SKIP" response
			_, _ = conn.Write([]byte("SKIP\n"))
		} else {
			// Send "UPLOAD" response
			_, _ = conn.Write([]byte("UPLOAD\n"))
		}
	case "CREATE_FILE":
		// Create the file and prepare for content writing
		destPath := filepath.Join(destDir, event.Payload)
		err := createFile(destPath)
		if err != nil {
			log.Printf("Failed to create file: %v\n", err)
			return
		} else {
			log.Printf("Created file: %s\n", destPath)
		}
	case "WRITE_FILE":
		receiveFileChunks(event, destDir)
	default:
		log.Printf("Unknown event type: %s\n", event.Type)
	}
}

// receiveFileChunks listens for file chunks and writes them to the destination file
func receiveFileChunks(event Event, destDir string) {
	// Split the line into file path and content (in hexadecimal)
	parts := strings.SplitN(event.Payload, "|", 2)
	if len(parts) != 2 {
		log.Printf("invalid chunk format: %s\n", event.Payload)
	}

	chank := parts[1]
	destPath := filepath.Join(destDir, parts[0])

	file, err := os.OpenFile(destPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("Failed to open file for writing: %v\n", err)
		return
	}
	defer file.Close()

	content, err := hex.DecodeString(chank)
	if err != nil {
		log.Printf("Failed to decode chunk: %v\n", err)
		return
	}

	// Write the decoded content to the file
	_, err = file.Write(content)
	if err != nil {
		log.Printf("Failed to write file chunk: %v\n", err)
		return
	}

	log.Printf("Writed to file: %s\n", destPath)
}

// createFile ensures the file is created before writing content
func createFile(path string) error {
	_, err := os.Create(path)
	return err
}

// calculateAndStoreHashes calculates and stores hashes for all files in the directory.
func calculateAndStoreHashes(dirPath string) error {
	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return fmt.Errorf("error accessing path %s: %v", path, err)
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		hash, err := utils.CalculateFileHash(path)
		if err != nil {
			return fmt.Errorf("error calculating hash for file %s: %v", path, err)
		}

		// Store the hash in the memory store
		fileHashStore.Lock()
		fileHashStore.hashes[path] = hash
		fileHashStore.Unlock()

		return nil
	})

	return err
}

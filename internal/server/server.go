package server

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"gorsync/pkg/utils"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
)

type Event struct {
	Type    string // e.g., "CREATE_DIR", "CREATE_FILE", "SEND_FILE_CONTENT"
	Payload string // Directory or file name, or file content
}

const chunkSize = 1024 * 1024 // 1MB chunk size for file transfer

func Start(address, destDir string) error {
	utils.InitLogger()

	// Ensure the destination directory exists
	if _, err := os.Stat(destDir); os.IsNotExist(err) {
		if err := os.MkdirAll(destDir, 0755); err != nil {
			return fmt.Errorf("failed to create destination directory: %v", err)
		}
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

	scanner := bufio.NewScanner(conn)
	buf := make([]byte, 0, chunkSize) // Increase buffer size (1 MB)
	scanner.Buffer(buf, 10*1024*1024) // Set the maximum size (10 MB)

	for scanner.Scan() {
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

		// log.Printf("Received event: %v\n", event)

		// Process the event (add to queue or handle directly)
		processEvent(event, destDir)
	}

	if err := scanner.Err(); err != nil {
		log.Printf("error reading from connection: %v\n", err)
	}
}

func processEvent(event Event, destDir string) {
	switch event.Type {
	case "CREATE_DIR":
		// Handle directory creation
		destPath := filepath.Join(destDir, event.Payload)
		err := os.MkdirAll(destPath, 0755)
		if err != nil {
			log.Printf("Failed to create directory: %v\n", err)
		} else {
			log.Printf("Created directory: %s\n", destPath)
		}
	case "CREATE_FILE":
		// Create the file and prepare for content writing
		destPath := filepath.Join(destDir, event.Payload)
		err := createFile(destPath)
		if err != nil {
			log.Printf("Failed to create file: %v\n", err)
			return
		}
	case "WRITE_FILE":
		// For chunked file transfer, handle inside the corresponding WRITE_FILE event logic
		// The handling is moved to receiveFileChunks for efficiency
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
}

// createFile ensures the file is created before writing content
func createFile(path string) error {
	_, err := os.Create(path)
	return err
}

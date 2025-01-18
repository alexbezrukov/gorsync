package server

import (
	"bufio"
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

		log.Printf("Received event: %v\n", event)

		// Process the event (add to queue or handle directly)
		processEvent(event, destDir, conn)
	}

	if err := scanner.Err(); err != nil {
		log.Printf("error reading from connection: %v\n", err)
	}
}

func processEvent(event Event, destDir string, conn net.Conn) {
	switch event.Type {
	case "CREATE_DIR":
		// Handle directory creation
		// log.Printf("Processing CREATE_DIR: %s\n", destPath)
		destPath := filepath.Join(destDir, event.Payload)

		err := os.MkdirAll(destPath, 0755)
		if err != nil {
			log.Printf("Failed to create directory: %v\n", err)
		} else {
			log.Printf("Created directory: %s\n", destPath)
		}

	case "CREATE_FILE":
		// Handle file creation and writing content in one go
		parts := strings.SplitN(event.Payload, "|", 2)
		if len(parts) != 2 {
			log.Printf("invalid message format of write event: %s\n", event.Payload)
			return
		}

		// Create the file and write the content
		destPath := filepath.Join(destDir, parts[0])
		content := parts[1]

		// Create the file (if it doesn't exist)
		err := createFile(destPath)
		if err != nil {
			log.Printf("Failed to create file: %v\n", err)
			return
		}

		// Write content to the file
		err = writeFileContent(destPath, content)
		if err != nil {
			log.Printf("Failed to write file content: %v\n", err)
		} else {
			log.Printf("Written content to file: %s\n", destPath)
		}

	default:
		log.Printf("Unknown event type: %s\n", event.Type)
	}
}

// Function to create the file
func createFile(filePath string) error {
	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create file %s: %v", filePath, err)
	}
	defer file.Close()
	return nil
}

// Function to write content to the file
func writeFileContent(filePath, content string) error {
	// Ensure the directory structure exists
	dir := filepath.Dir(filePath)
	err := os.MkdirAll(dir, 0755)
	if err != nil {
		return fmt.Errorf("failed to create directories for file %s: %v", filePath, err)
	}

	// Open the file for writing
	file, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to open file %s: %v", filePath, err)
	}
	defer file.Close()

	// Write the content to the file
	_, err = file.WriteString(content)
	if err != nil {
		return fmt.Errorf("failed to write content to file %s: %v", filePath, err)
	}

	return nil
}

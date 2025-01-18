package file

import (
	"bufio"
	"fmt"
	"gorsync/pkg/utils"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
)

// SendDirectory synchronizes a directory to the server.
func SendDirectory(conn net.Conn, sourceDir, destAddr string, recursive bool) error {
	// Validate the source directory
	info, err := os.Stat(sourceDir)
	if err != nil {
		return fmt.Errorf("failed to access source directory: %v", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("source is not a directory")
	}

	// Walk through the source directory
	err = filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return fmt.Errorf("error accessing file: %v", err)
		}

		if info.IsDir() && !recursive {
			return nil
		}

		relPath, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return fmt.Errorf("failed to get relative path: %v", err)
		}

		if info.IsDir() {
			// Send directory creation event
			event := fmt.Sprintf("CREATE_DIR|%s\n", relPath)
			_, err := conn.Write([]byte(event))
			if err != nil {
				return fmt.Errorf("failed to send directory event: %v", err)
			}
		} else {
			// Send file content
			content, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("failed to read file: %v", err)
			}
			event := fmt.Sprintf("CREATE_FILE|%s|%s\n", relPath, string(content))
			_, err = conn.Write([]byte(event))
			if err != nil {
				return fmt.Errorf("failed to send file content: %v", err)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("error walking through directory: %v", err)
	}

	return nil
}

func sendFile(filePath, relPath string, conn net.Conn) error {
	// Open the file
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// Notify the server about the file creation
	fileMessage := fmt.Sprintf("CREATE_FILE|%s", relPath)
	_, err = conn.Write([]byte(fileMessage + "\n"))
	if err != nil {
		return fmt.Errorf("failed to send file creation message: %w", err)
	}

	// Send the file content
	_, err = io.Copy(conn, file)
	if err != nil {
		return fmt.Errorf("failed to send file content: %w", err)
	}

	return nil
}

// sendFile sends a single file to the server with its relative path.
// func sendFile(filePath, relPath string, conn net.Conn) error {
// 	// Open the file
// 	file, err := os.Open(filePath)
// 	if err != nil {
// 		return fmt.Errorf("failed to open file %s: %v", filePath, err)
// 	}
// 	defer file.Close()

// 	// Send the relative path length
// 	relPathLength := len(relPath)
// 	err = writeInt(conn, relPathLength)
// 	if err != nil {
// 		return fmt.Errorf("failed to send relative path length: %v", err)
// 	}

// 	// Send the relative path
// 	_, err = conn.Write([]byte(relPath))
// 	if err != nil {
// 		return fmt.Errorf("failed to send relative path: %v", err)
// 	}

// 	// Get file size
// 	fileInfo, err := file.Stat()
// 	if err != nil {
// 		return fmt.Errorf("failed to get file info: %v", err)
// 	}
// 	fileSize := fileInfo.Size()

// 	// Send the file size
// 	err = writeInt64(conn, fileSize)
// 	if err != nil {
// 		return fmt.Errorf("failed to send file size: %v", err)
// 	}

// 	// Send file content
// 	_, err = io.Copy(conn, file)
// 	if err != nil {
// 		return fmt.Errorf("failed to send file content: %v", err)
// 	}

// 	return nil
// }

// writeInt sends an integer to the connection.
// func writeInt(conn net.Conn, value int) error {
// 	data := []byte{byte(value >> 24), byte(value >> 16), byte(value >> 8), byte(value)}
// 	_, err := conn.Write(data)
// 	return err
// }

// // writeInt64 sends a 64-bit integer to the connection.
// func writeInt64(conn net.Conn, value int64) error {
// 	data := make([]byte, 8)
// 	for i := uint(0); i < 8; i++ {
// 		data[7-i] = byte(value >> (i * 8))
// 	}
// 	_, err := conn.Write(data)
// 	return err
// }

func SendFile(conn net.Conn, destAddr, sourcePath string) error {
	file, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %v", err)
	}
	defer file.Close()

	// Calculate file hash
	fileHash, err := utils.CalculateFileHash(sourcePath)
	if err != nil {
		return fmt.Errorf("failed to calculate file hash: %v", err)
	}

	// Send the file name, size, and hash
	fileInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to get file info: %v", err)
	}
	fileName := filepath.Base(sourcePath)
	fileSize := fileInfo.Size()

	_, err = fmt.Fprintf(conn, "%s\n%d\n%s\n", fileName, fileSize, fileHash)
	if err != nil {
		return fmt.Errorf("failed to send file metadata: %v", err)
	}

	// Send the file data
	_, err = io.Copy(conn, file)
	if err != nil {
		return fmt.Errorf("failed to send file data: %v", err)
	}

	return nil
}

func ReceiveFile(conn net.Conn, destDir string) error {
	// Read the file name, size, and hash
	var fileName string
	var fileSize int64
	var fileHash string
	_, err := fmt.Fscanf(conn, "%s\n%d\n%s\n", &fileName, &fileSize, &fileHash)
	if err != nil {
		return fmt.Errorf("failed to read file metadata: %v", err)
	}

	destPath := filepath.Join(destDir, fileName)
	file, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %v", err)
	}
	defer file.Close()

	// Receive the file data
	_, err = io.CopyN(file, conn, fileSize)
	if err != nil {
		return fmt.Errorf("failed to receive file data: %v", err)
	}

	// Calculate the hash of the received file
	receivedFileHash, err := utils.CalculateFileHash(destPath)
	if err != nil {
		return fmt.Errorf("failed to calculate hash of received file: %v", err)
	}

	// Compare the hashes
	if receivedFileHash != fileHash {
		return fmt.Errorf("file hash mismatch: expected %s, got %s", fileHash, receivedFileHash)
	}

	return nil
}

func ReceiveDirectory(conn net.Conn, destDir string) error {
	reader := bufio.NewReader(conn)

	for {
		// Read the next message from the client
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break // Connection closed by the client
			}
			return fmt.Errorf("error reading from connection: %v", err)
		}
		line = strings.TrimSpace(line)

		// Parse the message
		parts := strings.SplitN(line, "|", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid message format: %s", line)
		}

		opType := parts[0]
		relPath := parts[1]
		absPath := filepath.Join(destDir, relPath)

		switch opType {
		case "CREATE_DIR":
			// Create a directory on the server
			if err := os.MkdirAll(absPath, 0755); err != nil {
				return fmt.Errorf("failed to create directory %s: %v", absPath, err)
			}
		default:
			return fmt.Errorf("unknown operation type: %s", opType)
		}
	}

	fmt.Println("Directory synchronization completed.")
	return nil
}

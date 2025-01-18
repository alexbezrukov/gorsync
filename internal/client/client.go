package client

import (
	"fmt"
	"gorsync/internal/file"
	"gorsync/pkg/utils"
	"net"
	"os"
)

func Start(sourcePath, destAddr string, recursive bool) error {
	utils.InitLogger()

	// Check if the source path exists and gather its information
	info, err := os.Stat(sourcePath)
	if err != nil {
		return fmt.Errorf("failed to access source path: %v", err)
	}

	conn, err := net.Dial("tcp", destAddr)
	if err != nil {
		return fmt.Errorf("failed to connect to server: %v", err)
	}
	defer conn.Close()

	if info.IsDir() {
		// Directory synchronization
		if recursive {
			fmt.Printf("Starting recursive synchronization from %s to %s\n", sourcePath, destAddr)
		} else {
			fmt.Printf("Starting non-recursive synchronization from %s to %s\n", sourcePath, destAddr)
		}

		err = file.SendDirectory(conn, sourcePath, destAddr, recursive)
		if err != nil {
			return fmt.Errorf("failed to sync directory: %v", err)
		}
		// fmt.Println("Directory synchronization completed successfully.")
	} else {
		// Single file synchronization
		// fmt.Printf("Starting file synchronization for %s to %s\n", sourcePath, destAddr)
		// err = file.SendFile(conn, destAddr, sourcePath)
		// if err != nil {
		// 	return fmt.Errorf("failed to sync file: %v", err)
		// }
		// fmt.Println("File synchronization completed successfully.")
	}

	return nil
}

// func sendFile(filePath, destAddr string) error {
// 	conn, err := net.Dial("tcp", destAddr)
// 	if err != nil {
// 		return fmt.Errorf("failed to connect to server: %v", err)
// 	}

// 	err = file.SendFile(conn, filePath)
// 	if err != nil {
// 		return fmt.Errorf("failed to send file: %v", err)
// 	}

// 	return nil
// }

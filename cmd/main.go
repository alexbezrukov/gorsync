package main

import (
	"context"
	"fmt"
	"gorsync/internal/device"
	"gorsync/internal/discovery"
	"gorsync/internal/server"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var rootCmd = &cobra.Command{
	Use:   "gorsync",
	Short: "gorsync - A simple directory sync tool",
	Long:  `gorsync synchronizes directories between multiple machines using a peer-to-peer model.`,
}

var defaultConfig = `
devices: {}
sync_directory: "./sync"
port: 9000
log_level: "info"
discovery_interval: 5
ignore_patterns:
  - "*.swp"
  - "*.tmp"
  - "node_modules"
  - ".git"
  - ".DS_Store"
  - "*.log"
  - "*.bak"
  - "*.~*"
  - "__pycache__"
  - "tmp"
  - ".obsidian"
`

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}

func init() {
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(addPeerCmd)
	rootCmd.AddCommand(startCmd)
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize the gorsync configuration",
	Run: func(cmd *cobra.Command, args []string) {
		configPath := "config.yaml"
		if _, err := os.Stat(configPath); err == nil {
			fmt.Println("Configuration file already exists.")
			return
		}
		err := os.WriteFile(configPath, []byte(defaultConfig), 0644)
		if err != nil {
			log.Fatal("Failed to create config file:", err)
		}
		fmt.Println("gorsync initialized successfully.")
	},
}

var addPeerCmd = &cobra.Command{
	Use:   "add-peer",
	Short: "Add a peer to sync with",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		peerID := args[0]
		peerAddress := args[1]
		viper.SetConfigFile("config.yaml")
		if err := viper.ReadInConfig(); err != nil {
			log.Fatal("Failed to read config:", err)
		}

		peers := viper.GetStringMapString("devices")
		peers[peerID] = peerAddress
		viper.Set("devices", peers)
		if err := viper.WriteConfig(); err != nil {
			log.Fatal("Failed to save config:", err)
		}

		fmt.Println("Added peer:", peerID, "->", peerAddress)
	},
}

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the sync service",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Starting gorsync...")

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		homeDir, err := os.UserHomeDir()
		if err != nil {
			log.Fatal("Failed to get user home directory:", err)
		}

		configDir := filepath.Join(homeDir, ".gorsync")
		if xdgConfigHome := os.Getenv("XDG_CONFIG_HOME"); xdgConfigHome != "" {
			configDir = filepath.Join(xdgConfigHome, "gorsync")
		}

		viper.SetConfigFile("config.yaml")
		if err := viper.ReadInConfig(); err != nil {
			log.Fatal("Failed to read config:", err)
		}

		deviceID, err := device.GenerateDeviceID(configDir)
		if err != nil {
			log.Fatal("Failed to generate device ID:", err)
		}

		port := viper.GetInt("port")
		syncDir := viper.GetString("sync_directory")

		// Create metadata with useful information
		metadata := map[string]string{
			"version": "1.0.0",
			"os":      detectOS(),
		}

		// Create the discovery service
		disc := discovery.NewDiscovery(
			deviceID,      // Unique ID for this instance
			"file-syncer", // Service name (all your sync apps should use the same name)
			9000,          // Port your sync service listens on
			metadata,      // Additional metadata
		)

		// Start the discovery service
		err = disc.Start()
		if err != nil {
			log.Fatalf("Failed to start discovery: %v", err)
		}

		// Handle OS signals
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

		go func() {
			<-sigChan
			fmt.Println("\nShutting down gracefully...")
			cancel()
			disc.Stop()
			os.Exit(0)
		}()

		// Start the sync server
		server := server.NewSyncServer(deviceID, port, syncDir, disc)
		if err := server.Start(ctx); err != nil {
			log.Fatal("Server error:", err)
		}
	},
}

// Get the real IP address of the container
func getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		log.Fatal(err)
	}

	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok {
			if !ipNet.IP.IsLoopback() && ipNet.IP.To4() != nil {
				return ipNet.IP.String()
			}
		}
	}
	return "127.0.0.1" // Fallback to localhost if no external IP is found
}

// detectOS returns the current operating system
func detectOS() string {
	switch os := runtime.GOOS; os {
	case "windows":
		return "windows"
	case "darwin":
		return "macos"
	case "linux":
		return "linux"
	default:
		return os
	}
}

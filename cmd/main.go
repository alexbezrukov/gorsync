package main

import (
	"context"
	"fmt"
	"gorsync/internal/api"
	"gorsync/internal/device"
	"gorsync/internal/discovery"
	"gorsync/internal/memstore"
	"gorsync/internal/server"
	"log"
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

		memstore := memstore.NewMemStore()

		// Create the discovery service
		discovery := discovery.NewDiscovery(
			deviceID,      // Unique ID for this instance
			"file-syncer", // Service name (all your sync apps should use the same name)
			9000,          // Port your sync service listens on
			metadata,      // Additional metadata
			memstore,
		)

		// Start the discovery service
		err = discovery.Start()
		if err != nil {
			log.Fatalf("Failed to start discovery: %v", err)
		}

		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

		syncServer := server.NewSyncServer(deviceID, port, syncDir, discovery)
		// go func() {
		// 	if err := syncServer.Start(ctx); err != nil {
		// 		log.Fatal("Server error:", err)
		// 	}
		// }()

		apiServer := api.NewAPIServer(syncServer, memstore)
		go func() {
			apiServer.Start()
		}()

		<-sigChan
		fmt.Println("\nShutting down gracefully...")
		cancel()
		discovery.Stop()
		apiServer.Stop(ctx)
		// syncServer.Stop()
		os.Exit(0)
	},
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

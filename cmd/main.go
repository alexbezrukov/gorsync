package main

import (
	"context"
	"fmt"
	"gorsync/internal/consul"
	"gorsync/internal/device"
	"gorsync/internal/server"
	"log"
	"os"
	"os/signal"
	"path/filepath"
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

		// Register service in Consul
		consulClient, err := consul.NewClient()
		if err != nil {
			log.Fatal("Failed to initialize Consul client:", err)
		}

		err = consulClient.RegisterService(deviceID, port)
		if err != nil {
			log.Fatal("Failed to register service in Consul:", err)
		}

		// Handle OS signals
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

		go func() {
			<-sigChan
			fmt.Println("\nShutting down gracefully...")
			cancel()
			consulClient.DeregisterService(deviceID)
			os.Exit(0)
		}()

		// Start the sync server
		server := server.NewSyncServer(deviceID, port, syncDir)
		if err := server.Start(ctx); err != nil {
			log.Fatal("Server error:", err)
		}
	},
}

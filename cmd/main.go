package main

import (
	"context"
	"fmt"
	"gorsync/internal/api"
	"gorsync/internal/client"
	"gorsync/internal/device"
	"gorsync/internal/discovery"
	"gorsync/internal/memstore"
	"gorsync/internal/server"
	"log"
	"os"
	"os/signal"
	"regexp"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

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
	rootCmd.AddCommand(initCmd, pairCmd, startCmd)
}

var rootCmd = &cobra.Command{
	Use:   "gorsync",
	Short: "Gorsync CLI for file synchronization",
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize the gorsync configuration",
	Run:   initializeConfig,
}

var pairCmd = &cobra.Command{
	Use:   "pair [pairing-code]",
	Short: "Add a new device using a pairing code",
	Args:  cobra.ExactArgs(1),
	Run:   pair,
}

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the sync service",
	Run:   startService,
}

func pair(cmd *cobra.Command, args []string) {
	if len(args) == 0 {
		fmt.Println("Please provide a pairing code")
		return
	}

	pairingCode := args[0]
	if !isValidPairingCode(pairingCode) {
		fmt.Println("Invalid pairing code format. Expected: XXXX-YYYY-ZZZZ-AAAA")
		return
	}

	relayServerURL := "https://cloud-relay.ru"

	c := client.NewWebSocketClient(relayServerURL)
	defer c.Close()

	err := c.Connect()
	if err != nil {
		fmt.Printf("Failed to connect to relay server: %s\n", err)
		return
	}

	device, err := c.StartPairing(pairingCode)
	if err != nil {
		fmt.Printf("Failed to start pairing: %s\n", err)
		return
	}

	// if err := file.SaveDeviceLocally(device.ID, *device); err != nil {
	// 	log.Fatalf("Failed to save device locally: %v", err)
	// }

	fmt.Println("Device added successfully. Device ID:", device.ID)
}

func initializeConfig(cmd *cobra.Command, args []string) {
	configPath := "config.yaml"
	if _, err := os.Stat(configPath); err == nil {
		fmt.Println("Configuration file already exists.")
		return
	}
	if err := os.WriteFile(configPath, []byte(defaultConfig), 0644); err != nil {
		log.Fatalf("Failed to create config file: %v", err)
	}
	fmt.Println("gorsync initialized successfully.")
}

func isValidPairingCode(code string) bool {
	re := regexp.MustCompile(`^[A-Z0-9]{4}-[A-Z0-9]{4}-[A-Z0-9]{4}-[A-Z0-9]{4}$`)
	return re.MatchString(code)
}

func startService(cmd *cobra.Command, args []string) {
	fmt.Println("Starting gorsync...")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	configDir := device.GetConfigDir()
	if err := loadConfig(); err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	deviceID, err := device.GetDeviceID(configDir)
	if err != nil {
		log.Fatalf("Failed to generate device ID: %v", err)
	}

	port := viper.GetInt("port")
	syncServer := setupSyncService(deviceID, port)
	apiServer := setupAPIServer(syncServer)

	setupSignalHandling(ctx, cancel, syncServer, apiServer)
}

func setupSyncService(deviceID string, port int) *server.SyncServer {
	syncDir := viper.GetString("sync_directory")
	discovery := discovery.NewDiscovery(deviceID,
		"file-syncer", 9000, memstore.NewMemStore())
	if err := discovery.Start(); err != nil {
		log.Fatalf("Failed to start discovery: %v", err)
	}

	syncServer := server.NewSyncServer(deviceID, port, syncDir, discovery)
	go func() {
		if err := syncServer.Start(context.Background()); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	}()
	return syncServer
}

func setupAPIServer(syncServer *server.SyncServer) *api.APIServer {
	apiServer := api.NewAPIServer(syncServer, memstore.NewMemStore())
	go apiServer.Start()
	return apiServer
}

func setupSignalHandling(ctx context.Context, cancel context.CancelFunc, syncServer *server.SyncServer, apiServer *api.APIServer) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan
	fmt.Println("\nShutting down gracefully...")

	cancel()
	syncServer.Stop()
	apiServer.Stop(ctx)
	os.Exit(0)
}

func loadConfig() error {
	viper.SetConfigFile("config.yaml")
	return viper.ReadInConfig()
}

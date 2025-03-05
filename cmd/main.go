package main

import (
	"context"
	"encoding/json"
	"fmt"
	"gorsync/internal/api"
	"gorsync/internal/client"
	"gorsync/internal/discovery"
	"gorsync/internal/memstore"
	"gorsync/internal/model"
	"gorsync/internal/server"
	"log"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
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
	rootCmd.AddCommand(initCmd, startCmd, addDeviceCmd)
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

var startCmd = &cobra.Command{
	Use:   "pair [pairing-code]",
	Short: "Pair device with sync network",
	Args:  cobra.ExactArgs(1),
	Run:   startPairing,
}

// CLI command to start pairing
func startPairing(cmd *cobra.Command, args []string) {
	// Retrieve configuration values
	relayServerURL, _ := cmd.Flags().GetString("relay-server")
	authToken, _ := cmd.Flags().GetString("auth-token")

	// Validate inputs
	if len(args) == 0 {
		fmt.Println("Please provide a pairing code")
		return
	}

	pairingCode := args[0]

	// Create WebSocket client
	c := client.NewWebSocketClient(relayServerURL, authToken)

	// Connect to relay server
	if err := c.Connect(); err != nil {
		log.Fatalf("Failed to connect to relay server: %v", err)
	}
	defer c.Close()

	// Start pairing process
	if err := c.StartPairing(pairingCode); err != nil {
		log.Fatalf("Pairing failed: %v", err)
	}

	// Start sync and wait
	// if err := c.StartSync(); err != nil {
	// 	log.Fatalf("Sync failed: %v", err)
	// }

	// Keep the connection alive
	select {}
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

// var startCmd = &cobra.Command{
// 	Use:   "start",
// 	Short: "Start the sync service",
// 	Run:   startService,
// }

// func startService(cmd *cobra.Command, args []string) {
// 	fmt.Println("Starting gorsync...")
// 	ctx, cancel := context.WithCancel(context.Background())
// 	defer cancel()

// 	configDir := getConfigDir()
// 	if err := loadConfig(); err != nil {
// 		log.Fatalf("Failed to load config: %v", err)
// 	}

// 	deviceID, err := device.GetDeviceID(configDir)
// 	if err != nil {
// 		log.Fatalf("Failed to generate device ID: %v", err)
// 	}

// 	port := viper.GetInt("port")
// 	syncServer := setupSyncService(deviceID, port)
// 	apiServer := setupAPIServer(syncServer)

// 	setupSignalHandling(ctx, cancel, syncServer, apiServer)
// }

func setupSyncService(deviceID string, port int) *server.SyncServer {
	syncDir := viper.GetString("sync_directory")
	discovery := discovery.NewDiscovery(deviceID, "file-syncer", 9000, map[string]string{"version": "1.0.0", "os": detectOS()}, memstore.NewMemStore())
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

var addDeviceCmd = &cobra.Command{
	Use:   "add-device [pairing-code]",
	Short: "Add a new device using a pairing code",
	Args:  cobra.ExactArgs(1),
	Run:   addDevice,
}

// addDevice is the CLI command handler for adding a new device
func addDevice(cmd *cobra.Command, args []string) {
	// Retrieve configuration values

	// Validate inputs
	if len(args) == 0 {
		fmt.Println("Please provide a pairing code")
		return
	}

	pairingCode := args[0]
	if !isValidPairingCode(pairingCode) {
		fmt.Println("Invalid pairing code format. Expected: XXXX-YYYY-ZZZZ-AAAA")
		return
	}

	// Perform device registration
	if err := registerAndSaveDevice(pairingCode, "https://cloud-relay.ru"); err != nil {
		log.Fatalf("Failed to add device: %v", err)
	}

	fmt.Println("Device added successfully.")
}

// registerAndSaveDevice handles the device registration process
func registerAndSaveDevice(pairingCode string, relayServerURL string) error {
	// Parse the relay server URL
	u, err := url.Parse(relayServerURL)
	if err != nil {
		return fmt.Errorf("invalid relay server URL: %v", err)
	}

	// Construct WebSocket URL
	u.Scheme = strings.Replace(u.Scheme, "http", "ws", 1)
	wsURL := fmt.Sprintf("%s/ws?code=%s", u.String(), pairingCode)

	// Establish WebSocket connection
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("failed to connect to relay server: %v", err)
	}
	defer conn.Close()

	// Generate a new device ID
	deviceID := generateDeviceID()

	// Prepare device registration payload
	deviceInfo := model.Device{
		ID:   deviceID,
		Name: getLocalHostname(),
	}
	payload, err := json.Marshal(deviceInfo)
	if err != nil {
		return fmt.Errorf("failed to marshal device info: %v", err)
	}

	// Create registration message
	registerMsg := model.Message{
		Type:     model.MsgTypeRegister,
		DeviceID: deviceID,
		Payload:  payload,
	}

	// Send registration message
	if err := conn.WriteJSON(registerMsg); err != nil {
		return fmt.Errorf("failed to send registration message: %v", err)
	}

	// Optional: Wait for confirmation or handle response
	var response model.Message
	if err := conn.ReadJSON(&response); err != nil {
		return fmt.Errorf("error reading server response: %v", err)
	}

	if response.Type == model.MsgTypeError {
		var errMsg struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(response.Payload, &errMsg); err == nil {
			return fmt.Errorf("server error: %s", errMsg.Error)
		}
		return fmt.Errorf("unknown server error")
	}

	// fmt.Println("response", response)

	// // Save device details locally (implement your preferred storage method)
	// if err := saveDeviceLocally(deviceID, deviceInfo); err != nil {
	// 	return fmt.Errorf("failed to save device locally: %v", err)
	// }

	return nil
}

// saveDeviceLocally saves the device information to a JSON file
func saveDeviceLocally(deviceID string, deviceInfo model.Device) error {
	// Ensure config directory exists
	configDir := getConfigDir()
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return fmt.Errorf("failed to create config directory: %v", err)
	}

	// Path to the devices configuration file
	configPath := filepath.Join(configDir, "devices.json")

	// Read existing configuration
	var config model.DeviceConfig
	configData, err := os.ReadFile(configPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("error reading existing config: %v", err)
		}
		// Initialize new config if file doesn't exist
		config = model.DeviceConfig{
			Devices: make(map[string]model.Device),
		}
	} else {
		// Unmarshal existing configuration
		if err := json.Unmarshal(configData, &config); err != nil {
			return fmt.Errorf("error parsing existing config: %v", err)
		}
	}

	// Add or update device
	config.Devices[deviceID] = deviceInfo

	// Marshal the updated configuration
	updatedConfigData, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal device config: %v", err)
	}

	// Write the updated configuration
	if err := os.WriteFile(configPath, updatedConfigData, 0600); err != nil {
		return fmt.Errorf("failed to write device config: %v", err)
	}

	return nil
}

// loadDevicesFromConfig reads the devices from the local configuration
func loadDevicesFromConfig() (map[string]model.Device, error) {
	configDir := getConfigDir()
	configPath := filepath.Join(configDir, "devices.json")

	// Read configuration file
	configData, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Return empty map if no config exists
			return make(map[string]model.Device), nil
		}
		return nil, fmt.Errorf("error reading device config: %v", err)
	}

	// Parse configuration
	var config model.DeviceConfig
	if err := json.Unmarshal(configData, &config); err != nil {
		return nil, fmt.Errorf("error parsing device config: %v", err)
	}

	return config.Devices, nil
}

// removeDeviceFromConfig removes a specific device from the local configuration
func removeDeviceFromConfig(deviceID string) error {
	configDir := getConfigDir()
	configPath := filepath.Join(configDir, "devices.json")

	// Read existing configuration
	var config model.DeviceConfig
	configData, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("error reading existing config: %v", err)
	}

	// Unmarshal existing configuration
	if err := json.Unmarshal(configData, &config); err != nil {
		return fmt.Errorf("error parsing existing config: %v", err)
	}

	// Remove the device
	delete(config.Devices, deviceID)

	// Marshal the updated configuration
	updatedConfigData, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal device config: %v", err)
	}

	// Write the updated configuration
	if err := os.WriteFile(configPath, updatedConfigData, 0600); err != nil {
		return fmt.Errorf("failed to write device config: %v", err)
	}

	return nil
}

func getConfigDir() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("Failed to get user home directory: %v", err)
	}
	if xdgConfigHome := os.Getenv("XDG_CONFIG_HOME"); xdgConfigHome != "" {
		return filepath.Join(xdgConfigHome, "gorsync")
	}
	return filepath.Join(homeDir, ".gorsync")
}

func loadConfig() error {
	viper.SetConfigFile("config.yaml")
	return viper.ReadInConfig()
}

func isValidPairingCode(code string) bool {
	re := regexp.MustCompile(`^[A-Z0-9]{4}-[A-Z0-9]{4}-[A-Z0-9]{4}-[A-Z0-9]{4}$`)
	return re.MatchString(code)
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

// generateDeviceID creates a unique device identifier
func generateDeviceID() string {
	// In a real implementation, this would be a more robust unique ID generation
	return strings.ReplaceAll(uuid.New().String(), "-", "")
}

// getLocalHostname retrieves the device's hostname
func getLocalHostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		// Fallback to a default name with some unique identifier
		return fmt.Sprintf("Device-%s", generateShortDeviceID())
	}
	return hostname
}

// generateShortDeviceID creates a shortened unique identifier
func generateShortDeviceID() string {
	// Use first 8 characters of the UUID
	fullID := strings.ReplaceAll(uuid.New().String(), "-", "")
	return fullID[:8]
}

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"gorsync/internal/api"
	"gorsync/internal/device"
	"gorsync/internal/discovery"
	"gorsync/internal/memstore"
	"gorsync/internal/model"
	"gorsync/internal/server"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"syscall"
	"time"

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

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the sync service",
	Run:   startService,
}

func startService(cmd *cobra.Command, args []string) {
	fmt.Println("Starting gorsync...")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	configDir := getConfigDir()
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

	device, err := getDevice(deviceID)
	if err != nil {
		log.Fatalf("Failed to get device: %v", err)
	}

	device.Syncing = true
	device.LastSeenAt = time.Now()
	localIP, err := getOutboundIP()
	if err != nil {
		fmt.Printf("Error getting outbound IP: %v\n", err)
		localIP = net.IPv4zero
	}
	device.Address = localIP.String()
	device.Port = port
	err = updateDeviceInfo(deviceID, *device)
	if err != nil {
		log.Fatalf("Failed to update device: %v", err)
	}

	setupSignalHandling(ctx, cancel, syncServer, apiServer)
}

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

	configDir := getConfigDir()
	deviceID, err := device.GetDeviceID(configDir)
	if err != nil {
		log.Fatalf("Failed to get device ID: %v", err)
	}

	device, err := getDevice(deviceID)
	if err != nil {
		log.Fatalf("Failed to get device: %v", err)
	}

	device.Syncing = false
	err = updateDeviceInfo(deviceID, *device)
	if err != nil {
		log.Fatalf("Failed to update device: %v", err)
	}

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

func addDevice(cmd *cobra.Command, args []string) {
	pairingCode := args[0]
	if !isValidPairingCode(pairingCode) {
		fmt.Println("Invalid pairing code format. Expected: XXXX-YYYY-ZZZZ-AAAA")
		return
	}
	if err := registerAndSaveDevice(pairingCode); err != nil {
		log.Fatalf("Failed to add device: %v", err)
	}
	fmt.Println("Device added successfully.")
}

func registerAndSaveDevice(pairingCode string) error {
	dev, err := registerDevice(pairingCode)
	if err != nil {
		return err
	}
	configDir := getConfigDir()
	return device.CreateConfig(configDir, dev.ID)
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

func registerDevice(pairingCode string) (*model.Device, error) {
	apiURL := "http://localhost:8080/api/devices/add"

	reqBody, err := json.Marshal(map[string]string{
		"pairing_code": pairingCode,
		"os":           detectOS(),
	})
	if err != nil {
		return nil, err
	}

	resp, err := http.Post(apiURL, "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("server error: %s", string(body))
	}

	// Unmarshal the response into a Device struct
	var device model.Device
	if err := json.NewDecoder(resp.Body).Decode(&device); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &device, nil
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

func getDevice(deviceID string) (*model.Device, error) {
	apiURL := fmt.Sprintf("http://localhost:8080/api/devices/%s", deviceID)

	// Create a new HTTP request
	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Execute the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Handle response status
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("server error: %s", string(body))
	}

	var device model.Device
	if err := json.NewDecoder(resp.Body).Decode(&device); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	log.Printf("Device found successfully: %+v", device.ID)
	return &device, nil
}

func updateDeviceInfo(deviceID string, updatedDevice model.Device) error {
	apiURL := fmt.Sprintf("http://localhost:8080/api/devices/%s", deviceID)

	// Marshal updated device data to JSON
	reqBody, err := json.Marshal(updatedDevice)
	if err != nil {
		return fmt.Errorf("failed to marshal request body: %w", err)
	}

	// Create a new HTTP request
	req, err := http.NewRequest(http.MethodPut, apiURL, bytes.NewBuffer(reqBody))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Execute the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Handle response status
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server error: %s", string(body))
	}

	// Parse response body (if needed)
	var updated model.Device
	if err := json.NewDecoder(resp.Body).Decode(&updated); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	log.Printf("Device updated successfully: %+v", updated)
	return nil
}

// var addPeerCmd = &cobra.Command{
// 	Use:   "add-peer",
// 	Short: "Add a peer to sync with",
// 	Args:  cobra.ExactArgs(2),
// 	Run: func(cmd *cobra.Command, args []string) {
// 		peerID := args[0]
// 		peerAddress := args[1]
// 		viper.SetConfigFile("config.yaml")
// 		if err := viper.ReadInConfig(); err != nil {
// 			log.Fatal("Failed to read config:", err)
// 		}

// 		peers := viper.GetStringMapString("devices")
// 		peers[peerID] = peerAddress
// 		viper.Set("devices", peers)
// 		if err := viper.WriteConfig(); err != nil {
// 			log.Fatal("Failed to save config:", err)
// 		}

// 		fmt.Println("Added peer:", peerID, "->", peerAddress)
// 	},
// }

// getOutboundIP gets the preferred outbound IP of this machine
func getOutboundIP() (net.IP, error) {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP, nil
}

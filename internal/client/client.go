package client

import (
	"encoding/json"
	"fmt"
	"gorsync/internal/deviceInfo"
	"gorsync/internal/model"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// WebSocketClient manages the WebSocket connection and device synchronization
type WebSocketClient struct {
	conn           *websocket.Conn
	relayServerURL string
	deviceID       string
	devices        map[string]model.Device
	devicesMutex   sync.RWMutex
}

// NewWebSocketClient creates a new WebSocket client
func NewWebSocketClient(relayServerURL string) *WebSocketClient {
	return &WebSocketClient{
		relayServerURL: relayServerURL,
		devices:        make(map[string]model.Device),
	}
}

// Connect establishes a WebSocket connection to the relay server
func (c *WebSocketClient) Connect() error {
	// Parse the relay server URL
	u, err := url.Parse(c.relayServerURL)
	if err != nil {
		return fmt.Errorf("invalid relay server URL: %v", err)
	}

	// Construct WebSocket URL
	u.Scheme = strings.Replace(u.Scheme, "http", "ws", 1)
	wsURL := fmt.Sprintf("%s/ws", u.String())

	// Establish WebSocket connection
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("failed to connect to relay server: %v", err)
	}

	c.conn = conn
	return nil
}

// waitForDeviceUpdate waits for a device update message from the server
func (c *WebSocketClient) WaitForDeviceUpdate() (*model.Device, error) {
	// Set a timeout for waiting for the device update
	_ = c.conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	defer c.conn.SetReadDeadline(time.Time{}) // Clear deadline

	for {
		// Read message
		_, msgBytes, err := c.conn.ReadMessage()
		if err != nil {
			return nil, fmt.Errorf("error reading WebSocket message: %v", err)
		}

		// Parse message
		var msg model.Message
		if err := json.Unmarshal(msgBytes, &msg); err != nil {
			return nil, fmt.Errorf("error parsing message: %v", err)
		}

		// Check for device update message
		if msg.Type == model.MsgDeviceUpdate {
			var deviceInfo model.Device
			if err := json.Unmarshal(msg.Payload, &deviceInfo); err != nil {
				return nil, fmt.Errorf("error parsing device info: %v", err)
			}
			return &deviceInfo, nil
		}

		// Optional: Handle other message types or unexpected messages
		if msg.Type == model.MsgTypeError {
			var errMsg struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(msg.Payload, &errMsg); err == nil {
				return nil, fmt.Errorf("server error: %s", errMsg.Error)
			}
			return nil, fmt.Errorf("unknown server error")
		}
	}
}

// Close terminates the WebSocket connection
func (c *WebSocketClient) Close() error {
	if c.conn == nil {
		return fmt.Errorf("WebSocket connection is not initialized")
	}

	err := c.conn.Close()
	if err != nil {
		return fmt.Errorf("failed to close WebSocket connection: %v", err)
	}

	c.conn = nil
	return nil
}

// StartPairing initiates the device pairing process
func (c *WebSocketClient) StartPairing(pairingCode string) (*model.Device, error) {
	if c.conn == nil {
		return nil, fmt.Errorf("not connected to relay server")
	}

	// Generate a new device ID
	deviceID, err := deviceInfo.GenerateDeviceID(deviceInfo.GetConfigDir())
	if err != nil {
		return nil, fmt.Errorf("failed to generate device id: %s", err)
	}

	// Prepare device registration payload
	deviceInfo := model.Device{
		ID:          deviceID,
		Name:        getLocalHostname(),
		PairingCode: pairingCode,
	}
	payload, err := json.Marshal(deviceInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal device info: %v", err)
	}

	// Create registration message
	registerMsg := model.Message{
		Type:     model.MsgTypeRegister,
		DeviceID: deviceID,
		Payload:  payload,
	}

	// Send registration message
	if err := c.conn.WriteJSON(registerMsg); err != nil {
		return nil, fmt.Errorf("failed to send registration message: %v", err)
	}

	return &deviceInfo, nil
}

// handleIncomingMessages processes messages from the relay server
func (c *WebSocketClient) handleIncomingMessages() {
	for {
		// Read message
		_, msgBytes, err := c.conn.ReadMessage()
		if err != nil {
			log.Printf("Error reading WebSocket message: %v", err)
			break
		}

		// Parse message
		var msg model.Message
		if err := json.Unmarshal(msgBytes, &msg); err != nil {
			log.Printf("Error parsing message: %v", err)
			continue
		}

		// Handle different message types
		switch msg.Type {
		case "device_update":
			c.handleDeviceUpdate(msg)
		case "pairing_complete":
			c.handlePairingComplete(msg)
			// Add other message type handlers as needed
		}
	}
}

// handleDeviceUpdate updates the local device list when a device changes
func (c *WebSocketClient) handleDeviceUpdate(msg model.Message) {
	var deviceInfo model.Device
	if err := json.Unmarshal(msg.Payload, &deviceInfo); err != nil {
		log.Printf("Error parsing device update: %v", err)
		return
	}

	c.devicesMutex.Lock()
	defer c.devicesMutex.Unlock()

	// Update or add device
	c.devices[deviceInfo.ID] = deviceInfo

	// Optional: Persist device list
	if err := c.saveDevicesToConfig(); err != nil {
		log.Printf("Failed to save device updates: %v", err)
	}

	// Log the update
	log.Printf("Device updated: %+v", deviceInfo)
}

// handlePairingComplete processes the successful pairing
func (c *WebSocketClient) handlePairingComplete(msg model.Message) {
	var pairingInfo struct {
		DeviceID string `json:"deviceId"`
	}
	if err := json.Unmarshal(msg.Payload, &pairingInfo); err != nil {
		log.Printf("Error parsing pairing complete message: %v", err)
		return
	}

	c.deviceID = pairingInfo.DeviceID
	log.Printf("Pairing complete. Assigned Device ID: %s", c.deviceID)
}

// saveDevicesToConfig saves the current device list to a JSON config file
func (c *WebSocketClient) saveDevicesToConfig() error {
	configDir := getConfigDir()
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return fmt.Errorf("failed to create config directory: %v", err)
	}

	configPath := filepath.Join(configDir, "devices.json")

	// Prepare device config
	config := model.DeviceConfig{
		Devices: c.devices,
	}

	// Marshal and save
	configData, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal device config: %v", err)
	}

	return os.WriteFile(configPath, configData, 0600)
}

// StartSync begins the synchronization process
func (c *WebSocketClient) StartSync() error {
	// Start message handling
	go c.handleIncomingMessages()

	// Periodic heartbeat or additional sync logic
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()

		for range ticker.C {
			// Send periodic sync message or heartbeat
			syncMsg := model.Message{
				Type: "sync_request",
			}
			if err := c.conn.WriteJSON(syncMsg); err != nil {
				log.Printf("Sync request failed: %v", err)
				break
			}
		}
	}()

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

package client

import (
	"context"
	"encoding/json"
	"fmt"
	"gorsync/internal/deviceInfo"
	"gorsync/internal/model"
	"io"
	"log"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// WebSocketClient manages the WebSocket connection and device synchronization
type WebSocketClient struct {
	conn           *websocket.Conn
	relayServerURL string
}

// NewWebSocketClient creates a new WebSocket client
func NewWebSocketClient(relayServerURL string) *WebSocketClient {
	return &WebSocketClient{
		relayServerURL: relayServerURL,
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
func (c *WebSocketClient) StartPairing(pairingCode string, port int) (*model.Device, error) {
	if c.conn == nil {
		return nil, fmt.Errorf("not connected to relay server")
	}

	// Generate a new device ID
	deviceID, err := generateDeviceID()
	if err != nil {
		return nil, fmt.Errorf("failed to generate device ID: %w", err)
	}

	localIP, err := getOutboundIP()
	if err != nil {
		return nil, fmt.Errorf("failed to get outbound ip: %w", err)
	}

	publicIP, err := getPublicIP()
	if err != nil {
		return nil, fmt.Errorf("failed to get public IP: %w", err)
	}

	// Prepare device registration payload
	deviceInfo := model.Device{
		ID:          deviceID,
		Name:        getLocalHostname(),
		PairingCode: pairingCode,
		Address:     localIP.String(),
		PublicAddr:  publicIP,
		Port:        port,
		OS:          deviceInfo.DetectOS(),
	}
	if err := c.sendRegistration(deviceInfo); err != nil {
		return nil, fmt.Errorf("failed to send registration message: %w", err)
	}

	return &deviceInfo, nil
}

func (c *WebSocketClient) sendRegistration(device model.Device) error {
	payload, err := json.Marshal(device)
	if err != nil {
		return fmt.Errorf("failed to marshal device info: %w", err)
	}

	registerMsg := model.Message{
		Type:     model.MsgTypeRegister,
		DeviceID: device.ID,
		Payload:  payload,
	}

	return c.conn.WriteJSON(registerMsg)
}

func (c *WebSocketClient) HandleMessages(ctx context.Context, cb func(msg model.Message)) {
	defer c.conn.Close()

	for {
		select {
		case <-ctx.Done():
			return
		default:
			_, msgBytes, err := c.conn.ReadMessage()
			if err != nil {
				log.Printf("Error reading from: %v", err)
				go c.reconnect(ctx)
				return
			}

			var msg model.Message
			if err := json.Unmarshal(msgBytes, &msg); err != nil {
				log.Printf("Error parsing message from: %v", err)
				continue
			}

			fmt.Println("msg", msg)

			cb(msg)
		}
	}
}

// SendMessage sends a message
func (c *WebSocketClient) SendMessage(msg model.Message) error {
	if c.conn == nil {
		return fmt.Errorf("no connection to relay server")
	}

	msgBytes, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	return c.conn.WriteMessage(websocket.TextMessage, msgBytes)
}

func (c *WebSocketClient) reconnect(ctx context.Context) {
	backoff := 1 * time.Second
	maxBackoff := 60 * time.Second

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
			log.Printf("Attempting to reconnect to relayt server...")

			if err := c.Connect(); err != nil {
				log.Printf("Failed to reaconnect to relay server: %v", err)

				jitter := time.Duration(rand.IntN(1000)) * time.Millisecond
				backoff = backoff*2 + jitter
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
			} else {
				log.Printf("Successfully reconnect to relay server")
				return
			}
		}
	}
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
			// c.handleDeviceUpdate(msg)
		case "pairing_complete":
			// c.handlePairingComplete(msg)
			// Add other message type handlers as needed
		}
	}
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

func getPublicIP() (string, error) {
	resp, err := http.Get("https://api64.ipify.org?format=text")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	ip, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(ip), nil
}

func generateDeviceID() (string, error) {
	return deviceInfo.GenerateDeviceID(deviceInfo.GetConfigDir())
}

// waitForDeviceUpdate waits for a device update message from the server
// func (c *WebSocketClient) WaitForDeviceUpdate() (*model.Device, error) {
// 	// Set a timeout for waiting for the device update
// 	_ = c.conn.SetReadDeadline(time.Now().Add(30 * time.Second))
// 	defer c.conn.SetReadDeadline(time.Time{}) // Clear deadline

// 	for {
// 		// Read message
// 		_, msgBytes, err := c.conn.ReadMessage()
// 		if err != nil {
// 			return nil, fmt.Errorf("error reading WebSocket message: %v", err)
// 		}

// 		// Parse message
// 		var msg model.Message
// 		if err := json.Unmarshal(msgBytes, &msg); err != nil {
// 			return nil, fmt.Errorf("error parsing message: %v", err)
// 		}

// 		// Check for device update message
// 		if msg.Type == model.MsgDeviceUpdate {
// 			var deviceInfo model.Device
// 			if err := json.Unmarshal(msg.Payload, &deviceInfo); err != nil {
// 				return nil, fmt.Errorf("error parsing device info: %v", err)
// 			}
// 			return &deviceInfo, nil
// 		}

// 		// Optional: Handle other message types or unexpected messages
// 		if msg.Type == model.MsgTypeError {
// 			var errMsg struct {
// 				Error string `json:"error"`
// 			}
// 			if err := json.Unmarshal(msg.Payload, &errMsg); err == nil {
// 				return nil, fmt.Errorf("server error: %s", errMsg.Error)
// 			}
// 			return nil, fmt.Errorf("unknown server error")
// 		}
// 	}
// }

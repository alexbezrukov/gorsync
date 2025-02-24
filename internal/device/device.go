package device

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type DeviceInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
}

func GenerateDeviceID(configDir string) (string, error) {
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create config directory: %w", err)
	}

	deviceFalePath := filepath.Join(configDir, "device.json")

	if deviceID, err := loadDeviceID(deviceFalePath); err == nil {
		return deviceID, nil
	}

	deviceID, err := createNewDeviceID(deviceFalePath)
	if err != nil {
		return "", fmt.Errorf("failed to create new device ID: %w", err)
	}

	return deviceID, nil
}

func loadDeviceID(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	var deviceInfo DeviceInfo
	if err := json.Unmarshal(data, &deviceInfo); err != nil {
		return "", err
	}

	return deviceInfo.ID, nil
}

func createNewDeviceID(path string) (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}

	deviceInfo := DeviceInfo{
		ID:        hex.EncodeToString(bytes),
		Name:      generateDeviceName(),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}

	data, err := json.MarshalIndent(deviceInfo, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal device info: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", fmt.Errorf("failed to write device info")
	}

	return deviceInfo.ID, nil
}

func generateDeviceName() string {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown-host"
	}
	return fmt.Sprintf("%s-%s", hostname, time.Now().Format("20060102"))
}

package deviceInfo

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
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

func GetDeviceID(configDir string) (string, error) {
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create config directory: %w", err)
	}

	deviceFalePath := filepath.Join(configDir, "device.json")

	deviceID, err := loadDeviceID(deviceFalePath)
	if err != nil {
		return "", err
	}

	return deviceID, nil
}

func CreateConfig(configDir string, deviceID string) error {
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	deviceFalePath := filepath.Join(configDir, "device.json")

	deviceInfo := DeviceInfo{
		ID:        deviceID,
		Name:      generateDeviceName(),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}

	data, err := json.MarshalIndent(deviceInfo, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal device info: %w", err)
	}

	if err := os.WriteFile(deviceFalePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write device info")
	}

	return nil
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

func GetConfigDir() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("Failed to get user home directory: %v", err)
	}
	if xdgConfigHome := os.Getenv("XDG_CONFIG_HOME"); xdgConfigHome != "" {
		return filepath.Join(xdgConfigHome, "gorsync")
	}
	return filepath.Join(homeDir, ".gorsync")
}

// detectOS returns the current operating system
func DetectOS() string {
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

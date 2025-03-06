package file

import (
	"encoding/json"
	"fmt"
	"gorsync/internal/model"
	"log"
	"os"
	"path/filepath"
)

// SaveDeviceLocally saves the device information to a JSON file
func SaveDeviceLocally(deviceID string, deviceInfo model.Device) error {
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

package main

import (
	"context"
	"fmt"
	"gorsync/internal/device"
	"gorsync/internal/netinfo"
	"gorsync/internal/server"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/grandcat/zeroconf"
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

func getLocalIP() string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return ""
	}

	for _, iface := range interfaces {
		// Ignore down interfaces, loopback, and virtual adapters (e.g., WSL)
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			if ipNet, ok := addr.(*net.IPNet); ok && ipNet.IP.To4() != nil {
				ip := ipNet.IP.String()
				// Exclude WSL subnet (e.g., 172.x.x.x)
				if !strings.HasPrefix(ip, "172.") {
					return ip
				}
			}
		}
	}

	return ""
}

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the sync service",
	Run: func(cmd *cobra.Command, args []string) {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Println("Error getting current directory:", err)
		} else {
			fmt.Println("Current working directory:", cwd)
		}

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

		// Get filtered network interfaces
		intr, err := netinfo.GetMainInternetInterface()
		if err != nil {
			log.Fatalf("Failed to get network interfaces: %v", err)
		}

		fmt.Println("intr", intr)

		zeroconfServer, err := zeroconf.Register(
			deviceID,              // Service instance name
			"_peer._tcp",          // Service type (custom)
			"local.",              // Service domain
			port,                  // Port to expose
			[]string{"txtvers=1"}, // Optional TXT records
			intr,                  // Use default network interface
		)
		if err != nil {
			log.Fatalf("Failed to register mDNS service: %v", err)
		}
		defer zeroconfServer.Shutdown()

		log.Printf("mDNS service '%s' registered on port %d", deviceID, port)

		// Start the sync server
		server := server.NewSyncServer(deviceID, port, syncDir)
		if err := server.Start(ctx); err != nil {
			log.Fatal("Server error:", err)
		}
	},
}

// func getSystemInterfaces() ([]netinfo.InterfaceInfo, error) {
// 	var validInterfaces []netinfo.InterfaceInfo

// 	// Get all network interfaces
// 	interfaces, err := netinfo.GetAllInterfaces()
// 	if err != nil {
// 		log.Fatalf("Error getting interfaces: %v", err)
// 	}

// 	for _, iface := range interfaces {
// 		// Exclude WSL-related interfaces
// 		fmt.Printf("iface: %s, is up: %v\n", iface.Name, iface.IsUp)
// 		if strings.Contains(iface.Name, "WSL") || strings.Contains(iface.Name, "vEthernet") {
// 			continue
// 		}
// 		validInterfaces = append(validInterfaces, iface)
// 	}

// 	return validInterfaces, nil
// }

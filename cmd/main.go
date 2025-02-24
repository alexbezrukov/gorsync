package main

import (
	"encoding/json"
	"fmt"
	"gorsync/internal/device"
	"gorsync/internal/model"
	"gorsync/internal/server"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/net/ipv4"
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
multicast_address: "224.0.0.1:9999"
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

func startAutomaticDiscovery(wg *sync.WaitGroup, deviceID string, port int) {
	defer wg.Done()
	go discoveryListener(deviceID)
	go discoveryBroadcaster(deviceID, port)
}

func discoveryListener(deviceID string) {
	addr := viper.GetString("multicast_address")
	localIP := getLocalIP()

	// Parse multicast address
	multicastAddr, err := net.ResolveUDPAddr("udp4", addr)
	if err != nil {
		log.Printf("Error resolving multicast address: %v", err)
		return
	}

	conn, err := net.ListenPacket("udp4", addr)
	if err != nil {
		log.Printf("Error starting discovery listener: %v", err)
		return
	}
	defer conn.Close()

	udpConn, ok := conn.(*net.UDPConn)
	if !ok {
		log.Println("Error asserting connection to *net.UDPConn")
		return
	}

	// Join the multicast group
	ip := udpConn.LocalAddr().(*net.UDPAddr).IP
	if ip == nil {
		log.Println("Local IP address not found")
		return
	}

	// Use the `ipv4` package to join the multicast group
	p := ipv4.NewPacketConn(udpConn)
	if err := p.JoinGroup(nil, &net.UDPAddr{IP: multicastAddr.IP}); err != nil {
		log.Printf("Error joining multicast group: %v", err)
		return
	}

	log.Println("Listening for peers...")
	buf := make([]byte, 1024)

	for {
		n, remoteAddr, err := conn.ReadFrom(buf)
		if err != nil {
			log.Printf("Read error: %v", err)
			continue
		}

		var peerInfo model.PeerInfo
		if err := json.Unmarshal(buf[:n], &peerInfo); err != nil {
			log.Printf("Invalid peer info: %v", err)
			continue
		}

		peers := viper.GetStringMapString("devices")
		_, exist := peers[peerInfo.DeviceID]

		if peerInfo.IP != localIP && peerInfo.DeviceID != deviceID && !exist {
			log.Printf("Discovered peer: %s (Device ID: %s) at %s:%d",
				peerInfo.IP, peerInfo.DeviceID, remoteAddr.String(), peerInfo.Port)

			// Add peer to configuration
			peerAddress := fmt.Sprintf("%s:%d", peerInfo.IP, peerInfo.Port)
			peers[peerInfo.DeviceID] = peerAddress
			viper.Set("devices", peers)

			if err := viper.WriteConfig(); err != nil {
				log.Printf("Failed to save new peer to config: %v", err)
			}
		}
	}
}

func discoveryBroadcaster(deviceID string, port int) {
	addr := viper.GetString("multicast_address")
	localIP := getLocalIP()
	discoveryInterval := viper.GetDuration("discovery_interval")
	if discoveryInterval == 0 {
		discoveryInterval = 30 * time.Second
	}

	conn, err := net.Dial("udp4", addr)
	if err != nil {
		log.Printf("Error creating discovery broadcaster: %v", err)
		return
	}
	defer conn.Close()

	peerInfo := model.PeerInfo{
		IP:       localIP,
		Port:     port,
		DeviceID: deviceID,
	}

	for {
		data, err := json.Marshal(peerInfo)
		if err != nil {
			log.Printf("Error marshaling peer info: %v", err)
			continue
		}

		if _, err := conn.Write(data); err != nil {
			log.Printf("Error broadcasting discovery: %v", err)
		}

		time.Sleep(discoveryInterval)
	}
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
		fmt.Println("Starting gorsync...")

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
		peers := viper.GetStringMapString("devices")

		// Start automatic discovery in background
		var wg sync.WaitGroup
		wg.Add(1)
		go startAutomaticDiscovery(&wg, deviceID, port)

		// Start the sync server
		server := server.NewSyncServer(deviceID, port, syncDir, peers)
		if err := server.Start(); err != nil {
			log.Fatal("Server error:", err)
		}
	},
}

//go:build darwin

package netinfo

import (
	"fmt"
	"net"
	"os/exec"
	"strings"
)

// GetMainInternetInterface returns the main network interface used for internet access on macOS
func GetMainInternetInterface() ([]net.Interface, error) {
	// Run 'route -n get default' to find the default route interface
	cmd := exec.Command("sh", "-c", "route -n get default | grep 'interface:' | awk '{print $2}'")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to execute route command: %w", err)
	}

	interfaceName := strings.TrimSpace(string(output))
	if interfaceName == "" {
		return nil, fmt.Errorf("could not determine main internet interface")
	}

	fmt.Println("Detected Interface:", interfaceName)

	// Get all network interfaces
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("failed to get network interfaces: %w", err)
	}

	// Find the interface that matches the detected interface name
	for _, iface := range interfaces {
		if iface.Name == interfaceName {
			return []net.Interface{iface}, nil
		}
	}

	return nil, fmt.Errorf("main internet interface not found")
}

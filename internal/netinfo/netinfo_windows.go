//go:build windows
// +build windows

package netinfo

import (
	"bytes"
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	// Constants from iptypes.h
	IfOperStatusUp            = 1
	IF_TYPE_SOFTWARE_LOOPBACK = 24
)

// GetAllInterfaces retrieves all network interfaces on Windows using syscalls
func GetAllInterfaces() ([]InterfaceInfo, error) {
	var interfaces []InterfaceInfo

	// Determine required buffer size
	var size uint32
	err := windows.GetAdaptersAddresses(syscall.AF_UNSPEC, windows.GAA_FLAG_INCLUDE_PREFIX, 0, nil, &size)
	if err != nil && err != windows.ERROR_BUFFER_OVERFLOW {
		return nil, err
	}

	// Allocate memory and retrieve adapter addresses
	buffer := make([]byte, size)
	addrPtr := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buffer[0]))
	err = windows.GetAdaptersAddresses(syscall.AF_UNSPEC, windows.GAA_FLAG_INCLUDE_PREFIX, 0, addrPtr, &size)
	if err != nil {
		return nil, err
	}

	// Iterate through adapters
	for addr := addrPtr; addr != nil; addr = addr.Next {
		if addr.OperStatus != windows.IfOperStatusUp { // Skip inactive interfaces
			continue
		}

		// Extract interface name
		name := windows.UTF16PtrToString(addr.FriendlyName)

		// Get MAC address
		mac := extractMAC(addr)

		// Process IP addresses
		ipv4Addresses, ipv6Addresses := extractIPAddresses(addr)

		// Ensure at least one valid address exists
		if len(ipv4Addresses) == 0 && len(ipv6Addresses) == 0 {
			continue
		}

		interfaces = append(interfaces, InterfaceInfo{
			Name:          name,
			MAC:           mac,
			IPv4Addresses: ipv4Addresses,
			IPv6Addresses: ipv6Addresses,
			IsUp:          true,
			IsLoopback:    addr.IfType == windows.IF_TYPE_SOFTWARE_LOOPBACK,
			Speed:         int64(addr.TransmitLinkSpeed / 1000000), // Convert to Mbps
		})
	}

	return interfaces, nil
}

// extractMAC retrieves the MAC address from an adapter
func extractMAC(addr *windows.IpAdapterAddresses) string {
	if addr.PhysicalAddressLength == 0 {
		return ""
	}
	hwAddr := make(net.HardwareAddr, addr.PhysicalAddressLength)
	for i := uint32(0); i < addr.PhysicalAddressLength; i++ {
		hwAddr[i] = addr.PhysicalAddress[i]
	}
	return hwAddr.String()
}

// extractIPAddresses extracts IPv4 and IPv6 addresses from an adapter
func extractIPAddresses(addr *windows.IpAdapterAddresses) ([]string, []string) {
	var ipv4Addresses, ipv6Addresses []string
	for unicastAddr := addr.FirstUnicastAddress; unicastAddr != nil; unicastAddr = unicastAddr.Next {
		sa := (*syscall.RawSockaddrAny)(unsafe.Pointer(unicastAddr.Address.Sockaddr))
		ip := sockaddrToIP(sa)
		if ip == "" || ip == "0.0.0.0" {
			continue // Ignore invalid or empty IPs
		}
		if net.ParseIP(ip).To4() != nil {
			ipv4Addresses = append(ipv4Addresses, ip)
		} else {
			ipv6Addresses = append(ipv6Addresses, ip)
		}
	}
	return ipv4Addresses, ipv6Addresses
}

// sockaddrToIP converts a raw sockaddr to an IP string
func sockaddrToIP(sa *syscall.RawSockaddrAny) string {
	switch sa.Addr.Family {
	case syscall.AF_INET:
		ip := (*syscall.RawSockaddrInet4)(unsafe.Pointer(sa))
		return net.IPv4(ip.Addr[0], ip.Addr[1], ip.Addr[2], ip.Addr[3]).String()
	case syscall.AF_INET6:
		ip := (*syscall.RawSockaddrInet6)(unsafe.Pointer(sa))
		return net.IP(ip.Addr[:]).String()
	}
	return ""
}

// extractIPFromSockaddr extracts IP address from a sockaddr structure
func extractIPFromSockaddr(rawSockaddr *syscall.RawSockaddrAny) string {
	switch rawSockaddr.Addr.Family {
	case syscall.AF_INET:
		sa := (*syscall.SockaddrInet4)(unsafe.Pointer(rawSockaddr))
		return net.IP(sa.Addr[:]).String()

	case syscall.AF_INET6:
		sa := (*syscall.SockaddrInet6)(unsafe.Pointer(rawSockaddr))
		return net.IP(sa.Addr[:]).String()
	}

	return ""
}

// GetMainInternetInterface returns the main network interface used for internet access
func GetMainInternetInterface() ([]net.Interface, error) {
	// Run 'route print' to find the default interface IP (not the gateway)
	cmd := exec.Command("cmd", "/C", "route print 0.0.0.0")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to execute route print: %w", err)
	}

	lines := strings.Split(string(output), "\n")
	var interfaceIP string

	// Parse the output to find the interface IP associated with 0.0.0.0
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 4 && fields[0] == "0.0.0.0" {
			interfaceIP = fields[3] // Extract the correct interface IP
			break
		}
	}

	if interfaceIP == "" {
		return nil, fmt.Errorf("could not determine main internet interface IP")
	}

	fmt.Println("Detected Interface IP:", interfaceIP)

	// Get all network interfaces
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("failed to get network interfaces: %w", err)
	}

	// Find the correct interface by matching its assigned IP
	for _, iface := range interfaces {
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			ip, _, err := net.ParseCIDR(addr.String())
			if err == nil && ip.String() == interfaceIP {
				return []net.Interface{iface}, nil
			}
		}
	}

	return nil, fmt.Errorf("main internet interface not found")
}

// getDefaultRouteInterfaceIP finds the local IP of the interface used for 0.0.0.0/0
func getDefaultRouteInterfaceIP() (string, error) {
	// Run `route print` to get the default gateway
	cmd := exec.Command("cmd", "/C", "route print 0.0.0.0")
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		return "", err
	}

	// Extract the interface IP from the output
	re := regexp.MustCompile(`\s+0.0.0.0\s+0.0.0.0\s+\S+\s+(\S+)\s+\d+`)
	matches := re.FindStringSubmatch(out.String())
	if len(matches) < 2 {
		return "", fmt.Errorf("could not determine default route interface IP")
	}

	return matches[1], nil
}

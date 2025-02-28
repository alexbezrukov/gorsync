package netinfo

// import (
// 	"bufio"
// 	"fmt"
// 	"io/ioutil"
// 	"os"
// 	"os/exec"
// 	"runtime"
// 	"strconv"
// 	"strings"
// )

// InterfaceInfo represents network interface details
type InterfaceInfo struct {
	Name          string
	MAC           string
	IPv4Addresses []string
	IPv6Addresses []string
	IsUp          bool
	IsLoopback    bool
	Speed         int64
}

// InterfaceInfo contains detailed information about a network interface
// type InterfaceInfo struct {
// 	Name          string
// 	HardwareAddr  string
// 	MTU           int
// 	Flags         uint32
// 	IPv4Addresses []string
// 	IPv6Addresses []string
// 	IsUp          bool
// 	IsLoopback    bool
// 	Speed         int64 // in Mbps
// 	Duplex        string
// 	Type          string
// 	Driver        string
// 	PCI           string // PCI information (Linux)
// 	Vendor        string
// 	Model         string
// 	MAC           string
// }

// // GetAllInterfaces returns detailed information about all network interfaces
// // without using net.Interfaces()
// func GetAllInterfaces() ([]InterfaceInfo, error) {
// 	switch runtime.GOOS {
// 	case "linux":
// 		return getLinuxInterfaces()
// 	case "windows":
// 		return getWindowsInterfaces()
// 	case "darwin":
// 		return getDarwinInterfaces()
// 	default:
// 		return nil, fmt.Errorf("unsupported operating system: %s", runtime.GOOS)
// 	}
// }

// // Linux-specific implementation
// func getLinuxInterfaces() ([]InterfaceInfo, error) {
// 	// Read from /proc/net/dev which contains all interfaces
// 	file, err := os.Open("/proc/net/dev")
// 	if err != nil {
// 		return nil, err
// 	}
// 	defer file.Close()

// 	var interfaces []InterfaceInfo

// 	// Skip the first two lines (headers)
// 	scanner := bufio.NewScanner(file)
// 	scanner.Scan() // header line 1
// 	scanner.Scan() // header line 2

// 	// Process each interface line
// 	for scanner.Scan() {
// 		line := scanner.Text()
// 		fields := strings.Fields(line)

// 		if len(fields) < 1 {
// 			continue
// 		}

// 		// First field is interface name with colon
// 		name := strings.TrimSuffix(fields[0], ":")

// 		// Create interface info
// 		info := InterfaceInfo{
// 			Name: name,
// 		}

// 		// Get additional info from /sys/class/net/{interface}
// 		sysPath := fmt.Sprintf("/sys/class/net/%s", name)

// 		// Get flags
// 		flagsBytes, err := ioutil.ReadFile(fmt.Sprintf("%s/flags", sysPath))
// 		if err == nil {
// 			flagsStr := strings.TrimSpace(string(flagsBytes))
// 			flags, err := strconv.ParseUint(flagsStr, 16, 32)
// 			if err == nil {
// 				info.Flags = uint32(flags)
// 				// IFF_UP is 0x1
// 				info.IsUp = (info.Flags & 0x1) != 0
// 				// IFF_LOOPBACK is 0x8
// 				info.IsLoopback = (info.Flags & 0x8) != 0
// 			}
// 		}

// 		// Get MAC address
// 		macBytes, err := ioutil.ReadFile(fmt.Sprintf("%s/address", sysPath))
// 		if err == nil {
// 			info.MAC = strings.TrimSpace(string(macBytes))
// 			info.HardwareAddr = info.MAC
// 		}

// 		// Get MTU
// 		mtuBytes, err := ioutil.ReadFile(fmt.Sprintf("%s/mtu", sysPath))
// 		if err == nil {
// 			mtuStr := strings.TrimSpace(string(mtuBytes))
// 			mtu, err := strconv.Atoi(mtuStr)
// 			if err == nil {
// 				info.MTU = mtu
// 			}
// 		}

// 		// Get IP addresses from /proc/net/if_inet6 and /proc/net/fib_trie
// 		getLinuxInterfaceAddresses(&info)

// 		// Get additional OS-specific information
// 		getLinuxAdditionalInfo(&info)

// 		interfaces = append(interfaces, info)
// 	}

// 	return interfaces, nil
// }

// // getLinuxInterfaceAddresses populates the IP addresses for a Linux interface
// func getLinuxInterfaceAddresses(info *InterfaceInfo) {
// 	// Get IPv4 addresses from /proc/net/fib_trie
// 	file, err := os.Open("/proc/net/fib_trie")
// 	if err == nil {
// 		defer file.Close()
// 		scanner := bufio.NewScanner(file)
// 		var currentPrefix string
// 		var lookingForLocal bool

// 		for scanner.Scan() {
// 			line := scanner.Text()
// 			line = strings.TrimSpace(line)

// 			// Match the interface name
// 			if strings.HasPrefix(line, "If:") && strings.Contains(line, info.Name) {
// 				lookingForLocal = true
// 				continue
// 			}

// 			// If we're looking for a local address
// 			if lookingForLocal {
// 				// Check for prefix line
// 				if strings.Contains(line, "/") && !strings.HasPrefix(line, "|") {
// 					parts := strings.Fields(line)
// 					if len(parts) > 0 {
// 						currentPrefix = parts[0]
// 					}
// 				}

// 				// Check for local addresses
// 				if strings.Contains(line, "LOCAL") {
// 					if currentPrefix != "" {
// 						// Add the prefix if it belongs to this interface
// 						ipAddr := strings.TrimSpace(currentPrefix)
// 						info.IPv4Addresses = append(info.IPv4Addresses, ipAddr)
// 						lookingForLocal = false
// 						currentPrefix = ""
// 					}
// 				}
// 			}
// 		}
// 	}

// 	// Get IPv6 addresses from /proc/net/if_inet6
// 	file, err = os.Open("/proc/net/if_inet6")
// 	if err == nil {
// 		defer file.Close()
// 		scanner := bufio.NewScanner(file)

// 		for scanner.Scan() {
// 			line := scanner.Text()
// 			fields := strings.Fields(line)

// 			if len(fields) >= 6 && fields[5] == info.Name {
// 				// Format IPv6 address
// 				addrStr := fields[0]
// 				var ipv6Addr strings.Builder

// 				for i := 0; i < len(addrStr); i += 4 {
// 					if i > 0 {
// 						ipv6Addr.WriteString(":")
// 					}
// 					end := i + 4
// 					if end > len(addrStr) {
// 						end = len(addrStr)
// 					}
// 					ipv6Addr.WriteString(addrStr[i:end])
// 				}

// 				// Add prefix length
// 				prefixLen, _ := strconv.Atoi(fields[1])
// 				ipv6AddrWithPrefix := fmt.Sprintf("%s/%d", ipv6Addr.String(), prefixLen)

// 				info.IPv6Addresses = append(info.IPv6Addresses, ipv6AddrWithPrefix)
// 			}
// 		}
// 	}
// }

// // getLinuxAdditionalInfo gets additional Linux-specific interface information
// func getLinuxAdditionalInfo(info *InterfaceInfo) {
// 	sysPath := fmt.Sprintf("/sys/class/net/%s", info.Name)

// 	// Get interface type
// 	typeBytes, err := ioutil.ReadFile(fmt.Sprintf("%s/type", sysPath))
// 	if err == nil {
// 		typeStr := strings.TrimSpace(string(typeBytes))
// 		typeNum, err := strconv.Atoi(typeStr)
// 		if err == nil {
// 			switch typeNum {
// 			case 1:
// 				info.Type = "Ethernet"
// 			case 772:
// 				info.Type = "Loopback"
// 			case 801:
// 				info.Type = "WLAN"
// 			default:
// 				info.Type = fmt.Sprintf("Unknown (%d)", typeNum)
// 			}
// 		}
// 	}

// 	// Get speed
// 	speedBytes, err := ioutil.ReadFile(fmt.Sprintf("%s/speed", sysPath))
// 	if err == nil {
// 		speedStr := strings.TrimSpace(string(speedBytes))
// 		speed, err := strconv.ParseInt(speedStr, 10, 64)
// 		if err == nil {
// 			info.Speed = speed
// 		}
// 	}

// 	// Get duplex
// 	duplexBytes, err := ioutil.ReadFile(fmt.Sprintf("%s/duplex", sysPath))
// 	if err == nil {
// 		info.Duplex = strings.TrimSpace(string(duplexBytes))
// 	}

// 	// Get driver
// 	driverPath := fmt.Sprintf("%s/device/driver", sysPath)
// 	if _, err := os.Stat(driverPath); err == nil {
// 		// Use readlink to get the driver name
// 		out, err := exec.Command("readlink", "-f", driverPath).Output()
// 		if err == nil {
// 			parts := strings.Split(strings.TrimSpace(string(out)), "/")
// 			if len(parts) > 0 {
// 				info.Driver = parts[len(parts)-1]
// 			}
// 		}
// 	}

// 	// Try to get vendor information
// 	vendorPath := fmt.Sprintf("%s/device/vendor", sysPath)
// 	if _, err := os.Stat(vendorPath); err == nil {
// 		vendorBytes, err := ioutil.ReadFile(vendorPath)
// 		if err == nil {
// 			info.Vendor = strings.TrimSpace(string(vendorBytes))
// 		}
// 	}

// 	// Try to get device/model information
// 	devicePath := fmt.Sprintf("%s/device/device", sysPath)
// 	if _, err := os.Stat(devicePath); err == nil {
// 		deviceBytes, err := ioutil.ReadFile(devicePath)
// 		if err == nil {
// 			info.Model = strings.TrimSpace(string(deviceBytes))
// 		}
// 	}

// 	// For PCI devices, try to get more detailed information
// 	if info.Vendor != "" || info.Model != "" {
// 		// Try to get PCI info using lspci
// 		out, err := exec.Command("lspci", "-v").Output()
// 		if err == nil {
// 			lines := strings.Split(string(out), "\n")
// 			for i, line := range lines {
// 				if strings.Contains(strings.ToLower(line), "network") || strings.Contains(strings.ToLower(line), "ethernet") {
// 					// Found a network device in lspci output
// 					info.PCI = line
// 					// Include a few more lines of details
// 					endIdx := i + 10
// 					if endIdx > len(lines) {
// 						endIdx = len(lines)
// 					}
// 					for j := i + 1; j < endIdx; j++ {
// 						info.PCI += "\n" + lines[j]
// 					}
// 					break
// 				}
// 			}
// 		}
// 	}
// }

// // Windows-specific implementation
// func getWindowsInterfaces() ([]InterfaceInfo, error) {
// 	// Use PowerShell to get network adapter information
// 	cmd := exec.Command("powershell", "-Command", "Get-NetAdapter | Select-Object Name,InterfaceDescription,Status,MacAddress,LinkSpeed,MediaType | ConvertTo-Csv -NoTypeInformation")
// 	output, err := cmd.Output()
// 	if err != nil {
// 		return nil, fmt.Errorf("error executing PowerShell command: %v", err)
// 	}

// 	var interfaces []InterfaceInfo

// 	// Parse CSV output
// 	lines := strings.Split(string(output), "\n")
// 	for i, line := range lines {
// 		// Skip header line
// 		if i == 0 || line == "" {
// 			continue
// 		}

// 		// Parse CSV line
// 		fields := strings.Split(line, ",")
// 		if len(fields) < 6 {
// 			continue
// 		}

// 		// Remove quotes
// 		for j := range fields {
// 			fields[j] = strings.Trim(fields[j], "\"")
// 		}

// 		// Create interface info
// 		info := InterfaceInfo{
// 			Name:         fields[0],
// 			Model:        fields[1],
// 			MAC:          fields[3],
// 			HardwareAddr: fields[3],
// 			IsUp:         fields[2] == "Up",
// 			Type:         fields[5],
// 		}

// 		// Parse speed
// 		speedStr := fields[4]
// 		if strings.Contains(speedStr, "gbps") {
// 			var speedValue float64
// 			fmt.Sscanf(speedStr, "%f gbps", &speedValue)
// 			info.Speed = int64(speedValue * 1000) // Convert Gbps to Mbps
// 		} else if strings.Contains(speedStr, "mbps") {
// 			var speedValue int64
// 			fmt.Sscanf(speedStr, "%d mbps", &speedValue)
// 			info.Speed = speedValue
// 		}

// 		// Get IP addresses
// 		getWindowsIPAddresses(&info)

// 		// Get additional details
// 		getWindowsAdditionalInfo(&info)

// 		interfaces = append(interfaces, info)
// 	}

// 	return interfaces, nil
// }

// // getWindowsIPAddresses populates the IP addresses for a Windows interface
// func getWindowsIPAddresses(info *InterfaceInfo) {
// 	// Use PowerShell to get IP addresses
// 	cmd := exec.Command("powershell", "-Command", fmt.Sprintf("Get-NetIPAddress -InterfaceAlias '%s' | Select-Object IPAddress,AddressFamily,PrefixLength | ConvertTo-Csv -NoTypeInformation", info.Name))
// 	output, err := cmd.Output()
// 	if err != nil {
// 		return
// 	}

// 	// Parse CSV output
// 	lines := strings.Split(string(output), "\n")
// 	for i, line := range lines {
// 		// Skip header line
// 		if i == 0 || line == "" {
// 			continue
// 		}

// 		// Parse CSV line
// 		fields := strings.Split(line, ",")
// 		if len(fields) < 3 {
// 			continue
// 		}

// 		// Remove quotes
// 		for j := range fields {
// 			fields[j] = strings.Trim(fields[j], "\"")
// 		}

// 		// Format IP address with prefix
// 		ipAddr := fields[0]
// 		prefixLen := fields[2]
// 		ipWithPrefix := fmt.Sprintf("%s/%s", ipAddr, prefixLen)

// 		// Add to appropriate list based on address family
// 		addrFamily := fields[1]
// 		if addrFamily == "IPv4" {
// 			info.IPv4Addresses = append(info.IPv4Addresses, ipWithPrefix)
// 		} else if addrFamily == "IPv6" {
// 			info.IPv6Addresses = append(info.IPv6Addresses, ipWithPrefix)
// 		}
// 	}
// }

// // getWindowsAdditionalInfo gets additional Windows-specific interface information
// func getWindowsAdditionalInfo(info *InterfaceInfo) {
// 	// Get MTU
// 	cmd := exec.Command("powershell", "-Command", fmt.Sprintf("Get-NetIPInterface -InterfaceAlias '%s' -AddressFamily IPv4 | Select-Object NlMtu | ConvertTo-Csv -NoTypeInformation", info.Name))
// 	output, err := cmd.Output()
// 	if err == nil {
// 		lines := strings.Split(string(output), "\n")
// 		if len(lines) >= 2 {
// 			mtuStr := strings.Trim(lines[1], "\"")
// 			mtu, err := strconv.Atoi(mtuStr)
// 			if err == nil {
// 				info.MTU = mtu
// 			}
// 		}
// 	}

// 	// Get driver information
// 	cmd = exec.Command("powershell", "-Command", fmt.Sprintf("Get-NetAdapter -Name '%s' | Get-NetAdapterDriver | Select-Object DriverProvider,DriverVersion | ConvertTo-Csv -NoTypeInformation", info.Name))
// 	output, err = cmd.Output()
// 	if err == nil {
// 		lines := strings.Split(string(output), "\n")
// 		if len(lines) >= 2 {
// 			fields := strings.Split(lines[1], ",")
// 			if len(fields) >= 2 {
// 				provider := strings.Trim(fields[0], "\"")
// 				version := strings.Trim(fields[1], "\"")
// 				info.Driver = fmt.Sprintf("%s %s", provider, version)
// 			}
// 		}
// 	}

// 	// Check if it's a loopback interface
// 	for _, addr := range info.IPv4Addresses {
// 		if strings.HasPrefix(addr, "127.") {
// 			info.IsLoopback = true
// 			break
// 		}
// 	}

// 	// Try to get vendor information from MAC address
// 	if len(info.MAC) >= 8 {
// 		// First 6 characters of MAC could be used to identify vendor
// 		// In a real implementation, you'd have a database of MAC prefixes
// 		info.Vendor = fmt.Sprintf("Vendor for MAC prefix: %s", strings.Replace(info.MAC[:8], ":", "", -1))
// 	}
// }

// // Darwin (macOS) specific implementation
// func getDarwinInterfaces() ([]InterfaceInfo, error) {
// 	// Use the 'ifconfig' command to get network interface information
// 	cmd := exec.Command("ifconfig", "-a")
// 	output, err := cmd.Output()
// 	if err != nil {
// 		return nil, fmt.Errorf("error executing ifconfig command: %v", err)
// 	}

// 	var interfaces []InterfaceInfo
// 	var currentIface *InterfaceInfo

// 	// Parse ifconfig output
// 	scanner := bufio.NewScanner(strings.NewReader(string(output)))
// 	for scanner.Scan() {
// 		line := scanner.Text()

// 		// Check for a new interface section
// 		if !strings.HasPrefix(line, "\t") && strings.Contains(line, ":") {
// 			// Save previous interface if it exists
// 			if currentIface != nil {
// 				interfaces = append(interfaces, *currentIface)
// 			}

// 			// Start new interface
// 			fields := strings.Fields(line)
// 			name := strings.TrimSuffix(fields[0], ":")

// 			currentIface = &InterfaceInfo{
// 				Name: name,
// 			}

// 			// Check flags
// 			if len(fields) > 1 {
// 				flagsStr := fields[1]
// 				if strings.Contains(flagsStr, "UP") {
// 					currentIface.IsUp = true
// 				}
// 				if strings.Contains(flagsStr, "LOOPBACK") {
// 					currentIface.IsLoopback = true
// 					currentIface.Type = "Loopback"
// 				}
// 			}
// 		} else if currentIface != nil {
// 			// Process details for current interface
// 			line = strings.TrimSpace(line)

// 			// Get MAC address
// 			if strings.HasPrefix(line, "ether ") {
// 				currentIface.MAC = strings.Fields(line)[1]
// 				currentIface.HardwareAddr = currentIface.MAC
// 			}

// 			// Get MTU
// 			if strings.HasPrefix(line, "mtu ") {
// 				mtuStr := strings.Fields(line)[1]
// 				mtu, err := strconv.Atoi(mtuStr)
// 				if err == nil {
// 					currentIface.MTU = mtu
// 				}
// 			}

// 			// Get IPv4 address
// 			if strings.HasPrefix(line, "inet ") {
// 				fields := strings.Fields(line)
// 				if len(fields) >= 4 {
// 					ipAddr := fields[1]
// 					mask := fields[3]

// 					// Convert netmask to prefix length
// 					prefixLen := getMaskPrefixLength(mask)
// 					ipWithPrefix := fmt.Sprintf("%s/%d", ipAddr, prefixLen)

// 					currentIface.IPv4Addresses = append(currentIface.IPv4Addresses, ipWithPrefix)
// 				}
// 			}

// 			// Get IPv6 address
// 			if strings.HasPrefix(line, "inet6 ") {
// 				fields := strings.Fields(line)
// 				if len(fields) >= 4 {
// 					ipAddr := fields[1]
// 					prefixLen, err := strconv.Atoi(strings.TrimPrefix(fields[3], "prefixlen"))
// 					if err == nil {
// 						ipWithPrefix := fmt.Sprintf("%s/%d", ipAddr, prefixLen)
// 						currentIface.IPv6Addresses = append(currentIface.IPv6Addresses, ipWithPrefix)
// 					}
// 				}
// 			}
// 		}
// 	}

// 	// Add the last interface
// 	if currentIface != nil {
// 		interfaces = append(interfaces, *currentIface)
// 	}

// 	// Get additional information for each interface
// 	for i := range interfaces {
// 		getDarwinAdditionalInfo(&interfaces[i])
// 	}

// 	return interfaces, nil
// }

// // getMaskPrefixLength converts a netmask string to a prefix length
// func getMaskPrefixLength(mask string) int {
// 	// Parse the netmask like 255.255.255.0
// 	parts := strings.Split(mask, ".")
// 	if len(parts) != 4 {
// 		return 0
// 	}

// 	prefixLen := 0
// 	for _, part := range parts {
// 		num, err := strconv.Atoi(part)
// 		if err != nil {
// 			continue
// 		}

// 		// Count bits
// 		for i := 7; i >= 0; i-- {
// 			if (num & (1 << i)) != 0 {
// 				prefixLen++
// 			} else {
// 				break
// 			}
// 		}
// 	}

// 	return prefixLen
// }

// // getDarwinAdditionalInfo gets additional macOS-specific interface information
// func getDarwinAdditionalInfo(info *InterfaceInfo) {
// 	// Determine interface type if not already set
// 	if info.Type == "" {
// 		if strings.HasPrefix(info.Name, "en") {
// 			// Check if it's Wi-Fi or Ethernet
// 			cmd := exec.Command("networksetup", "-listallhardwareports")
// 			output, err := cmd.Output()
// 			if err == nil {
// 				lines := strings.Split(string(output), "\n")
// 				for i, line := range lines {
// 					if strings.Contains(line, "Device: "+info.Name) && i > 0 {
// 						// Check the previous line for the hardware port type
// 						if strings.Contains(lines[i-1], "Wi-Fi") {
// 							info.Type = "Wi-Fi"
// 							break
// 						} else if strings.Contains(lines[i-1], "Ethernet") {
// 							info.Type = "Ethernet"
// 							break
// 						}
// 					}
// 				}
// 			}

// 			// If still not determined, default to Ethernet
// 			if info.Type == "" {
// 				info.Type = "Ethernet"
// 			}
// 		} else if strings.HasPrefix(info.Name, "bridge") {
// 			info.Type = "Bridge"
// 		} else if strings.HasPrefix(info.Name, "awdl") {
// 			info.Type = "Apple Wireless Direct Link"
// 		} else if strings.HasPrefix(info.Name, "llw") {
// 			info.Type = "Low Latency WLAN"
// 		} else if strings.HasPrefix(info.Name, "utun") {
// 			info.Type = "Tunnel"
// 		}
// 	}

// 	// For Ethernet and Wi-Fi interfaces, try to get more details
// 	if info.Type == "Ethernet" {
// 		// Try to get speed and duplex
// 		cmd := exec.Command("networksetup", "-getMedia", info.Name)
// 		output, err := cmd.Output()
// 		if err == nil {
// 			outputStr := string(output)

// 			// Parse speed
// 			if strings.Contains(outputStr, "1000baseT") {
// 				info.Speed = 1000
// 			} else if strings.Contains(outputStr, "100baseT") {
// 				info.Speed = 100
// 			} else if strings.Contains(outputStr, "10baseT") {
// 				info.Speed = 10
// 			}

// 			// Parse duplex
// 			if strings.Contains(outputStr, "full-duplex") {
// 				info.Duplex = "full"
// 			} else if strings.Contains(outputStr, "half-duplex") {
// 				info.Duplex = "half"
// 			}
// 		}
// 	} else if info.Type == "Wi-Fi" {
// 		// For Wi-Fi, try to get signal info
// 		cmd := exec.Command("/System/Library/PrivateFrameworks/Apple80211.framework/Versions/Current/Resources/airport", "-I")
// 		output, err := cmd.Output()
// 		if err == nil {
// 			lines := strings.Split(string(output), "\n")
// 			for _, line := range lines {
// 				line = strings.TrimSpace(line)

// 				// Check for transmission rate
// 				if strings.HasPrefix(line, "lastTxRate:") {
// 					parts := strings.Split(line, ":")
// 					if len(parts) >= 2 {
// 						rateStr := strings.TrimSpace(parts[1])
// 						rate, err := strconv.ParseInt(rateStr, 10, 64)
// 						if err == nil {
// 							info.Speed = rate
// 						}
// 					}
// 				}
// 			}
// 		}
// 	}

// 	// Try to get vendor information from MAC address
// 	if len(info.MAC) >= 8 {
// 		// First 6 characters of MAC could be used to identify vendor
// 		// In a real implementation, you'd have a database of MAC prefixes
// 		info.Vendor = fmt.Sprintf("Vendor for MAC prefix: %s", strings.Replace(info.MAC[:8], ":", "", -1))
// 	}

// 	// Try to get more hardware details using system_profiler
// 	if info.Type == "Ethernet" || info.Type == "Wi-Fi" {
// 		cmd := exec.Command("system_profiler", "SPNetworkDataType")
// 		output, err := cmd.Output()
// 		if err == nil {
// 			outputStr := string(output)
// 			lines := strings.Split(outputStr, "\n")

// 			inSection := false
// 			for _, line := range lines {
// 				line = strings.TrimSpace(line)

// 				// Check if we're in the section for this interface
// 				if strings.Contains(line, info.Name+":") {
// 					inSection = true
// 					continue
// 				}

// 				// If we find another interface, we're out of our section
// 				if inSection && strings.HasSuffix(line, ":") && !strings.Contains(line, "Address") {
// 					inSection = false
// 				}

// 				// Extract information within our section
// 				if inSection {
// 					if strings.Contains(line, "Model:") {
// 						info.Model = strings.TrimSpace(strings.TrimPrefix(line, "Model:"))
// 					} else if strings.Contains(line, "Vendor:") {
// 						info.Vendor = strings.TrimSpace(strings.TrimPrefix(line, "Vendor:"))
// 					} else if strings.Contains(line, "Driver:") {
// 						info.Driver = strings.TrimSpace(strings.TrimPrefix(line, "Driver:"))
// 					}
// 				}
// 			}
// 		}
// 	}
// }

// // GetInterfaceByName returns detailed information about a specific interface by name
// func GetInterfaceByName(name string) (InterfaceInfo, error) {
// 	interfaces, err := GetAllInterfaces()
// 	if err != nil {
// 		return InterfaceInfo{}, err
// 	}

// 	for _, iface := range interfaces {
// 		if iface.Name == name {
// 			return iface, nil
// 		}
// 	}

// 	return InterfaceInfo{}, fmt.Errorf("interface %s not found", name)
// }

// // PrintInterfaceInfo prints detailed information about a network interface
// func PrintInterfaceInfo(info InterfaceInfo) {
// 	fmt.Printf("Interface: %s\n", info.Name)
// 	fmt.Printf("  Status: %s\n", getStatusString(info.IsUp))
// 	fmt.Printf("  Hardware Address (MAC): %s\n", info.MAC)
// 	fmt.Printf("  MTU: %d\n", info.MTU)
// 	fmt.Printf("  Type: %s\n", info.Type)

// 	if info.Speed > 0 {
// 		fmt.Printf("  Speed: %d Mbps\n", info.Speed)
// 	}
// 	if info.Duplex != "" {
// 		fmt.Printf("  Duplex: %s\n", info.Duplex)
// 	}
// 	if info.Driver != "" {
// 		fmt.Printf("  Driver: %s\n", info.Driver)
// 	}
// 	if info.Vendor != "" {
// 		fmt.Printf("  Vendor: %s\n", info.Vendor)
// 	}
// 	if info.Model != "" {
// 		fmt.Printf("  Model: %s\n", info.Model)
// 	}

// 	fmt.Println("  IPv4 Addresses:")
// 	for _, addr := range info.IPv4Addresses {
// 		fmt.Printf("    %s\n", addr)
// 	}

// 	fmt.Println("  IPv6 Addresses:")
// 	for _, addr := range info.IPv6Addresses {
// 		fmt.Printf("    %s\n", addr)
// 	}

// 	fmt.Printf("  Flags: 0x%x\n", info.Flags)

// 	if info.PCI != "" {
// 		fmt.Println("  PCI Information:")
// 		fmt.Println(info.PCI)
// 	}

// 	fmt.Println()
// }

// // Helper function to get status string
// func getStatusString(isUp bool) string {
// 	if isUp {
// 		return "UP"
// 	}
// 	return "DOWN"
// }

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

// func getLocalIP() string {
// 	interfaces, err := net.Interfaces()
// 	if err != nil {
// 		return ""
// 	}

// 	for _, iface := range interfaces {
// 		// Ignore down interfaces, loopback, and virtual adapters (e.g., WSL)
// 		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
// 			continue
// 		}

// 		addrs, err := iface.Addrs()
// 		if err != nil {
// 			continue
// 		}

// 		for _, addr := range addrs {
// 			if ipNet, ok := addr.(*net.IPNet); ok && ipNet.IP.To4() != nil {
// 				ip := ipNet.IP.String()
// 				// Exclude WSL subnet (e.g., 172.x.x.x)
// 				if !strings.HasPrefix(ip, "172.") {
// 					return ip
// 				}
// 			}
// 		}
// 	}

// 	return ""
// }

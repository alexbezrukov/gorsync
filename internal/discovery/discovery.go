package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"gorsync/internal/memstore"
	"gorsync/internal/model"
	"net"
	"sync"
	"time"
)

// Discovery handles service discovery in local network
type Discovery struct {
	serviceID     string
	serviceName   string
	port          int
	broadcastAddr string
	broadcastPort int
	devices       map[string]*model.Device
	listener      *net.UDPConn
	broadcaster   *net.UDPConn
	mutex         sync.RWMutex
	ctx           context.Context
	cancel        context.CancelFunc
	memstore      *memstore.MemStore
}

// NewDiscovery creates a new service discovery instance
func NewDiscovery(
	serviceID, serviceName string,
	port int,
	memstore *memstore.MemStore) *Discovery {
	ctx, cancel := context.WithCancel(context.Background())
	return &Discovery{
		serviceID:     serviceID,
		serviceName:   serviceName,
		port:          port,
		broadcastAddr: "255.255.255.255",
		broadcastPort: 9876, // Choose a port unlikely to be in use
		devices:       make(map[string]*model.Device),
		ctx:           ctx,
		cancel:        cancel,
		memstore:      memstore,
	}
}

// Start begins broadcasting and listening for service announcements
func (d *Discovery) Start() error {
	// Set up UDP listening
	listenAddr := &net.UDPAddr{
		IP:   net.IPv4zero, // Listen on all interfaces
		Port: d.broadcastPort,
	}

	var err error
	d.listener, err = net.ListenUDP("udp4", listenAddr)
	if err != nil {
		return fmt.Errorf("failed to start listener: %w", err)
	}

	// Set up UDP broadcasting
	broadcastAddr := &net.UDPAddr{
		IP:   net.ParseIP(d.broadcastAddr),
		Port: d.broadcastPort,
	}

	d.broadcaster, err = net.DialUDP("udp4", nil, broadcastAddr)
	if err != nil {
		d.listener.Close()
		return fmt.Errorf("failed to set up broadcaster: %w", err)
	}

	// Start listening for broadcasts
	go d.receiveAnnouncements()

	// Start broadcasting our presence
	go d.broadcastPresence()

	// Start cleanup routine for stale services
	go d.cleanupStaleDevices()

	return nil
}

// Stop terminates the discovery service
func (d *Discovery) Stop() {
	d.cancel()
	if d.listener != nil {
		d.listener.Close()
	}
	if d.broadcaster != nil {
		d.broadcaster.Close()
	}
}

// GetDevices returns all discovered devices
func (d *Discovery) GetDevices() []*model.Device {
	d.mutex.RLock()
	defer d.mutex.RUnlock()

	devcies := make([]*model.Device, 0, len(d.devices))
	for _, devcie := range d.devices {
		devcies = append(devcies, devcie)
	}
	return devcies
}

func (d *Discovery) GetDevice(deviceID string) *model.Device {
	d.mutex.RLock()
	defer d.mutex.RUnlock()

	for _, device := range d.devices {
		if device.ID == deviceID {
			return device
		}
	}
	return nil
}

func (d *Discovery) ChangeDeviceInfo(s *model.Device) {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	d.devices[s.ID] = s
}

// GetDevicesByName returns devices filtered by name
func (d *Discovery) GetDevicesByName(name string) []*model.Device {
	d.mutex.RLock()
	defer d.mutex.RUnlock()

	devcies := make([]*model.Device, 0)
	for _, device := range d.devices {
		if device.Name == name {
			devcies = append(devcies, device)
		}
	}
	return devcies
}

func (d *Discovery) broadcastPresence() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	// Get the best local IP to advertise
	localIP, err := getOutboundIP()
	if err != nil {
		fmt.Printf("Error getting outbound IP: %v\n", err)
		localIP = net.IPv4zero
	}

	announcement := model.Device{
		ID:      d.serviceID,
		Name:    d.serviceName,
		Address: localIP.String(),
	}

	for {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			announcementData, err := json.Marshal(announcement)
			if err != nil {
				fmt.Printf("Error marshaling announcement: %v\n", err)
				continue
			}

			_, err = d.broadcaster.Write(announcementData)
			if err != nil {
				fmt.Printf("Error broadcasting presence: %v\n", err)
			}
		}
	}
}

func (d *Discovery) receiveAnnouncements() {
	buffer := make([]byte, 2048)

	for {
		select {
		case <-d.ctx.Done():
			return
		default:
			d.listener.SetReadDeadline(time.Now().Add(1 * time.Second))
			n, addr, err := d.listener.ReadFromUDP(buffer)

			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					// This is just a timeout, continue
					continue
				}
				fmt.Printf("Error receiving announcement: %v\n", err)
				continue
			}

			var device model.Device
			err = json.Unmarshal(buffer[:n], &device)
			if err != nil {
				fmt.Printf("Error unmarshaling announcement: %v\n", err)
				continue
			}

			// Filter out our own announcements
			// if device.ID == d.serviceID {
			// 	device.Local = true
			// }

			// If source address doesn't match the announced address, update it
			// This helps with NAT and containers
			if net.ParseIP(device.Address).IsUnspecified() {
				device.Address = addr.IP.String()
			}

			d.mutex.Lock()
			// existingDevice, exists := d.devices[device.ID]
			// if exists {
			// 	// existingDevice.Address = device.Address
			// 	// existingDevice.LastSeenAt = time.Now()
			// 	// existingDevice.Local = device.Local
			// 	// existingDevice.Syncing = true
			// } else {

			// }
			d.devices[device.ID] = &device
			d.memstore.SaveDevice(d.devices[device.ID])
			d.mutex.Unlock()
		}
	}
}

func (d *Discovery) cleanupStaleDevices() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			d.mutex.Lock()
			for id, device := range d.devices {
				// Remove services not seen for 60 seconds
				if time.Since(device.LastSeen) > 60*time.Second {
					delete(d.devices, id)
				}
			}
			d.mutex.Unlock()
		}
	}
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

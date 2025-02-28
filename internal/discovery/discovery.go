package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"
)

// ServiceInfo contains information about a service instance
type ServiceInfo struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Address    string            `json:"address"`
	Port       int               `json:"port"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	LastSeenAt time.Time         `json:"lastSeenAt"`
}

// Discovery handles service discovery in local network
type Discovery struct {
	serviceID     string
	serviceName   string
	port          int
	metadata      map[string]string
	broadcastAddr string
	broadcastPort int
	services      map[string]ServiceInfo
	listener      *net.UDPConn
	broadcaster   *net.UDPConn
	mutex         sync.RWMutex
	ctx           context.Context
	cancel        context.CancelFunc
}

// NewDiscovery creates a new service discovery instance
func NewDiscovery(serviceID, serviceName string, port int, metadata map[string]string) *Discovery {
	ctx, cancel := context.WithCancel(context.Background())
	return &Discovery{
		serviceID:     serviceID,
		serviceName:   serviceName,
		port:          port,
		metadata:      metadata,
		broadcastAddr: "255.255.255.255",
		broadcastPort: 9876, // Choose a port unlikely to be in use
		services:      make(map[string]ServiceInfo),
		ctx:           ctx,
		cancel:        cancel,
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
	go d.cleanupStaleServices()

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

// GetServices returns all discovered services
func (d *Discovery) GetServices() []ServiceInfo {
	d.mutex.RLock()
	defer d.mutex.RUnlock()

	services := make([]ServiceInfo, 0, len(d.services))
	for _, service := range d.services {
		services = append(services, service)
	}
	return services
}

// GetServicesByName returns services filtered by name
func (d *Discovery) GetServicesByName(name string) []ServiceInfo {
	d.mutex.RLock()
	defer d.mutex.RUnlock()

	services := make([]ServiceInfo, 0)
	for _, service := range d.services {
		if service.Name == name {
			services = append(services, service)
		}
	}
	return services
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

	announcement := ServiceInfo{
		ID:       d.serviceID,
		Name:     d.serviceName,
		Address:  localIP.String(),
		Port:     d.port,
		Metadata: d.metadata,
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

			var service ServiceInfo
			err = json.Unmarshal(buffer[:n], &service)
			if err != nil {
				fmt.Printf("Error unmarshaling announcement: %v\n", err)
				continue
			}

			// Filter out our own announcements
			if service.ID == d.serviceID {
				continue
			}

			// If source address doesn't match the announced address, update it
			// This helps with NAT and containers
			if net.ParseIP(service.Address).IsUnspecified() {
				service.Address = addr.IP.String()
			}

			service.LastSeenAt = time.Now()

			d.mutex.Lock()
			d.services[service.ID] = service
			d.mutex.Unlock()
		}
	}
}

func (d *Discovery) cleanupStaleServices() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			d.mutex.Lock()
			for id, service := range d.services {
				// Remove services not seen for 60 seconds
				if time.Since(service.LastSeenAt) > 60*time.Second {
					delete(d.services, id)
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

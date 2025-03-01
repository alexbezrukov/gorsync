package memstore

import (
	"gorsync/internal/model"
	"sync"
)

type MemStore struct {
	// syncServers map[string]model.SyncServer
	devices map[string]*model.Device
	mu      sync.RWMutex
}

func NewMemStore() *MemStore {
	return &MemStore{
		// syncServers: make(map[string]model.SyncServer),
		devices: make(map[string]*model.Device),
	}
}

func (m *MemStore) GetDevice(deviceID string) (*model.Device, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	device, exists := m.devices[deviceID]
	return device, exists
}

func (m *MemStore) GetDevices() []*model.Device {
	m.mu.RLock()
	defer m.mu.RUnlock()

	devices := make([]*model.Device, 0, len(m.devices))
	for _, device := range m.devices {
		devices = append(devices, device)
	}
	return devices
}

func (m *MemStore) SaveDevice(device *model.Device) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// fmt.Println("Saving device:", device.ID, device.Settings)
	m.devices[device.ID] = device
	// fmt.Println("Devices in memory:", m.devices) // Debugging
}

// func (m *MemStore) GetSyncServer(deviceID string) (model.SyncServer, bool) {
// 	m.mu.Lock()
// 	defer m.mu.Unlock()
// 	server, exists := m.syncServers[deviceID]
// 	return server, exists
// }

// func (m *MemStore) SaveSyncServer(deviceID string, server model.SyncServer) {
// 	m.mu.Lock()
// 	defer m.mu.Unlock()
// 	m.syncServers[deviceID] = server
// }

// func (m *MemStore) DeleteSyncServer(deviceID string) {
// 	m.mu.Lock()
// 	defer m.mu.Unlock()
// 	delete(m.syncServers, deviceID)
// }

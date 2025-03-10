package memstore

import (
	"gorsync/internal/model"
	"sync"
)

type MemStore struct {
	devices   map[string]*model.Device
	devicesMu sync.RWMutex
}

func NewMemStore() *MemStore {
	return &MemStore{
		devices: make(map[string]*model.Device),
	}
}

func (m *MemStore) GetDevice(deviceID string) (*model.Device, bool) {
	m.devicesMu.RLock()
	defer m.devicesMu.RUnlock()
	device, exists := m.devices[deviceID]
	return device, exists
}

func (m *MemStore) GetDevices() []*model.Device {
	m.devicesMu.RLock()
	defer m.devicesMu.RUnlock()

	devices := make([]*model.Device, 0, len(m.devices))
	for _, device := range m.devices {
		devices = append(devices, device)
	}
	return devices
}

func (m *MemStore) SaveDevice(device *model.Device) {
	m.devicesMu.Lock()
	defer m.devicesMu.Unlock()

	m.devices[device.ID] = device
}

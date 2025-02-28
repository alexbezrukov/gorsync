package model

type PeerInfo struct {
	IP       string `json:"ip"`
	Port     int    `json:"port"`
	DeviceID string `json:"device_id"`
}

type ServiceRegistration struct {
	ID      string   `json:"ID"`
	Name    string   `json:"Name"`
	Port    int      `json:"Port"`
	Tags    []string `json:"Tags"`
	Address string   `json:"Address"`
}

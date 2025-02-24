package model

type PeerInfo struct {
	IP       string `json:"ip"`
	Port     int    `json:"port"`
	DeviceID string `json:"device_id"`
}

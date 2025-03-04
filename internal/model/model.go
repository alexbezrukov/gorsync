package model

import (
	"encoding/json"
	"time"
)

const (
	MsgTypeRegister = "register"
)

// Message represents the WebSocket message structure
type Message struct {
	Type     string          `json:"type"`
	DeviceID string          `json:"deviceId,omitempty"`
	Payload  json.RawMessage `json:"payload,omitempty"`
	TargetID string          `json:"targetId,omitempty"`
}

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

type AppSettings struct {
	AppName           string `json:"appName"`
	Port              int    `json:"port"`
	DefaultSyncDir    string `json:"defaultSyncDir"`
	SyncInterval      int    `json:"syncInterval"`
	AutoSync          bool   `json:"autoSync"`
	MaxTransferSize   int    `json:"maxTransferSize"`
	ConnectionTimeout int    `json:"connectionTimeout"`
	DebugMode         bool   `json:"debugMode"`
}

// FileInfo represents file or directory details
type FileInfo struct {
	Name         string     `json:"name"`
	Path         string     `json:"path"`
	IsDir        bool       `json:"isDir"`
	Size         int64      `json:"size"`
	LastModified time.Time  `json:"lastModified"`
	Children     []FileInfo `json:"children,omitempty"`
}

// DirectoryResponse represents the API response structure
type DirectoryResponse struct {
	Contents []FileInfo `json:"contents"`
}

type Transfer struct {
	ID         string  `json:"id"`
	FileName   string  `json:"fileName"`
	FilePath   string  `json:"filePath"`
	FileSize   int64   `json:"fileSize"`
	Direction  string  `json:"direction"`
	DeviceID   string  `json:"deviceId"`
	DeviceName string  `json:"deviceName"`
	StartTime  string  `json:"startTime"`
	EndTime    *string `json:"endTime,omitempty"`
	Status     string  `json:"status"`
	Progress   float64 `json:"progress"`
	Speed      *int64  `json:"speed,omitempty"`
}

type FileMetadata struct {
	Path      string    `json:"path"`
	Size      int64     `json:"size"`
	ModTime   time.Time `json:"mod_time"`
	CheckSum  string    `json:"checksum"`
	IsDeleted bool      `json:"is_deleted"`
}

type SyncMessage struct {
	Type     string       `json:"type"`
	DeviceID string       `json:"device_id"`
	Metadata FileMetadata `json:"metadata"`
	Data     []byte       `json:"data,omitempty"`
}

// Device contains information about a device
type Device struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Address    string            `json:"address"`
	Port       int               `json:"port"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	LastSeenAt time.Time         `json:"lastSeenAt"`
	Local      bool              `json:"local"`
	Syncing    bool              `json:"syncing"`
	Settings   DeviceSettings    `json:"settings"`
}

// DeviceSettings represents the settings of a device
type DeviceSettings struct {
	SyncInterval     int    `json:"syncInterval"`
	AutoSync         bool   `json:"autoSync"`
	BandwidthLimit   int    `json:"bandwidthLimit"`
	ExcludeFileTypes string `json:"excludeFileTypes"`
}

// type SyncServer struct {
// 	IsRunning bool `json:"isRunning"`
// }

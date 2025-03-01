package api

import (
	"context"
	"encoding/json"
	"fmt"
	"gorsync/internal/memstore"
	"gorsync/internal/model"
	"gorsync/internal/server"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gorilla/mux"
)

var (
	settings = model.AppSettings{
		AppName:           "MyApp",
		Port:              9001,
		DefaultSyncDir:    "/sync",
		SyncInterval:      60,
		AutoSync:          true,
		MaxTransferSize:   100,
		ConnectionTimeout: 30,
		DebugMode:         false,
	}

	settingsMutex sync.Mutex
)

var transfers = []model.Transfer{
	{ID: "1", FileName: "file1.txt", FilePath: "/path/to/file1.txt", FileSize: 1024, Direction: "upload", DeviceID: "device1", DeviceName: "Laptop", StartTime: "2024-07-10T10:00:00Z", Status: "in_progress", Progress: 50},
}

var mu sync.Mutex

type APIServer struct {
	httpServer *http.Server
	sync       *server.SyncServer
	cancel     context.CancelFunc
	ctx        context.Context
	memstore   *memstore.MemStore
}

func NewAPIServer(
	sync *server.SyncServer,
	memstore *memstore.MemStore,
) *APIServer {
	s := &APIServer{
		sync:     sync,
		memstore: memstore,
	}
	r := mux.NewRouter()
	r.Use(corsMiddleware, loggingMiddleware)

	// Handle OPTIONS requests globally
	r.PathPrefix("/api/").Methods("OPTIONS").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.WriteHeader(http.StatusNoContent)
	})

	r.HandleFunc("/api/transfers", getTransfersHandler).Methods("GET")
	r.HandleFunc("/api/transfers/{id}/cancel", cancelTransferHandler).Methods("POST")
	r.HandleFunc("/api/transfers/{id}/pause", pauseTransferHandler).Methods("POST")
	r.HandleFunc("/api/transfers/{id}/resume", resumeTransferHandler).Methods("POST")

	r.HandleFunc("/api/devices", s.getDevicesHandler).Methods("GET")
	r.HandleFunc("/api/devices/{deviceId}/settings", s.getDeviceSettingsHandler).Methods("GET")
	r.HandleFunc("/api/devices/{deviceId}/settings", s.updateDeviceSettingsHandler).Methods("POST")
	r.HandleFunc("/api/devices/{deviceId}/{action}", s.syncDeviceHandler).Methods("POST")

	r.HandleFunc("/api/settings", saveSettingsHandler).Methods("PUT")
	r.HandleFunc("/api/settings", fetchSettingsHandler).Methods("GET")

	r.HandleFunc("/api/filesystem/directory", getFilesystemInfo).Methods("GET")

	httpServer := &http.Server{
		Addr:         "0.0.0.0:9001",
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	s.httpServer = httpServer

	return s
}

func (s *APIServer) Start() {
	log.Println("Starting API server on", s.httpServer.Addr)
	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Could not start server: %s", err)
	}
}

func (s *APIServer) Stop(ctx context.Context) {
	log.Println("Shutting down server...")
	if err := s.httpServer.Shutdown(ctx); err != nil {
		log.Fatalf("Server shutdown failed: %s", err)
	}
	log.Println("API server gracefully stopped")
}

func respondWithError(w http.ResponseWriter, code int, message string) {
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func getTransfersHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	mu.Lock()
	defer mu.Unlock()
	json.NewEncoder(w).Encode(transfers)
}

func updateTransferStatus(w http.ResponseWriter, r *http.Request, status string) {
	vars := mux.Vars(r)
	transferID := vars["id"]

	mu.Lock()
	defer mu.Unlock()

	for i, t := range transfers {
		if t.ID == transferID {
			transfers[i].Status = status
			w.WriteHeader(http.StatusOK)
			return
		}
	}

	http.Error(w, "Transfer not found", http.StatusNotFound)
}

func cancelTransferHandler(w http.ResponseWriter, r *http.Request) {
	updateTransferStatus(w, r, "cancelled")
}
func pauseTransferHandler(w http.ResponseWriter, r *http.Request) {
	updateTransferStatus(w, r, "paused")
}
func resumeTransferHandler(w http.ResponseWriter, r *http.Request) {
	updateTransferStatus(w, r, "in_progress")
}

func saveSettingsHandler(w http.ResponseWriter, r *http.Request) {
	var updatedSettings model.AppSettings
	if err := json.NewDecoder(r.Body).Decode(&updatedSettings); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	settingsMutex.Lock()
	settings = updatedSettings
	settingsMutex.Unlock()
	w.WriteHeader(http.StatusOK)
}

func fetchSettingsHandler(w http.ResponseWriter, r *http.Request) {
	settingsMutex.Lock()
	defer settingsMutex.Unlock()
	json.NewEncoder(w).Encode(settings)
}

func (s *APIServer) getDevicesHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.memstore.GetDevices())
}

func getFilesystemInfo(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		respondWithError(w, http.StatusBadRequest, "Path is required")
		return
	}

	cleanPath, err := filepath.Abs(path)
	if err != nil || !filepath.IsAbs(cleanPath) {
		respondWithError(w, http.StatusBadRequest, "Invalid path")
		return
	}

	contents, err := getDirectoryContents(cleanPath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Cannot read directory")
		return
	}

	json.NewEncoder(w).Encode(contents)
}

func (s *APIServer) syncDeviceHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	deviceID := vars["deviceId"]
	action := vars["action"] // "start-sync" or "stop-sync"

	fmt.Println("action", action)

	device, exists := s.memstore.GetDevice(deviceID)
	if !exists {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	if device.Local {
		// Update syncing status
		if action == "start-sync" {
			if s.cancel != nil {
				s.cancel() // Cancel any existing sync process before starting a new one
			}

			s.ctx, s.cancel = context.WithCancel(context.Background())
			device.Syncing = true

			go func() {
				err := s.sync.Start(s.ctx)
				if err != nil {
					log.Fatal("Sync server error:", err)
				}
			}()
		} else if action == "stop-sync" {
			if s.cancel != nil {
				s.cancel() // Cancel the running sync process
			}
			device.Syncing = false
			s.sync.Stop()
		} else {
			http.Error(w, "Invalid action", http.StatusBadRequest)
			return
		}
	} else {
		// Forward the request to the remote device
		url := fmt.Sprintf("http://%s:%d/api/devices/%s/%s", device.Address, device.Port, deviceID, action)

		req, err := http.NewRequest("POST", url, nil)
		if err != nil {
			http.Error(w, "Failed to create request", http.StatusInternalServerError)
			return
		}

		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to forward request: %v", err), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body) // Copy response body from remote device to the client
		return
	}

	s.memstore.SaveDevice(device)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(device)
}

func getDirectoryContents(dirPath string) ([]model.FileInfo, error) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, err
	}

	var contents []model.FileInfo
	for _, entry := range entries {
		fileInfo, err := entry.Info()
		if err != nil {
			continue
		}

		contents = append(contents, model.FileInfo{
			Name:         entry.Name(),
			Path:         filepath.Join(dirPath, entry.Name()),
			IsDir:        entry.IsDir(),
			Size:         fileInfo.Size(),
			LastModified: fileInfo.ModTime(),
		})
	}
	return contents, nil
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s %s", r.RemoteAddr, r.Method, r.URL)
		next.ServeHTTP(w, r)
	})
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// Get device settings
func (s *APIServer) getDeviceSettingsHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	deviceID := vars["deviceId"]

	device, exists := s.memstore.GetDevice(deviceID)
	if !exists {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	fmt.Println("getDeviceSettingsHandler device", device.ID, device.Settings)

	// if device.Settings == nil {
	// 	device.Settings = model.DeviceSettings{
	// 		SyncInterval:     30,
	// 		AutoSync:         false,
	// 		BandwidthLimit:   30,
	// 		ExcludeFileTypes: ".git",
	// 	}
	// }

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(device.Settings)
}

// Update device settings
func (s *APIServer) updateDeviceSettingsHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	deviceID := vars["deviceId"]

	device, exists := s.memstore.GetDevice(deviceID)
	if !exists {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	var settings model.DeviceSettings
	if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	device.Settings = settings

	s.memstore.SaveDevice(device)

	fmt.Println("device", device)

	w.WriteHeader(http.StatusOK)
}

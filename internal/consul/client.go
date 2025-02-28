package consul

import (
	"bytes"
	"encoding/json"
	"fmt"
	"gorsync/internal/model"
	"log"
	"net/http"
)

const consulAddress = "http://host.docker.internal:8500" // Use "localhost:8500" if running locally

type ConsulClient struct{}

func NewClient() (*ConsulClient, error) {
	return &ConsulClient{}, nil
}

func (c *ConsulClient) RegisterService(address string, deviceID string, port int) error {
	consulAddr := fmt.Sprintf("%s/v1/agent/service/register", consulAddress)
	data := model.ServiceRegistration{
		ID:      deviceID,
		Name:    "gorsync",
		Port:    port,
		Tags:    []string{"sync"},
		Address: "host.docker.internal",
	}

	jsonData, _ := json.Marshal(data)
	req, err := http.NewRequest(http.MethodPut, consulAddr, bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to register service: %s", resp.Status)
	}

	log.Println("Service registered successfully")

	log.Printf("Registered service %s in Consul", deviceID)
	return nil
}

func (c *ConsulClient) DeregisterService(serviceID string) error {
	consulAddr := fmt.Sprintf("%s/v1/agent/service/deregister/%s", consulAddress, serviceID)

	req, err := http.NewRequest(http.MethodPut, consulAddr, nil)
	if err != nil {
		return err
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to deregister service: %s", resp.Status)
	}

	log.Printf("Deregistered service %s from Consul", serviceID)
	return nil
}

func (c *ConsulClient) DiscoverPeers() ([]model.ServiceRegistration, error) {
	// Get services with the "sync" tag
	consulAddr := fmt.Sprintf("%s/v1/catalog/service/gorsync?tag=sync", consulAddress)

	req, err := http.NewRequest("GET", consulAddr, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch services: %s", resp.Status)
	}

	var services []struct {
		ID             string   `json:"ID"`
		Node           string   `json:"Node"`
		Address        string   `json:"Address"`
		ServiceID      string   `json:"ServiceID"`
		ServiceName    string   `json:"ServiceName"`
		ServiceAddress string   `json:"ServiceAddress"`
		ServicePort    int      `json:"ServicePort"`
		ServiceTags    []string `json:"ServiceTags"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&services); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	var result []model.ServiceRegistration
	for _, service := range services {
		// Use ServiceAddress if available, else use the node Address
		address := service.ServiceAddress
		if address == "" {
			address = service.Address
		}

		result = append(result, model.ServiceRegistration{
			ID:      service.ServiceID,
			Name:    service.ServiceName,
			Address: address,
			Port:    service.ServicePort,
			Tags:    service.ServiceTags,
		})
	}

	return result, nil
}

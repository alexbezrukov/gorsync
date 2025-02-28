package consul

import (
	"bytes"
	"encoding/json"
	"fmt"
	"gorsync/internal/model"
	"log"
	"net/http"
)

const consulAddress = "http://localhost:8500" // Use "localhost:8500" if running locally

type ConsulClient struct{}

func NewClient() (*ConsulClient, error) {
	return &ConsulClient{}, nil
}

func (c *ConsulClient) RegisterService(serviceID string, port int) error {
	data := model.ServiceRegistration{
		ID:      serviceID,
		Name:    "gorsync",
		Port:    port,
		Tags:    []string{"sync"},
		Address: "127.0.0.1",
	}

	jsonData, _ := json.Marshal(data)
	req, err := http.NewRequest(http.MethodPut, consulAddress, bytes.NewBuffer(jsonData))
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

	log.Printf("Registered service %s in Consul", serviceID)
	return nil
}

func (c *ConsulClient) DeregisterService(serviceID string) {
	http.Get(fmt.Sprintf("%s/v1/agent/service/deregister/%s", consulAddress, serviceID))
	log.Printf("Deregistered service %s from Consul", serviceID)
}

func (c *ConsulClient) DiscoverPeers(serviceName string) ([]model.ServiceRegistration, error) {
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/v1/agent/services", consulAddress), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch services: %s", resp.Status)
	}

	// Decode the JSON response
	var services map[string]model.ServiceRegistration
	if err := json.NewDecoder(resp.Body).Decode(&services); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Extract relevant service info
	var result []model.ServiceRegistration
	for _, service := range services {
		if service.Name == serviceName {
			result = append(result, service)
		}
	}

	return result, nil

}

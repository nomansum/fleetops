package consul

import (
	"fmt"
	"os"

	consulapi "github.com/hashicorp/consul/api"
)

// Register registers a service with Consul and returns a deregister func.
//
// CONSUL_HTTP_ADDR env var controls the agent address (default: localhost:8500).
func Register(serviceName, serviceID string, port int) (func() error, error) {
	cfg := consulapi.DefaultConfig()
	if addr := os.Getenv("CONSUL_HTTP_ADDR"); addr != "" {
		cfg.Address = addr
	}

	client, err := consulapi.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("consul: client: %w", err)
	}

	reg := &consulapi.AgentServiceRegistration{
		ID:      serviceID,
		Name:    serviceName,
		Port:    port,
		Tags:    []string{"fleetops", "grpc"},
		Check: &consulapi.AgentServiceCheck{
			GRPC:                           fmt.Sprintf("localhost:%d/%s", port, serviceName),
			Interval:                       "10s",
			Timeout:                        "5s",
			DeregisterCriticalServiceAfter: "30s",
		},
	}

	if err := client.Agent().ServiceRegister(reg); err != nil {
		return nil, fmt.Errorf("consul: register %s: %w", serviceName, err)
	}

	deregister := func() error {
		return client.Agent().ServiceDeregister(serviceID)
	}

	return deregister, nil
}

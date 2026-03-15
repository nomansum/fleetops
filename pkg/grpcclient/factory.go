// Package grpcclient provides a factory for creating gRPC client connections
// with service mesh features wired in:
//
//   - Service discovery: addresses resolved via Consul DNS (dns:///host:port)
//   - Load balancing:    round-robin across all healthy instances from DNS
//   - Retries:           exponential backoff on UNAVAILABLE / RESOURCE_EXHAUSTED
//   - mTLS:              handled by Envoy sidecar in mesh mode; insecure in plain dev
//   - Observability:     OTel trace context propagated on every outbound call
package grpcclient

import (
	"context"
	"fmt"
	"os"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer/roundrobin"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

// defaultServiceConfig encodes load-balancing + retry policy as JSON.
// - round_robin: distributes calls across all healthy DNS A-record addresses
// - retryPolicy: up to 5 attempts with exponential backoff on transient errors
func defaultServiceConfig() string {
	return fmt.Sprintf(`{
		"loadBalancingConfig": [{"%s":{}}],
		"methodConfig": [{
			"name": [{}],
			"retryPolicy": {
				"maxAttempts": 5,
				"initialBackoff": "0.5s",
				"maxBackoff": "10s",
				"backoffMultiplier": 2.0,
				"retryableStatusCodes": ["UNAVAILABLE", "RESOURCE_EXHAUSTED"]
			},
			"waitForReady": true,
			"timeout": "30s"
		}]
	}`, roundrobin.Name)
}

// Dial opens a gRPC connection to addr.
//
// addr format:
//   - Plain dev:  "localhost:9001"
//   - Consul DNS: "dns:///iam-service.service.consul:9001"  (set via *_SERVICE_ADDR env vars)
//   - Mesh mode:  "127.0.0.1:9001" (Envoy sidecar on loopback handles mTLS + routing)
func Dial(ctx context.Context, addr string) (*grpc.ClientConn, error) {
	conn, err := grpc.NewClient(
		addr,

		// ── Transport ─────────────────────────────────────────────────────
		// Plain insecure in dev. In mesh mode Envoy sidecar terminates mTLS,
		// so the connection from app → sidecar is still plaintext on loopback.
		grpc.WithTransportCredentials(insecure.NewCredentials()),

		// ── Load balancing + Retry ────────────────────────────────────────
		grpc.WithDefaultServiceConfig(defaultServiceConfig()),

		// ── Observability ─────────────────────────────────────────────────
		// Injects trace context into gRPC metadata on every outbound call.
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),

		// ── Connection health ─────────────────────────────────────────────
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                30 * time.Second,
			Timeout:             10 * time.Second,
			PermitWithoutStream: true,
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("grpcclient: dial %s: %w", addr, err)
	}

	return conn, nil
}

// DialWithTimeout wraps Dial with a 30s context timeout.
func DialWithTimeout(addr string) (*grpc.ClientConn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return Dial(ctx, addr)
}

// Addresses holds the gRPC endpoints for every service.
//
// Plain dev:  set to "localhost:9001" etc.
// Consul DNS: set to "dns:///iam-service.service.consul:9001" for automatic
//
//	discovery and round-robin load balancing.
type Addresses struct {
	IAM          string
	Driver       string
	Dispatch     string
	Tracking     string
	Billing      string
	Notification string
}

// AddressesFromEnv reads service addresses from environment variables.
// In docker-compose these point to container names; in mesh mode to Consul DNS.
func AddressesFromEnv() Addresses {
	return Addresses{
		IAM:          envOr("IAM_SERVICE_ADDR", "localhost:9001"),
		Driver:       envOr("DRIVER_SERVICE_ADDR", "localhost:9002"),
		Dispatch:     envOr("DISPATCH_SERVICE_ADDR", "localhost:9003"),
		Tracking:     envOr("TRACKING_SERVICE_ADDR", "localhost:9004"),
		Billing:      envOr("BILLING_SERVICE_ADDR", "localhost:9005"),
		Notification: envOr("NOTIFICATION_SERVICE_ADDR", "localhost:9006"),
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

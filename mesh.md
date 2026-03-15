# Service Mesh — Consul + Envoy

This document explains how FleetOps services discover each other, stay healthy, and (in full mesh mode) enforce mTLS and traffic policies. It covers what is **actually running in dev mode** and what switches on in **mesh mode**.

---

## 1. Architecture Overview

### Dev Mode (what runs today)

```
┌─────────────────────────────────────────────────────────────────┐
│  Docker network  172.28.0.0/24                                  │
│                                                                  │
│  ┌─────────────┐   gRPC (plaintext)   ┌──────────────────────┐  │
│  │ api-gateway │ ──────────────────▶  │  iam-service :9001   │  │
│  │   :8080     │ ──────────────────▶  │  driver-service:9002 │  │
│  └─────────────┘   Docker DNS         │  dispatch-service    │  │
│                    resolves names      │  tracking-service    │  │
│                    e.g.               │  billing-service     │  │
│  ┌─────────────┐  "driver-service"    │  notification-svc    │  │
│  │   Consul    │ ◀─── register ────── └──────────────────────┘  │
│  │ 172.28.0.100│   (HTTP API :8500)                             │
│  │   :8500 UI  │                                                 │
│  │   :8600 DNS │   health-check every 10s                       │
│  └─────────────┘   gRPC probe ──────▶ {host}:{port}/{svcName}  │
└─────────────────────────────────────────────────────────────────┘
```

Services **register** with Consul so they appear in the UI and their gRPC health is monitored, but they **dial each other** using Docker's embedded DNS (`127.0.0.11:53`) — `driver-service:9002` resolves straight to the container IP.

### Mesh Mode (aspirational — one env var away)

```
┌─────────────────────────────────────────────────────────────────┐
│  ┌───────────────────────────────────────────────────────────┐  │
│  │  api-gateway                                              │  │
│  │  app (plaintext) ──▶ Envoy sidecar ──mTLS──▶             │  │
│  └───────────────────────────────────────────────────────────┘  │
│                              │                                   │
│                    Consul DNS :8600                              │
│              dns:///driver-service.service.consul:9002           │
│                              │                                   │
│  ┌───────────────────────────▼───────────────────────────────┐  │
│  │  driver-service                                            │  │
│  │  ──mTLS──▶ Envoy sidecar ──▶ app (plaintext on loopback) │  │
│  └───────────────────────────────────────────────────────────┘  │
│                                                                  │
│  Consul enforces service intentions (deny-by-default mTLS ACL)  │
└─────────────────────────────────────────────────────────────────┘
```

| Component | Dev mode | Mesh mode |
|-----------|----------|-----------|
| Service discovery | Docker DNS | Consul DNS (`*.service.consul`) |
| Transport security | Plaintext | mTLS via Envoy sidecar |
| Load balancing | Docker round-robin | gRPC `dns:///` + round-robin |
| Traffic policies | None | Consul service intentions |
| Retries | gRPC service config | gRPC service config (same) |

---

## 2. Consul — Service Registration

Every gRPC service registers itself at startup using `pkg/consul/registrar.go`.

### The registrar (`pkg/consul/registrar.go`)

```go
// Register registers a service with Consul and returns a deregister func.
//
// host is the address Consul uses to reach the service (e.g. the Docker service
// name). CONSUL_HTTP_ADDR env var controls the agent address (default: localhost:8500).
func Register(serviceName, serviceID, host string, port int) (func() error, error) {
    cfg := consulapi.DefaultConfig()
    if addr := os.Getenv("CONSUL_HTTP_ADDR"); addr != "" {
        cfg.Address = addr   // e.g. "consul:8500" in Docker Compose
    }

    client, err := consulapi.NewClient(cfg)
    if err != nil {
        return nil, fmt.Errorf("consul: client: %w", err)
    }

    reg := &consulapi.AgentServiceRegistration{
        ID:      serviceID,   // unique instance ID, e.g. "iam-service"
        Name:    serviceName, // logical service name, e.g. "iam-service"
        Address: host,        // address Consul advertises — the Docker service name
        Port:    port,        // gRPC port, e.g. 9001
        Tags:    []string{"fleetops", "grpc"},
        Check: &consulapi.AgentServiceCheck{
            // Consul calls this gRPC health endpoint every 10s
            // Format: host:port/fully.qualified.ServiceName  (or just serviceName for grpc.health.v1)
            GRPC:                           fmt.Sprintf("%s:%d/%s", host, port, serviceName),
            Interval:                       "10s",
            Timeout:                        "5s",
            DeregisterCriticalServiceAfter: "30s", // auto-remove if down > 30s
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
```

**Key parameters:**

| Parameter | Value in Docker Compose | Purpose |
|-----------|------------------------|---------|
| `host` | `SERVICE_HOST=iam-service` | Address Consul uses for health checks and advertises to clients |
| `CONSUL_HTTP_ADDR` | `consul:8500` | Where the Consul agent HTTP API is |
| `port` | `GRPC_PORT=9001` | gRPC listener port |

### How each service calls it (`services/iam-service/cmd/main.go`)

```go
// After the gRPC server goroutine is started:

portInt, _ := strconv.Atoi(port)
deregister, err := consulpkg.Register(
    "iam-service",                       // service name in Consul catalog
    "iam-service",                       // instance ID (unique per replica)
    envOr("SERVICE_HOST", "localhost"),  // Docker service name, resolved by Consul
    portInt,
)
if err != nil {
    log.Warn().Err(err).Msg("consul registration failed")
} else {
    defer deregister() // removes service from Consul on graceful shutdown
}
```

The pattern is identical across all six gRPC services (iam, driver, dispatch, tracking, billing, notification). The `log.Warn` (not `Fatal`) means a service continues running even if Consul is temporarily unreachable.

### What appears in the Consul UI

After `docker compose up --build`, visit `http://localhost:8500`:

- **Services tab** — all 6 gRPC services listed with tag `grpc`
- **Health checks** — green (passing) once gRPC health probe responds
- **Nodes tab** — single node `fleetops-dev` (Consul runs in `-dev` mode)

---

## 3. gRPC Client Factory — `pkg/grpcclient/factory.go`

Every outbound gRPC connection should be opened via this factory. It wires retry, load balancing, and tracing in one place.

### The `Dial` function

```go
// Dial opens a gRPC connection to addr.
//
// addr formats:
//   Plain dev:   "iam-service:9001"              (Docker DNS)
//   Consul DNS:  "dns:///iam-service.service.consul:9001"
//   Mesh mode:   "127.0.0.1:9001"  (Envoy sidecar on loopback)
func Dial(ctx context.Context, addr string) (*grpc.ClientConn, error) {
    conn, err := grpc.NewClient(
        addr,

        // Transport: plaintext in dev AND in mesh mode
        // In mesh mode, Envoy sidecar terminates mTLS; app→sidecar is loopback plaintext.
        grpc.WithTransportCredentials(insecure.NewCredentials()),

        // Load balancing + retry (see defaultServiceConfig below)
        grpc.WithDefaultServiceConfig(defaultServiceConfig()),

        // Inject OTel trace context into every outbound gRPC call
        grpc.WithStatsHandler(otelgrpc.NewClientHandler()),

        // Keep connection alive between requests
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
```

### The service config — retry + load balancing

```go
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
```

**What this does:**

- `round_robin` — when the `dns:///` resolver returns multiple A-records (e.g., multiple replicas), requests distribute across all of them
- `maxAttempts: 5` — up to 4 retries after the initial attempt
- Backoff: 0.5s → 1s → 2s → 4s → 10s (capped)
- Only retries `UNAVAILABLE` (connection lost) and `RESOURCE_EXHAUSTED` (overloaded) — not `NOT_FOUND`, `INVALID_ARGUMENT`, etc.
- `waitForReady: true` — waits for the connection to become ready rather than failing fast on transient network issues

### Address loading

```go
// Addresses holds gRPC endpoints for every downstream service.
type Addresses struct {
    IAM          string
    Driver       string
    Dispatch     string
    Tracking     string
    Billing      string
    Notification string
}

// AddressesFromEnv reads from environment variables.
// In docker-compose these are Docker service names; in mesh mode they become Consul DNS.
func AddressesFromEnv() Addresses {
    return Addresses{
        IAM:          envOr("IAM_SERVICE_ADDR",          "localhost:9001"),
        Driver:       envOr("DRIVER_SERVICE_ADDR",       "localhost:9002"),
        Dispatch:     envOr("DISPATCH_SERVICE_ADDR",     "localhost:9003"),
        Tracking:     envOr("TRACKING_SERVICE_ADDR",     "localhost:9004"),
        Billing:      envOr("BILLING_SERVICE_ADDR",      "localhost:9005"),
        Notification: envOr("NOTIFICATION_SERVICE_ADDR", "localhost:9006"),
    }
}
```

---

## 4. Service Discovery — Dev Mode vs Mesh Mode

### Dev mode (current)

The `*_SERVICE_ADDR` env vars in `docker-compose.yml` point to Docker service names:

```yaml
# docker-compose.yml — api-gateway environment
IAM_SERVICE_ADDR:          iam-service:9001
DRIVER_SERVICE_ADDR:       driver-service:9002
DISPATCH_SERVICE_ADDR:     dispatch-service:9003
TRACKING_SERVICE_ADDR:     tracking-service:9004
BILLING_SERVICE_ADDR:      billing-service:9005
NOTIFICATION_SERVICE_ADDR: notification-service:9006
```

Docker's embedded DNS resolver (`127.0.0.11:53`) inside each container resolves `iam-service` to the container IP on the `172.28.0.0/24` network. No Consul DNS involved.

**Request path:**
```
api-gateway → dial("iam-service:9001")
           → Docker DNS 127.0.0.11:53
           → 172.28.0.x:9001 (iam-service container)
```

### Mesh mode (one env var change)

Switch all `*_SERVICE_ADDR` values to `dns:///` Consul DNS format:

```yaml
IAM_SERVICE_ADDR: dns:///iam-service.service.consul:9001
DRIVER_SERVICE_ADDR: dns:///driver-service.service.consul:9002
```

The `dns:///` prefix tells the gRPC resolver to do a proper DNS A-record lookup (not just one-shot connect), enabling round-robin across multiple instances.

**Request path:**
```
api-gateway → dial("dns:///iam-service.service.consul:9001")
           → Consul DNS 172.28.0.100:8600
           → returns A-records for all healthy iam-service instances
           → round-robin across instances
```

The Consul agent at `172.28.0.100` exposes DNS on port `8600`. Its `-advertise=172.28.0.100` flag ensures services can reach it from within the Docker network.

For containers to query Consul DNS by default, configure Docker's daemon DNS or add to the container:
```yaml
dns: [172.28.0.100]
dns_search: [service.consul]
```

---

## 5. Consul Health Checks

Consul actively probes each registered service using the gRPC health protocol (`grpc.health.v1.Health/Check`).

### What the probe looks like

```
Consul agent (172.28.0.100)
    → gRPC connect to iam-service:9001
    → call grpc.health.v1.Health/Check with service="iam-service"
    → expects HealthCheckResponse_SERVING
    → every 10s, timeout 5s
```

### How each service implements it

Every gRPC service registers a standard health server before serving:

```go
// services/iam-service/cmd/main.go

healthSvc := health.NewServer()

// Mark this service as SERVING — Consul's gRPC health probe checks this status
healthSvc.SetServingStatus("iam-service", healthpb.HealthCheckResponse_SERVING)

healthpb.RegisterHealthServer(grpcServer, healthSvc)
reflection.Register(grpcServer)  // also enables grpcurl introspection
```

**Health state machine:**

| Consul state | Meaning |
|---|---|
| `passing` | gRPC probe returned `SERVING` within 5s |
| `critical` | Probe failed or timed out — service excluded from DNS results |
| `(deregistered)` | Still critical after 30s — entry removed from catalog |

### Changing status at runtime

To temporarily remove a service from rotation (e.g., during a drain):

```go
healthSvc.SetServingStatus("iam-service", healthpb.HealthCheckResponse_NOT_SERVING)
```

Consul will mark it critical within one check interval (10s) and stop routing traffic to it via DNS.

---

## 6. Envoy + Mesh Mode

> Envoy sidecars are **not yet deployed** in this repository. This section documents the intended architecture.

### How Envoy fits in

In mesh mode, each service pod/container runs an Envoy proxy sidecar. All inbound and outbound gRPC traffic goes through Envoy rather than directly to/from the app:

```
[api-gateway app]
    │  plaintext gRPC (loopback)
    ▼
[Envoy sidecar]  ←── Consul Connect issues mTLS certificates
    │  mTLS gRPC over network
    ▼
[Envoy sidecar]  ←── validates peer cert against Consul CA
    │  plaintext gRPC (loopback)
    ▼
[iam-service app]
```

**Why `insecure.NewCredentials()` is correct in both modes:**

The Go application always connects to its local Envoy on loopback (`127.0.0.1`). The app never sees certificates. Envoy handles all the mTLS on behalf of the app. So the transport credential setting does not change between dev and mesh mode.

### Service intentions (traffic rules)

Consul service intentions define which services may call which. In mesh mode, Envoy enforces these at the mTLS layer — a certificate from a service not listed in the intention is rejected even if network routing allows it.

Example intention (applies via `consul config write`):

```hcl
Kind = "service-intentions"
Name = "iam-service"
Sources = [
  { Name = "api-gateway",     Action = "allow" },
  { Name = "document-worker", Action = "allow" },
  { Name = "*",               Action = "deny"  },
]
```

This means only `api-gateway` and `document-worker` may call `iam-service`. Any other service that dials `iam-service` gets an mTLS rejection at the Envoy layer, even before the gRPC request reaches the application.

**Currently:** Intentions are defined but not enforced (no Envoy deployed). In dev mode, all gRPC connections succeed regardless.

### Enabling mesh mode

1. Add Envoy sidecar containers to `docker-compose.yml` (one per service)
2. Configure Consul Connect CA bootstrap
3. Change `*_SERVICE_ADDR` to `dns:///service.service.consul:port`
4. Apply service intentions: `consul config write deployments/consul/intentions-*.hcl`

---

## 7. OTel Observability

Every gRPC hop — both client-side and server-side — is instrumented with OpenTelemetry.

### Server-side (each gRPC service)

```go
grpcServer := grpc.NewServer(
    grpc.StatsHandler(otelgrpc.NewServerHandler()),
)
```

This creates a span for every incoming gRPC call, extracts trace context from metadata if the caller propagated one, and records call duration and status code.

### Client-side (outbound dials)

```go
grpc.WithStatsHandler(otelgrpc.NewClientHandler())
```

This injects the current trace context into outgoing gRPC metadata so the downstream service can continue the same trace.

### Export path

```
gRPC service (span created)
    │  OTLP gRPC  :4317
    ▼
otel-collector
    │  OTLP gRPC
    ▼
Jaeger (traces)       → http://localhost:16686
Prometheus (metrics)  → http://localhost:9090
Loki (logs)           → via Grafana at http://localhost:3000
```

The collector configuration is at `deployments/otel/config.yaml`. The `OTEL_EXPORTER_OTLP_ENDPOINT` env var on every service points to `otel-collector:4317`.

---

## Quick Reference

| URL | What it shows |
|-----|---------------|
| `http://localhost:8500` | Consul UI — service catalog, health checks |
| `http://localhost:16686` | Jaeger — distributed traces |
| `http://localhost:9090` | Prometheus — raw metrics |
| `http://localhost:3000` | Grafana — dashboards, logs, traces |
| `http://localhost:8080` | API Gateway — REST entry point |

| Service | gRPC port | Default address env var |
|---------|-----------|------------------------|
| iam-service | 9001 | `IAM_SERVICE_ADDR` |
| driver-service | 9002 | `DRIVER_SERVICE_ADDR` |
| dispatch-service | 9003 | `DISPATCH_SERVICE_ADDR` |
| tracking-service | 9004 | `TRACKING_SERVICE_ADDR` |
| billing-service | 9005 | `BILLING_SERVICE_ADDR` |
| notification-service | 9006 | `NOTIFICATION_SERVICE_ADDR` |

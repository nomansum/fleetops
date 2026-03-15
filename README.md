# FleetOps

A production-realistic **real-time fleet operations platform** for last-mile logistics companies. Demonstrates a Go microservices architecture using gRPC/Protobuf, Domain-Driven Design (DDD), NATS JetStream, PostGIS, TimescaleDB, Consul service mesh, and a full observability stack — all wired together in a single `docker compose up`.

---

## Table of Contents

- [Architecture Overview](#architecture-overview)
- [Bounded Contexts (DDD)](#bounded-contexts-ddd)
- [Services](#services)
- [Infrastructure](#infrastructure)
- [Folder Structure](#folder-structure)
- [Getting Started](#getting-started)
- [API Reference](#api-reference)
- [NATS Event Streams](#nats-event-streams)
- [Background Jobs](#background-jobs)
- [Auth Strategy](#auth-strategy)
- [Service Mesh](#service-mesh)
- [Observability](#observability)
- [Development](#development)

---

## Architecture Overview

```
                            ┌─────────────────────────────────────────────────┐
                            │                    Clients                      │
                            └───────────────────────┬─────────────────────────┘
                                                    │ HTTP/REST
                                        ┌───────────▼───────────┐
                                        │      api-gateway       │ :8080
                                        │  Gin · JWT · RateLimit │
                                        └──┬──┬──┬──┬──┬────────┘
                                           │  │  │  │  │  gRPC
              ┌────────────────────────────┘  │  │  │  └─────────────────────┐
              │           ┌───────────────────┘  │  └──────────┐             │
              ▼           ▼                       ▼             ▼             ▼
       iam-service  driver-service       dispatch-service  tracking-service  billing-service
         :9001         :9002                  :9003            :9004           :9005
        (PG-IAM)     (PG-DRV)           (PG-DISPATCH         (TimescaleDB)   (PG-BILL)
                                          + PostGIS)
              │           │                    │                │              │
              └───────────┴────────────────────┴────────────────┴──────────────┘
                                               │
                                    ┌──────────▼──────────┐
                                    │   NATS JetStream     │
                                    │  FLEET_EVENTS stream │
                                    └──┬──────────────┬────┘
                                       │              │
                            ┌──────────▼──┐    ┌──────▼──────────┐
                            │notification │    │ document-worker  │
                            │  -service   │    │  (cron jobs)     │
                            │   :9006     │    └─────────────────┘
                            └─────────────┘
```

All services register with **Consul** for health checks and service discovery. Inter-service gRPC calls use **round-robin DNS load balancing** with **automatic retry** on transient failures.

---

## Bounded Contexts (DDD)

Each service owns a single bounded context with clean DDD layers:

| Context | Root Aggregate | Key Entities / Value Objects |
|---|---|---|
| **IAM** | `User`, `Tenant` | `Role`, `ServiceAccount`, `RefreshToken` |
| **Driver Management** | `Driver` | `Vehicle`, `Document`, `DriverStatus`, `LicenseNumber` |
| **Dispatch** | `DeliveryOrder` | `Waypoint`, `DispatchAssignment`, `TimeWindow`, `Address` |
| **Live Tracking** | `TrackingSession` | `GeoPoint`, `GeofenceZone`, `LocationPing` |
| **Billing** | `Invoice` | `LineItem`, `PricingRule`, `Money`, `InvoiceStatus` |
| **Notification** | *(event-driven, no aggregate)* | `NotificationTemplate`, `Channel` |

**DDD layers per service:**

```
internal/
├── domain/          # Pure Go — aggregates, value objects, repo interfaces, domain events
├── application/     # Command/query handlers (CQRS-light) — orchestrates domain + infra
├── infrastructure/  # Postgres repos, NATS publishers, external clients
└── interfaces/      # gRPC server adapters
```

---

## Services

| Service | Protocol | Port | Database | Description |
|---|---|---|---|---|
| `api-gateway` | HTTP/REST (Gin) | 8080 | none | JWT auth, rate limiting, reverse proxy to gRPC services |
| `iam-service` | gRPC | 9001 | PostgreSQL | Users, tenants, JWT issuance, service accounts |
| `driver-service` | gRPC | 9002 | PostgreSQL | Driver profiles, vehicles, compliance documents |
| `dispatch-service` | gRPC | 9003 | PostgreSQL + PostGIS | Orders, assignments, geospatial driver discovery |
| `tracking-service` | gRPC (streaming) | 9004 | TimescaleDB | Real-time location ingestion, route history, geofences |
| `billing-service` | gRPC | 9005 | PostgreSQL | Invoices, pricing rules, payment recording |
| `notification-service` | gRPC | 9006 | Redis | Multi-channel notifications (email/SMS/push) |
| `document-worker` | *(background)* | — | reads driver DB | Cron-based document expiry scanner, stale assignment reaper |

---

## Infrastructure

### Containers

| Container | Port | Technology | Purpose |
|---|---|---|---|
| `postgres-iam` | 5432 | PostgreSQL 16 | IAM data |
| `postgres-driver` | 5433 | PostgreSQL 16 | Driver data |
| `postgres-dispatch` | 5434 | PostgreSQL 16 + PostGIS | Dispatch + geo queries |
| `postgres-billing` | 5435 | PostgreSQL 16 | Billing data |
| `timescaledb` | 5436 | TimescaleDB 2.x (PG16) | Time-series location pings |
| `redis-shared` | 6379 | Redis 7 | JWT deny-list, rate limiting, ValidateToken cache |
| `redis-notify` | 6380 | Redis 7 | Notification queue (Redis Streams) |
| `nats` | 4222 / 8222 | NATS 2.10 JetStream | Async event bus |
| `consul` | 8500 / 8600 | Consul 1.19 | Service discovery, health checks, service intentions |
| `otel-collector` | 4317 / 4318 | OpenTelemetry Collector | Trace/metric/log aggregation |
| `jaeger` | 16686 | Jaeger | Distributed tracing UI |
| `prometheus` | 9090 | Prometheus | Metrics scraping |
| `grafana` | 3000 | Grafana | Dashboards (traces + metrics + logs) |
| `loki` | 3100 | Grafana Loki | Log aggregation |
| `promtail` | — | Grafana Promtail | Docker container log shipping → Loki |
| `pgadmin` | 5050 | pgAdmin 4 | DB UI *(dev profile only)* |

**Total: 22 containers**

---

## Folder Structure

```
fleetops/
├── go.work                          # Go workspace (all modules)
├── go.work.sum
├── docker-compose.yml               # Full 22-container stack
├── docker-compose.mesh.yml          # Consul Connect + Envoy sidecar overlay
├── Makefile
│
├── proto/                           # Protobuf source (buf.yaml + buf.gen.yaml)
│   ├── iam/v1/iam.proto
│   ├── driver/v1/driver.proto
│   ├── dispatch/v1/dispatch.proto
│   ├── tracking/v1/tracking.proto
│   ├── billing/v1/billing.proto
│   └── notification/v1/notification.proto
│
├── gen/go/                          # Generated gRPC stubs (never hand-edited)
│
├── pkg/                             # Shared libraries
│   ├── auth/                        # JWT RS256 middleware
│   ├── grpcclient/                  # Dial factory: round-robin LB, retry, OTel
│   ├── logger/                      # zerolog structured logging
│   ├── tracer/                      # OpenTelemetry OTLP setup
│   ├── metrics/                     # Prometheus helpers
│   ├── nats/                        # JetStream publisher + durable consumer
│   ├── postgres/                    # pgxpool connection factory
│   ├── consul/                      # Service registration
│   └── errors/                      # Domain errors → gRPC status mapping
│
├── services/
│   ├── api-gateway/
│   ├── iam-service/
│   ├── driver-service/
│   ├── dispatch-service/
│   ├── tracking-service/
│   ├── billing-service/
│   ├── notification-service/
│   └── document-worker/
│
├── deployments/
│   ├── consul/intentions.hcl        # Service-to-service traffic rules
│   ├── otel/config.yaml             # OTel Collector pipelines
│   ├── prometheus/prometheus.yml    # Scrape targets
│   ├── grafana/
│   │   ├── datasources/             # Prometheus, Jaeger, Loki
│   │   └── dashboards/              # FleetOps overview dashboard
│   ├── loki/loki.yaml
│   └── promtail/promtail.yaml
│
└── secrets/
    ├── generate.sh                  # One-time RSA key generation
    ├── jwt_private.pem              # (git-ignored)
    └── jwt_public.pem               # (git-ignored)
```

---

## Getting Started

### Prerequisites

- Docker 24+ with Compose v2
- `openssl` (for key generation)
- Go 1.25+ (for local development)
- `buf` CLI (for proto regeneration): `brew install bufbuild/buf/buf`

### 1. Generate JWT keys

```bash
bash secrets/generate.sh
```

This creates `secrets/jwt_private.pem` and `secrets/jwt_public.pem` (RSA-4096). They are git-ignored and mounted as Docker secrets.

### 2. Start the full stack

```bash
docker compose up -d
```

Wait ~30 seconds for all health checks to pass:

```bash
docker compose ps
```

### 3. Smoke test

```bash
# Health check
curl http://localhost:8080/health

# Register a tenant admin
curl -s -X POST http://localhost:8080/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"email":"admin@acme.com","password":"secret123","tenant_name":"Acme Logistics"}'

# Login and capture JWT
TOKEN=$(curl -s -X POST http://localhost:8080/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"admin@acme.com","password":"secret123"}' | jq -r '.access_token')

# Create a driver
curl -s -X POST http://localhost:8080/v1/drivers \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"Alice Smith","phone":"+1555000001","license_number":"DL-123456"}'

# Create a delivery order
curl -s -X POST http://localhost:8080/v1/orders \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "pickup_address":"123 Warehouse Ave",
    "dropoff_address":"456 Customer St",
    "pickup_lat":37.7749,"pickup_lon":-122.4194,
    "dropoff_lat":37.3382,"dropoff_lon":-121.8863
  }'
```

### 4. Open dashboards

| Tool | URL | Purpose |
|---|---|---|
| Grafana | http://localhost:3000 | Metrics, logs, traces |
| Jaeger | http://localhost:16686 | Distributed traces |
| Prometheus | http://localhost:9090 | Raw metrics |
| NATS Monitor | http://localhost:8222 | JetStream consumers |
| Consul UI | http://localhost:8500 | Service mesh & health |
| pgAdmin | http://localhost:5050 | DB explorer *(requires `--profile devtools`)* |

Start pgAdmin:
```bash
docker compose --profile devtools up -d pgadmin
```

---

## API Reference

All authenticated routes require `Authorization: Bearer <access_token>`.
Rate limit: **300 requests / minute** per user (sliding window via Redis).

### Auth (public)

| Method | Path | Description |
|---|---|---|
| `POST` | `/v1/auth/register` | Register tenant + admin user, returns tokens |
| `POST` | `/v1/auth/login` | Authenticate, returns `access_token` + `refresh_token` |

### Drivers

| Method | Path | Description |
|---|---|---|
| `POST` | `/v1/drivers` | Create a driver (tenant-scoped) |
| `GET` | `/v1/drivers` | List available drivers (page_size: 20) |
| `GET` | `/v1/drivers/:id` | Get driver by ID |
| `GET` | `/v1/drivers/nearby?lat=&lon=&radius_km=` | Find drivers within radius (default 5 km) |

### Orders

| Method | Path | Description |
|---|---|---|
| `POST` | `/v1/orders` | Create delivery order |
| `GET` | `/v1/orders` | List all orders (tenant-scoped) |
| `GET` | `/v1/orders/:id` | Get order by ID |
| `POST` | `/v1/orders/:id/assign` | Assign a driver to an order |

### Tracking

| Method | Path | Description |
|---|---|---|
| `GET` | `/v1/tracking/drivers/:id/location` | Get driver's last known location |

### Billing

| Method | Path | Description |
|---|---|---|
| `GET` | `/v1/billing/invoices/:id` | Get invoice by ID |

### gRPC (direct, internal use)

Each service also exposes gRPC reflection. Use `grpcurl` or Postman:

```bash
grpcurl -plaintext localhost:9003 list
grpcurl -plaintext localhost:9003 dispatch.v1.DispatchService/FindNearbyDrivers
```

---

## NATS Event Streams

### Stream: `FLEET_EVENTS`  subjects: `fleet.>`

| Subject | Published by | Consumed by | Payload |
|---|---|---|---|
| `fleet.driver.status.changed` | driver-service | notification-service | `DriverStatusChangedEvent` |
| `fleet.order.assigned` | dispatch-service | notification-service | `OrderAssignedEvent` |
| `fleet.order.delivered` | dispatch-service | billing-service (×5 workers) | `OrderDeliveredEvent` |
| `fleet.order.failed` | document-worker | notification-service | `OrderFailedEvent` |
| `fleet.location.updated` | tracking-service | dispatch-service | `LocationUpdatedEvent` |
| `fleet.document.expiring` | document-worker | notification-service | `DocumentExpiringEvent` |
| `fleet.invoice.issued` | billing-service | notification-service | `InvoiceIssuedEvent` |

### Stream: `NOTIFICATIONS`  subjects: `notify.>`

| Subject | Published by | Consumed by |
|---|---|---|
| `notify.email.*` | notification-service | email transport |
| `notify.sms.*` | notification-service | SMS transport |
| `notify.push.*` | notification-service | push transport |

All consumers are **durable** with at-least-once delivery. Consumer names follow the pattern `<service>-<subject-slug>`.

---

## Background Jobs

Handled by `document-worker` using `robfig/cron` with `SkipIfStillRunning` policy:

| Job | Schedule (UTC) | Description |
|---|---|---|
| **Document Expiry Scanner** | `0 2 * * *` (02:00 daily) | Finds documents expiring within 30 days, publishes `fleet.document.expiring`. Auto-suspends drivers with already-expired docs. |
| **Stale Assignment Reaper** | `*/5 * * * *` (every 5 min) | Finds ASSIGNED orders stuck > 2 hours, marks them TIMED_OUT, deactivates the assignment, publishes `fleet.order.failed`. |

`billing-service` also runs:

| Job | Trigger | Description |
|---|---|---|
| **Invoice Auto-Generation** | `fleet.order.delivered` (5 workers) | Creates invoice: base $5.00 + $0.50/km, issues with 30-day due date. |
| **Overdue Invoice Checker** | `0 6 * * *` (06:00 daily) | Marks unpaid past-due invoices as OVERDUE. |

TimescaleDB handles a DB-native continuous aggregate (`hourly_route_summary`) that rolls up location pings hourly — no Go code required.

---

## Auth Strategy

### External (client → api-gateway)

- **JWT RS256** — 15-minute access tokens + 7-day refresh tokens
- `ValidateToken` result cached in Redis (SHA256-keyed, 30s TTL) to reduce iam-service RPC load
- Revoked JTIs stored in Redis SET; checked on every validation

### Internal (service → service)

| Mode | Mechanism |
|---|---|
| **Dev (default)** | Pre-shared `INTERNAL_API_KEY` gRPC unary/stream interceptor |
| **Mesh mode** | Consul Connect mTLS via Envoy sidecar (enable with `docker-compose.mesh.yml` overlay) |

`document-worker` exchanges a ServiceAccount credential with iam-service for a short-lived internal JWT, refreshed every 10 minutes via a background goroutine.

---

## Service Mesh

### Dev mode (default)

- **Service discovery:** Consul DNS (`<service>.service.consul`)
- **Load balancing:** gRPC round-robin via `dns:///` resolver in `pkg/grpcclient`
- **Retry policy:** 5 attempts with exponential backoff (1s → 2s → 4s → 8s cap) on `UNAVAILABLE` and `RESOURCE_EXHAUSTED`
- **Health checks:** gRPC health protocol registered on every service; Consul monitors via HTTP

### Mesh mode (`docker-compose.mesh.yml`)

```bash
make up-mesh
```

Adds Envoy sidecar containers that handle:

- **mTLS** — automatic certificate rotation via Consul CA
- **Traffic control** — service intentions defined in `deployments/consul/intentions.hcl` enforce which services may call which
- **Observability** — Envoy emits request metrics and traces automatically

### Service Intentions (Consul)

| Source | Destination | Action |
|---|---|---|
| api-gateway | iam-service, driver-service, dispatch-service, tracking-service, billing-service, notification-service | allow |
| dispatch-service | driver-service, tracking-service | allow |
| billing-service | dispatch-service | allow |
| document-worker | driver-service | allow |
| *(all others)* | *(all)* | deny |

---

## Observability

### Tracing

- Every gRPC server uses `otelgrpc.NewServerHandler()` and every client uses `otelgrpc.NewClientHandler()`
- `api-gateway` uses `otelgin.Middleware("api-gateway")`
- W3C TraceContext propagation across all service boundaries
- Traces exported via OTLP gRPC to **otel-collector → Jaeger**
- View traces: http://localhost:16686

### Metrics

- Services expose Prometheus metrics via OTel OTLP → collector's Prometheus exporter (`:8889`)
- Prometheus scrapes collector + NATS + Consul
- View metrics: http://localhost:9090
- Grafana dashboard at http://localhost:3000 (folder: **FleetOps**)

  Dashboard panels:
  - gRPC request rate per service
  - gRPC error rate by status code
  - gRPC P99 latency
  - NATS JetStream message throughput
  - Active orders / available drivers / location pings / invoices issued

### Logging

- All services use `rs/zerolog` structured JSON
- Every log line includes `service`, `trace_id`, `tenant_id`
- Promtail picks up all Docker container logs, parses JSON fields, ships to **Loki**
- View logs in Grafana → Explore → Loki data source
- Loki logs are correlated with Jaeger traces via `trace_id` field

---

## Development

### Makefile targets

```bash
make proto        # Regenerate gRPC stubs from .proto files (requires buf)
make build        # go build all services
make test         # go test ./... all services
make lint         # golangci-lint all services
make up           # docker compose up -d
make up-mesh      # bring up with Consul Connect + Envoy mesh
make down         # docker compose down -v (destroys volumes)
make logs         # follow all container logs
make tidy         # go mod tidy all modules
```

### Regenerating proto stubs

```bash
# Install buf
brew install bufbuild/buf/buf

# Generate
make proto
```

Stubs land in `gen/go/` and are committed alongside source.

### Adding a new service

1. Create `services/<name>/` with `cmd/main.go` + DDD layers
2. Add a `go.mod` with `replace` directives for local modules:
   ```
   replace github.com/fleetops/gen => ../../gen/go
   replace github.com/fleetops/pkg => ../../pkg
   ```
3. Add the module to `go.work`
4. Add a `Dockerfile` (copy the pattern from any existing service)
5. Add the service to `docker-compose.yml` and `Makefile`'s `SERVICES` list
6. Optionally add a Consul service intention in `deployments/consul/intentions.hcl`

### Local development without Docker

Each service reads configuration from environment variables:

```bash
export DATABASE_URL="postgres://fleetops:fleetops_secret@localhost:5432/iam_db?sslmode=disable"
export NATS_URL="nats://localhost:4222"
export GRPC_PORT="9001"
export JWT_PRIVATE_KEY_PATH="./secrets/jwt_private.pem"
export JWT_PUBLIC_KEY_PATH="./secrets/jwt_public.pem"
export LOG_LEVEL="debug"

cd services/iam-service && go run ./cmd/main.go
```

---

## Tech Stack

| Category | Technology |
|---|---|
| Language | Go 1.25 |
| HTTP Framework | Gin |
| RPC | gRPC / Protobuf (buf) |
| Message Broker | NATS JetStream |
| Databases | PostgreSQL 16, PostGIS 3.4, TimescaleDB 2.x |
| Cache | Redis 7 |
| Service Mesh | Consul 1.19 + Envoy |
| Tracing | OpenTelemetry → Jaeger |
| Metrics | OpenTelemetry → Prometheus → Grafana |
| Logging | zerolog → Promtail → Loki → Grafana |
| Auth | JWT RS256 (golang-jwt/jwt) |
| Containerization | Docker Compose v2 |
| Scheduler | robfig/cron v3 |

.PHONY: proto build lint test up down logs

SERVICES := api-gateway iam-service driver-service dispatch-service \
            tracking-service billing-service notification-service document-worker

# ── Proto ────────────────────────────────────────────────────────────────────
proto:
	cd proto && buf generate

proto-lint:
	cd proto && buf lint

# ── Build ────────────────────────────────────────────────────────────────────
build:
	@for svc in $(SERVICES); do \
		echo "Building $$svc..."; \
		cd services/$$svc && go build ./cmd/... && cd ../..; \
	done

# ── Test ─────────────────────────────────────────────────────────────────────
test:
	@for svc in $(SERVICES); do \
		echo "Testing $$svc..."; \
		cd services/$$svc && go test ./... && cd ../..; \
	done

# ── Lint ─────────────────────────────────────────────────────────────────────
lint:
	@for svc in $(SERVICES); do \
		cd services/$$svc && golangci-lint run ./... && cd ../..; \
	done

# ── Docker ───────────────────────────────────────────────────────────────────
up:
	docker compose up -d

up-mesh:
	docker compose -f docker-compose.yml -f docker-compose.mesh.yml up -d

down:
	docker compose down -v

logs:
	docker compose logs -f

# ── DB Migrations ─────────────────────────────────────────────────────────────
migrate-up:
	@for svc in iam driver dispatch billing; do \
		echo "Migrating $$svc..."; \
		migrate -path services/$$svc-service/internal/infrastructure/postgres/migrations \
		        -database "$${DATABASE_URL_$$(echo $$svc | tr a-z A-Z)}" up; \
	done

# ── Seed ─────────────────────────────────────────────────────────────────────
seed:
	./scripts/seed.sh

# ── Helpers ──────────────────────────────────────────────────────────────────
tidy:
	@for svc in $(SERVICES); do \
		cd services/$$svc && go mod tidy && cd ../..; \
	done
	cd pkg && go mod tidy
	cd gen/go && go mod tidy

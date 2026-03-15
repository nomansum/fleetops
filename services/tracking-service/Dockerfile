FROM golang:1.25-alpine AS builder
WORKDIR /workspace

COPY go.work go.work.sum ./
COPY gen/go/ gen/go/
COPY pkg/ pkg/

# Copy go.mod files for all workspace modules so go.work can load them
COPY services/api-gateway/go.mod services/api-gateway/go.mod
COPY services/billing-service/go.mod services/billing-service/go.mod
COPY services/dispatch-service/go.mod services/dispatch-service/go.mod
COPY services/document-worker/go.mod services/document-worker/go.mod
COPY services/driver-service/go.mod services/driver-service/go.mod
COPY services/iam-service/go.mod services/iam-service/go.mod
COPY services/notification-service/go.mod services/notification-service/go.mod

COPY services/tracking-service/ services/tracking-service/

RUN go build -trimpath -ldflags="-s -w" -o /out/tracking-service ./services/tracking-service/cmd/main.go

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /out/tracking-service /tracking-service
ENTRYPOINT ["/tracking-service"]

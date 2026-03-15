FROM golang:1.25-alpine AS builder
WORKDIR /workspace

COPY go.work go.work.sum ./
COPY gen/go/ gen/go/
COPY pkg/ pkg/
COPY services/api-gateway/ services/api-gateway/

RUN go build -trimpath -ldflags="-s -w" -o /out/api-gateway ./services/api-gateway/cmd/main.go

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /out/api-gateway /api-gateway
ENTRYPOINT ["/api-gateway"]

FROM golang:1.25-alpine AS builder
WORKDIR /workspace

COPY go.work go.work.sum ./
COPY gen/go/ gen/go/
COPY pkg/ pkg/
COPY services/billing-service/ services/billing-service/

RUN go build -trimpath -ldflags="-s -w" -o /out/billing-service ./services/billing-service/cmd/main.go

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /out/billing-service /billing-service
ENTRYPOINT ["/billing-service"]

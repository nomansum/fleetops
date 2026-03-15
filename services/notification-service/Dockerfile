FROM golang:1.25-alpine AS builder
WORKDIR /workspace

COPY go.work go.work.sum ./
COPY gen/go/ gen/go/
COPY pkg/ pkg/
COPY services/notification-service/ services/notification-service/

RUN go build -trimpath -ldflags="-s -w" -o /out/notification-service ./services/notification-service/cmd/main.go

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /out/notification-service /notification-service
ENTRYPOINT ["/notification-service"]

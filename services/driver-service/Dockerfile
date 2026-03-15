FROM golang:1.25-alpine AS builder
WORKDIR /workspace

COPY go.work go.work.sum ./
COPY gen/go/ gen/go/
COPY pkg/ pkg/
COPY services/driver-service/ services/driver-service/

RUN go build -trimpath -ldflags="-s -w" -o /out/driver-service ./services/driver-service/cmd/main.go

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /out/driver-service /driver-service
ENTRYPOINT ["/driver-service"]

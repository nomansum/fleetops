FROM golang:1.25-alpine AS builder
WORKDIR /workspace

COPY go.work go.work.sum ./
COPY gen/go/ gen/go/
COPY pkg/ pkg/
COPY services/tracking-service/ services/tracking-service/

RUN go build -trimpath -ldflags="-s -w" -o /out/tracking-service ./services/tracking-service/cmd/main.go

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /out/tracking-service /tracking-service
ENTRYPOINT ["/tracking-service"]

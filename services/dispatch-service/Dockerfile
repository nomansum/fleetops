FROM golang:1.25-alpine AS builder
WORKDIR /workspace

COPY go.work go.work.sum ./
COPY gen/go/ gen/go/
COPY pkg/ pkg/
COPY services/dispatch-service/ services/dispatch-service/

RUN go build -trimpath -ldflags="-s -w" -o /out/dispatch-service ./services/dispatch-service/cmd/main.go

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /out/dispatch-service /dispatch-service
ENTRYPOINT ["/dispatch-service"]

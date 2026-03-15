FROM golang:1.25-alpine AS builder
WORKDIR /workspace

COPY go.work go.work.sum ./
COPY gen/go/ gen/go/
COPY pkg/ pkg/
COPY services/iam-service/ services/iam-service/

RUN go build -trimpath -ldflags="-s -w" -o /out/iam-service ./services/iam-service/cmd/main.go

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /out/iam-service /iam-service
ENTRYPOINT ["/iam-service"]

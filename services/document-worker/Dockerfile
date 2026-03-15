FROM golang:1.25-alpine AS builder
WORKDIR /workspace

COPY go.work go.work.sum ./
COPY gen/go/ gen/go/
COPY pkg/ pkg/
COPY services/document-worker/ services/document-worker/

RUN go build -trimpath -ldflags="-s -w" -o /out/document-worker ./services/document-worker/cmd/main.go

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /out/document-worker /document-worker
ENTRYPOINT ["/document-worker"]

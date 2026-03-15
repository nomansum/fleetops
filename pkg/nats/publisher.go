package nats

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Connect creates a NATS connection with JetStream enabled.
// NATS_URL env var controls the server address (default: nats://localhost:4222).
func Connect() (*nats.Conn, jetstream.JetStream, error) {
	url := os.Getenv("NATS_URL")
	if url == "" {
		url = nats.DefaultURL
	}

	nc, err := nats.Connect(url,
		nats.Name("fleetops"),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2_000_000_000), // 2s
	)
	if err != nil {
		return nil, nil, fmt.Errorf("nats: connect: %w", err)
	}

	js, err := jetstream.New(nc)
	if err != nil {
		nc.Drain()
		return nil, nil, fmt.Errorf("nats: jetstream: %w", err)
	}

	return nc, js, nil
}

// Publisher wraps JetStream to publish structured events as JSON.
type Publisher struct {
	js jetstream.JetStream
}

// NewPublisher returns a Publisher backed by the given JetStream context.
func NewPublisher(js jetstream.JetStream) *Publisher {
	return &Publisher{js: js}
}

// Publish serialises payload to JSON and publishes it on subject.
func (p *Publisher) Publish(ctx context.Context, subject string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("nats: marshal: %w", err)
	}

	_, err = p.js.Publish(ctx, subject, data)
	if err != nil {
		return fmt.Errorf("nats: publish %s: %w", subject, err)
	}

	return nil
}

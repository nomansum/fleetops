package nats

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nats-io/nats.go/jetstream"
)

// EventPublisher publishes driver domain events to NATS JetStream.
type EventPublisher struct {
	js jetstream.JetStream
}

func NewEventPublisher(js jetstream.JetStream) *EventPublisher {
	return &EventPublisher{js: js}
}

// Publish serialises payload to JSON and publishes to the given subject.
func (p *EventPublisher) Publish(ctx context.Context, subject string, payload any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("nats: marshal: %w", err)
	}
	_, err = p.js.Publish(ctx, subject, b)
	if err != nil {
		return fmt.Errorf("nats: publish %s: %w", subject, err)
	}
	return nil
}

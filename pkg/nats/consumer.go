package nats

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// Handler is called for each received message.
// If it returns an error the message is nack-ed (with backoff); otherwise ack-ed.
type Handler func(ctx context.Context, subject string, data []byte) error

// ConsumeConfig describes a JetStream consumer.
type ConsumeConfig struct {
	Stream   string
	Consumer string // durable name
	Subjects []string
	Workers  int // parallel goroutines
}

// Consume creates (or re-attaches to) a durable push consumer and starts Workers goroutines.
// The returned cancel func stops consumption.
func Consume(ctx context.Context, js jetstream.JetStream, cfg ConsumeConfig, h Handler) (context.CancelFunc, error) {
	stream, err := js.Stream(ctx, cfg.Stream)
	if err != nil {
		return nil, fmt.Errorf("nats: stream %s: %w", cfg.Stream, err)
	}

	cons, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:        cfg.Consumer,
		FilterSubjects: cfg.Subjects,
		AckPolicy:      jetstream.AckExplicitPolicy,
		MaxDeliver:     5,
		BackOff:        []time.Duration{time.Second, 5 * time.Second, 30 * time.Second},
	})
	if err != nil {
		return nil, fmt.Errorf("nats: create consumer %s: %w", cfg.Consumer, err)
	}

	cctx, cancel := context.WithCancel(ctx)

	workers := cfg.Workers
	if workers < 1 {
		workers = 1
	}

	for range workers {
		go func() {
			for {
				select {
				case <-cctx.Done():
					return
				default:
				}

				msgs, err := cons.Fetch(10)
				if err != nil {
					continue
				}

				for msg := range msgs.Messages() {
					if err := h(cctx, msg.Subject(), msg.Data()); err != nil {
						_ = msg.Nak()
					} else {
						_ = msg.Ack()
					}
				}
			}
		}()
	}

	return cancel, nil
}

// UnmarshalMsg is a helper to decode a raw NATS message body into a typed value.
func UnmarshalMsg[T any](data []byte) (T, error) {
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return v, fmt.Errorf("nats: unmarshal: %w", err)
	}
	return v, nil
}

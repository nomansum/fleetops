// Package nats contains the event consumer for billing-service.
// It subscribes to fleet.order.delivered events and auto-generates invoices.
package nats

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/rs/zerolog"
)

// OrderDeliveredEvent mirrors the event published by dispatch-service.
type OrderDeliveredEvent struct {
	OrderID     string    `json:"order_id"`
	TenantID    string    `json:"tenant_id"`
	DriverID    string    `json:"driver_id"`
	DistanceKM  float64   `json:"distance_km"`
	DeliveredAt time.Time `json:"delivered_at"`
}

// InvoiceCreator is the function called when an order.delivered event arrives.
type InvoiceCreator func(ctx context.Context, event OrderDeliveredEvent) error

// StartOrderDeliveredConsumer creates a durable consumer on FLEET_EVENTS
// and starts workerCount goroutines processing fleet.order.delivered messages.
func StartOrderDeliveredConsumer(ctx context.Context, js jetstream.JetStream, log zerolog.Logger, workerCount int, creator InvoiceCreator) error {
	stream, err := js.Stream(ctx, "FLEET_EVENTS")
	if err != nil {
		return fmt.Errorf("billing nats: stream: %w", err)
	}

	cons, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:        "billing-order-delivered",
		FilterSubjects: []string{"fleet.order.delivered"},
		AckPolicy:      jetstream.AckExplicitPolicy,
		MaxDeliver:     5,
		BackOff:        []time.Duration{1 * time.Second, 5 * time.Second, 30 * time.Second},
	})
	if err != nil {
		return fmt.Errorf("billing nats: create consumer: %w", err)
	}

	for range workerCount {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}

				msgs, err := cons.Fetch(10, jetstream.FetchMaxWait(2*time.Second))
				if err != nil {
					continue
				}

				for msg := range msgs.Messages() {
					var event OrderDeliveredEvent
					if err := json.Unmarshal(msg.Data(), &event); err != nil {
						log.Error().Err(err).Msg("unmarshal order.delivered")
						_ = msg.Nak()
						continue
					}

					if err := creator(ctx, event); err != nil {
						log.Error().Err(err).Str("order_id", event.OrderID).Msg("create invoice")
						_ = msg.Nak()
					} else {
						_ = msg.Ack()
					}
				}
			}
		}()
	}

	return nil
}

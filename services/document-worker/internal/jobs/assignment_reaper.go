package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/rs/zerolog"
)

const subjectOrderFailed = "fleet.order.failed"

// AssignmentReaper finds ASSIGNED orders older than 2 hours and marks them TIMED_OUT.
// It runs every 5 minutes via cron.
type AssignmentReaper struct {
	db  *pgxpool.Pool
	js  jetstream.JetStream
	log zerolog.Logger
}

func NewAssignmentReaper(db *pgxpool.Pool, js jetstream.JetStream, log zerolog.Logger) *AssignmentReaper {
	return &AssignmentReaper{db: db, js: js, log: log}
}

// Run is the cron entry point.
func (r *AssignmentReaper) Run() {
	ctx := context.Background()
	cutoff := time.Now().UTC().Add(-2 * time.Hour)

	rows, err := r.db.Query(ctx, `
		SELECT id, tenant_id FROM orders
		WHERE status = 'ASSIGNED' AND updated_at < $1
		LIMIT 100`, cutoff)
	if err != nil {
		r.log.Error().Err(err).Msg("reaper: query stale assignments")
		return
	}
	defer rows.Close()

	var count int
	for rows.Next() {
		var orderID, tenantID string
		if err := rows.Scan(&orderID, &tenantID); err != nil {
			continue
		}

		if err := r.timeoutOrder(ctx, orderID, tenantID); err != nil {
			r.log.Error().Err(err).Str("order_id", orderID).Msg("reaper: timeout order")
		} else {
			count++
		}
	}

	if count > 0 {
		r.log.Warn().Int("count", count).Msg("assignment reaper: timed out stale orders")
	}
}

func (r *AssignmentReaper) timeoutOrder(ctx context.Context, orderID, tenantID string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE orders SET status='TIMED_OUT', updated_at=NOW() WHERE id=$1`, orderID)
	if err != nil {
		return fmt.Errorf("update order: %w", err)
	}

	// Deactivate assignment
	_, _ = r.db.Exec(ctx, `UPDATE assignments SET active=FALSE WHERE order_id=$1`, orderID)

	event := map[string]any{
		"order_id":    orderID,
		"tenant_id":   tenantID,
		"reason":      "TIMED_OUT",
		"occurred_at": time.Now().UTC().Format(time.RFC3339),
	}
	b, _ := json.Marshal(event)
	_, _ = r.js.Publish(ctx, subjectOrderFailed, b)

	return nil
}

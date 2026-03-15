// Package jobs contains the background cron jobs for document-worker.
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

const (
	subjectDocumentExpiring = "fleet.document.expiring"
	pageSize                = 100
)

// ExpiryScanner scans the driver-service DB for expiring documents
// and publishes fleet.document.expiring events to NATS.
// It also suspends drivers whose documents have already expired.
type ExpiryScanner struct {
	db  *pgxpool.Pool
	js  jetstream.JetStream
	log zerolog.Logger
}

func NewExpiryScanner(db *pgxpool.Pool, js jetstream.JetStream, log zerolog.Logger) *ExpiryScanner {
	return &ExpiryScanner{db: db, js: js, log: log}
}

// Run is called by the cron scheduler.
func (s *ExpiryScanner) Run() {
	ctx := context.Background()
	s.log.Info().Msg("expiry scanner: starting")

	if err := s.scanExpiring(ctx, 30, "WARNING"); err != nil {
		s.log.Error().Err(err).Msg("scan expiring (30d)")
	}
	if err := s.scanExpiring(ctx, 7, "CRITICAL"); err != nil {
		s.log.Error().Err(err).Msg("scan expiring (7d)")
	}
	if err := s.suspendExpired(ctx); err != nil {
		s.log.Error().Err(err).Msg("suspend expired")
	}

	s.log.Info().Msg("expiry scanner: done")
}

func (s *ExpiryScanner) scanExpiring(ctx context.Context, withinDays int, severity string) error {
	cutoffEarly := time.Now().UTC()
	cutoffLate := cutoffEarly.Add(time.Duration(withinDays) * 24 * time.Hour)

	rows, err := s.db.Query(ctx, `
		SELECT d.id, d.driver_id, dr.tenant_id, d.type, d.expiry_date
		FROM documents d
		JOIN drivers dr ON dr.id = d.driver_id
		WHERE d.expiry_date BETWEEN $1 AND $2
		  AND d.verification_status = 'VERIFIED'
		ORDER BY d.expiry_date ASC
		LIMIT $3`,
		cutoffEarly, cutoffLate, pageSize)
	if err != nil {
		return fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var docID, driverID, tenantID, docType string
		var expiryDate time.Time
		if err := rows.Scan(&docID, &driverID, &tenantID, &docType, &expiryDate); err != nil {
			continue
		}

		daysLeft := int(time.Until(expiryDate).Hours() / 24)
		event := map[string]any{
			"driver_id":   driverID,
			"tenant_id":   tenantID,
			"document_id": docID,
			"doc_type":    docType,
			"expiry_date": expiryDate.Format(time.RFC3339),
			"days_left":   daysLeft,
			"severity":    severity,
			"occurred_at": time.Now().UTC().Format(time.RFC3339),
		}
		b, _ := json.Marshal(event)
		if _, err := s.js.Publish(ctx, subjectDocumentExpiring, b); err != nil {
			s.log.Error().Err(err).Str("doc_id", docID).Msg("publish expiring event")
		}
		count++
	}

	s.log.Info().Int("count", count).Int("within_days", withinDays).Str("severity", severity).Msg("expiry scan complete")
	return rows.Err()
}

func (s *ExpiryScanner) suspendExpired(ctx context.Context) error {
	rows, err := s.db.Query(ctx, `
		SELECT DISTINCT dr.id
		FROM drivers dr
		JOIN documents d ON d.driver_id = dr.id
		WHERE d.expiry_date < NOW()
		  AND d.verification_status = 'VERIFIED'
		  AND dr.status != 'SUSPENDED'`)
	if err != nil {
		return fmt.Errorf("query expired: %w", err)
	}
	defer rows.Close()

	var count int
	for rows.Next() {
		var driverID string
		if err := rows.Scan(&driverID); err != nil {
			continue
		}
		_, err := s.db.Exec(ctx,
			`UPDATE drivers SET status='SUSPENDED', updated_at=NOW() WHERE id=$1`, driverID)
		if err != nil {
			s.log.Error().Err(err).Str("driver_id", driverID).Msg("suspend driver")
			continue
		}
		s.log.Warn().Str("driver_id", driverID).Msg("driver suspended: expired document")
		count++
	}

	s.log.Info().Int("suspended", count).Msg("expired document suspension complete")
	return rows.Err()
}

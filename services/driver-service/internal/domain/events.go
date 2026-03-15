package domain

import "time"

// Domain events published to NATS after successful state changes.

const (
	SubjectDriverStatusChanged = "fleet.driver.status.changed"
	SubjectDocumentExpiring    = "fleet.document.expiring"
)

// DriverStatusChangedEvent is published whenever a driver's status changes.
type DriverStatusChangedEvent struct {
	DriverID  string       `json:"driver_id"`
	TenantID  string       `json:"tenant_id"`
	OldStatus DriverStatus `json:"old_status"`
	NewStatus DriverStatus `json:"new_status"`
	OccurredAt time.Time  `json:"occurred_at"`
}

// DocumentExpiringEvent is published by document-worker when a doc is near expiry.
type DocumentExpiringEvent struct {
	DriverID   string       `json:"driver_id"`
	TenantID   string       `json:"tenant_id"`
	DocumentID string       `json:"document_id"`
	DocType    DocumentType `json:"doc_type"`
	ExpiryDate time.Time    `json:"expiry_date"`
	DaysLeft   int          `json:"days_left"`
	Severity   string       `json:"severity"` // "WARNING" | "CRITICAL"
	OccurredAt time.Time    `json:"occurred_at"`
}

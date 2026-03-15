package domain

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ── Value Objects ─────────────────────────────────────────────────────────────

type InvoiceStatus string

const (
	StatusDraft   InvoiceStatus = "DRAFT"
	StatusIssued  InvoiceStatus = "ISSUED"
	StatusPaid    InvoiceStatus = "PAID"
	StatusOverdue InvoiceStatus = "OVERDUE"
	StatusVoid    InvoiceStatus = "VOID"
)

// Money is a value object representing a monetary amount.
type Money struct {
	AmountCents  int64
	CurrencyCode string
}

func (m Money) Add(other Money) (Money, error) {
	if m.CurrencyCode != other.CurrencyCode {
		return Money{}, fmt.Errorf("currency mismatch: %s vs %s", m.CurrencyCode, other.CurrencyCode)
	}
	return Money{AmountCents: m.AmountCents + other.AmountCents, CurrencyCode: m.CurrencyCode}, nil
}

// PricingRule holds a tenant's rate card.
type PricingRule struct {
	ID           string
	TenantID     string
	Name         string
	BaseCharge   Money
	RatePerKM    float64 // cents per km
	SurchargePct float64 // e.g. 0.20 = 20%
	ActiveFrom   time.Time
	ActiveUntil  time.Time
}

// LineItem is an entity within Invoice.
type LineItem struct {
	ID          string
	Description string
	Quantity    int
	UnitPrice   Money
	Total       Money
}

// Invoice is the aggregate root.
type Invoice struct {
	ID        string
	TenantID  string
	OrderID   string
	Reference string
	Status    InvoiceStatus
	LineItems []*LineItem
	Subtotal  Money
	Tax       Money
	Total     Money
	IssuedAt  *time.Time
	DueDate   *time.Time
	PaidAt    *time.Time
	CreatedAt time.Time
}

// InvoiceRepository defines persistence operations.
type InvoiceRepository interface {
	Save(i *Invoice) error
	FindByID(id string) (*Invoice, error)
	FindByOrderID(orderID string) (*Invoice, error)
	List(tenantID string, statusFilter InvoiceStatus, limit, offset int) ([]*Invoice, error)
	Update(i *Invoice) error
	FindOverdue() ([]*Invoice, error)
}

// PricingRuleRepository defines persistence for pricing rules.
type PricingRuleRepository interface {
	Save(r *PricingRule) error
	FindActiveForTenant(tenantID string) (*PricingRule, error)
}

// ── Factories & methods ───────────────────────────────────────────────────────

func NewInvoice(tenantID, orderID string) *Invoice {
	now := time.Now().UTC()
	return &Invoice{
		ID:        uuid.NewString(),
		TenantID:  tenantID,
		OrderID:   orderID,
		Reference: fmt.Sprintf("INV-%d", now.UnixNano()%100_000),
		Status:    StatusDraft,
		CreatedAt: now,
	}
}

func (inv *Invoice) AddLineItem(desc string, qty int, unitPrice Money) {
	total := Money{AmountCents: int64(qty) * unitPrice.AmountCents, CurrencyCode: unitPrice.CurrencyCode}
	inv.LineItems = append(inv.LineItems, &LineItem{
		ID:          uuid.NewString(),
		Description: desc,
		Quantity:    qty,
		UnitPrice:   unitPrice,
		Total:       total,
	})
	inv.recalculate()
}

func (inv *Invoice) recalculate() {
	if len(inv.LineItems) == 0 {
		return
	}
	currency := inv.LineItems[0].Total.CurrencyCode
	var subtotal int64
	for _, li := range inv.LineItems {
		subtotal += li.Total.AmountCents
	}
	inv.Subtotal = Money{AmountCents: subtotal, CurrencyCode: currency}
	taxCents := int64(float64(subtotal) * 0.20) // 20% VAT
	inv.Tax = Money{AmountCents: taxCents, CurrencyCode: currency}
	inv.Total = Money{AmountCents: subtotal + taxCents, CurrencyCode: currency}
}

func (inv *Invoice) Issue(dueDate time.Time) error {
	if inv.Status != StatusDraft {
		return fmt.Errorf("invoice: can only issue a DRAFT invoice, current: %s", inv.Status)
	}
	now := time.Now().UTC()
	inv.Status = StatusIssued
	inv.IssuedAt = &now
	inv.DueDate = &dueDate
	return nil
}

func (inv *Invoice) RecordPayment(paidAt time.Time) error {
	if inv.Status != StatusIssued && inv.Status != StatusOverdue {
		return errors.New("invoice: can only pay an ISSUED or OVERDUE invoice")
	}
	inv.Status = StatusPaid
	inv.PaidAt = &paidAt
	return nil
}

func (inv *Invoice) MarkOverdue() error {
	if inv.Status != StatusIssued {
		return nil
	}
	inv.Status = StatusOverdue
	return nil
}

// ── Domain events ─────────────────────────────────────────────────────────────

const (
	SubjectInvoiceIssued  = "fleet.invoice.issued"
	SubjectInvoiceOverdue = "fleet.invoice.overdue"
)

type InvoiceIssuedEvent struct {
	InvoiceID string    `json:"invoice_id"`
	TenantID  string    `json:"tenant_id"`
	OrderID   string    `json:"order_id"`
	Total     int64     `json:"total_cents"`
	Currency  string    `json:"currency"`
	IssuedAt  time.Time `json:"issued_at"`
}

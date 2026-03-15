package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Plan enumerates tenant subscription tiers.
type Plan string

const (
	PlanFree       Plan = "FREE"
	PlanPro        Plan = "PRO"
	PlanEnterprise Plan = "ENTERPRISE"
)

// Tenant is the root aggregate for a company using FleetOps.
type Tenant struct {
	ID           string
	Name         string
	BillingEmail string
	Plan         Plan
	Active       bool
	CreatedAt    time.Time
}

// NewTenant validates and constructs a Tenant.
func NewTenant(name, billingEmail string) (*Tenant, error) {
	if name == "" {
		return nil, errors.New("tenant: name is required")
	}
	if billingEmail == "" {
		return nil, errors.New("tenant: billing email is required")
	}
	return &Tenant{
		ID:           uuid.NewString(),
		Name:         name,
		BillingEmail: billingEmail,
		Plan:         PlanFree,
		Active:       true,
		CreatedAt:    time.Now().UTC(),
	}, nil
}

// TenantRepository defines persistence operations for the Tenant aggregate.
type TenantRepository interface {
	Save(t *Tenant) error
	FindByID(id string) (*Tenant, error)
}

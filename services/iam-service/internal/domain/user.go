package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Role enumerates the possible user roles.
type Role string

const (
	RoleAdmin      Role = "ADMIN"
	RoleDispatcher Role = "DISPATCHER"
	RoleDriver     Role = "DRIVER"
	RoleFinance    Role = "FINANCE"
)

// User is the IAM aggregate root representing a human operator.
type User struct {
	ID           string
	TenantID     string
	Email        string
	Name         string
	Role         Role
	PasswordHash string
	Active       bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// NewUser constructs a User aggregate, validates invariants.
func NewUser(tenantID, email, name string, role Role) (*User, error) {
	if email == "" {
		return nil, errors.New("user: email is required")
	}
	if name == "" {
		return nil, errors.New("user: name is required")
	}

	now := time.Now().UTC()
	return &User{
		ID:        uuid.NewString(),
		TenantID:  tenantID,
		Email:     email,
		Name:      name,
		Role:      role,
		Active:    true,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// Deactivate marks the user as inactive.
func (u *User) Deactivate() {
	u.Active = false
	u.UpdatedAt = time.Now().UTC()
}

// UserRepository defines persistence operations for the User aggregate.
type UserRepository interface {
	Save(u *User) error
	FindByID(id string) (*User, error)
	FindByEmail(tenantID, email string) (*User, error)
	List(tenantID string, limit int, offset int) ([]*User, error)
	Update(u *User) error
}

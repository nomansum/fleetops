package commands

import (
	"fmt"

	"github.com/fleetops/iam-service/internal/domain"
	"golang.org/x/crypto/bcrypt"
)

// RegisterInput is the command payload for tenant + admin user creation.
type RegisterInput struct {
	TenantName    string
	BillingEmail  string
	AdminName     string
	AdminEmail    string
	AdminPassword string
}

// RegisterOutput is returned on success.
type RegisterOutput struct {
	Tenant *domain.Tenant
	Admin  *domain.User
}

// RegisterHandler handles the Register command.
type RegisterHandler struct {
	tenants domain.TenantRepository
	users   domain.UserRepository
}

func NewRegisterHandler(t domain.TenantRepository, u domain.UserRepository) *RegisterHandler {
	return &RegisterHandler{tenants: t, users: u}
}

func (h *RegisterHandler) Handle(in RegisterInput) (*RegisterOutput, error) {
	tenant, err := domain.NewTenant(in.TenantName, in.BillingEmail)
	if err != nil {
		return nil, err
	}

	admin, err := domain.NewUser(tenant.ID, in.AdminEmail, in.AdminName, domain.RoleAdmin)
	if err != nil {
		return nil, err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(in.AdminPassword), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("register: hash password: %w", err)
	}
	admin.PasswordHash = string(hash)

	if err := h.tenants.Save(tenant); err != nil {
		return nil, fmt.Errorf("register: save tenant: %w", err)
	}
	if err := h.users.Save(admin); err != nil {
		return nil, fmt.Errorf("register: save admin: %w", err)
	}

	return &RegisterOutput{Tenant: tenant, Admin: admin}, nil
}

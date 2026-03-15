package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/fleetops/iam-service/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// UserRepo implements domain.UserRepository using PostgreSQL.
type UserRepo struct{ db *pgxpool.Pool }

func NewUserRepo(db *pgxpool.Pool) *UserRepo { return &UserRepo{db: db} }

func (r *UserRepo) Save(u *domain.User) error {
	_, err := r.db.Exec(context.Background(), `
		INSERT INTO users (id, tenant_id, email, name, role, password_hash, active, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		u.ID, u.TenantID, u.Email, u.Name, string(u.Role), u.PasswordHash, u.Active, u.CreatedAt, u.UpdatedAt,
	)
	return wrapErr("save user", err)
}

func (r *UserRepo) FindByID(id string) (*domain.User, error) {
	row := r.db.QueryRow(context.Background(),
		`SELECT id,tenant_id,email,name,role,password_hash,active,created_at,updated_at FROM users WHERE id=$1`, id)
	return scanUser(row)
}

func (r *UserRepo) FindByEmail(tenantID, email string) (*domain.User, error) {
	row := r.db.QueryRow(context.Background(),
		`SELECT id,tenant_id,email,name,role,password_hash,active,created_at,updated_at FROM users WHERE tenant_id=$1 AND email=$2`,
		tenantID, email)
	return scanUser(row)
}

func (r *UserRepo) List(tenantID string, limit, offset int) ([]*domain.User, error) {
	rows, err := r.db.Query(context.Background(),
		`SELECT id,tenant_id,email,name,role,password_hash,active,created_at,updated_at FROM users WHERE tenant_id=$1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
		tenantID, limit, offset)
	if err != nil {
		return nil, wrapErr("list users", err)
	}
	defer rows.Close()

	var users []*domain.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (r *UserRepo) Update(u *domain.User) error {
	u.UpdatedAt = time.Now().UTC()
	_, err := r.db.Exec(context.Background(),
		`UPDATE users SET name=$1,role=$2,password_hash=$3,active=$4,updated_at=$5 WHERE id=$6`,
		u.Name, string(u.Role), u.PasswordHash, u.Active, u.UpdatedAt, u.ID)
	return wrapErr("update user", err)
}

// TenantRepo implements domain.TenantRepository using PostgreSQL.
type TenantRepo struct{ db *pgxpool.Pool }

func NewTenantRepo(db *pgxpool.Pool) *TenantRepo { return &TenantRepo{db: db} }

func (r *TenantRepo) Save(t *domain.Tenant) error {
	_, err := r.db.Exec(context.Background(),
		`INSERT INTO tenants (id,name,billing_email,plan,active,created_at) VALUES ($1,$2,$3,$4,$5,$6)`,
		t.ID, t.Name, t.BillingEmail, string(t.Plan), t.Active, t.CreatedAt)
	return wrapErr("save tenant", err)
}

func (r *TenantRepo) FindByID(id string) (*domain.Tenant, error) {
	row := r.db.QueryRow(context.Background(),
		`SELECT id,name,billing_email,plan,active,created_at FROM tenants WHERE id=$1`, id)
	var t domain.Tenant
	var plan string
	err := row.Scan(&t.ID, &t.Name, &t.BillingEmail, &plan, &t.Active, &t.CreatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("tenant %s not found", id)
		}
		return nil, wrapErr("find tenant", err)
	}
	t.Plan = domain.Plan(plan)
	return &t, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

type scanner interface {
	Scan(dest ...any) error
}

func scanUser(s scanner) (*domain.User, error) {
	var u domain.User
	var role string
	err := s.Scan(&u.ID, &u.TenantID, &u.Email, &u.Name, &role,
		&u.PasswordHash, &u.Active, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("user not found")
		}
		return nil, wrapErr("scan user", err)
	}
	u.Role = domain.Role(role)
	return &u, nil
}

func wrapErr(op string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("postgres %s: %w", op, err)
}

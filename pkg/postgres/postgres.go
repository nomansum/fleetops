package postgres

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Config holds connection parameters.
type Config struct {
	DSN          string
	MaxConns     int32
	MinConns     int32
	MaxIdleTime  time.Duration
	MaxLifetime  time.Duration
}

// ConfigFromEnv reads DATABASE_URL from env. Other values use safe defaults.
func ConfigFromEnv() Config {
	return Config{
		DSN:         os.Getenv("DATABASE_URL"),
		MaxConns:    20,
		MinConns:    2,
		MaxIdleTime: 5 * time.Minute,
		MaxLifetime: 30 * time.Minute,
	}
}

// New creates a pgx connection pool.
func New(ctx context.Context, cfg Config) (*pgxpool.Pool, error) {
	if cfg.DSN == "" {
		return nil, fmt.Errorf("postgres: DATABASE_URL is required")
	}

	pcfg, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("postgres: parse config: %w", err)
	}

	pcfg.MaxConns = cfg.MaxConns
	pcfg.MinConns = cfg.MinConns
	pcfg.MaxConnIdleTime = cfg.MaxIdleTime
	pcfg.MaxConnLifetime = cfg.MaxLifetime

	pool, err := pgxpool.NewWithConfig(ctx, pcfg)
	if err != nil {
		return nil, fmt.Errorf("postgres: connect: %w", err)
	}

	// Verify connectivity.
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: ping: %w", err)
	}

	return pool, nil
}

package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/fleetops/document-worker/internal/jobs"
	"github.com/jackc/pgx/v5/pgxpool"
	natspkg "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog"
)

func main() {
	log := zerolog.New(os.Stdout).With().Timestamp().Str("service", "document-worker").Logger()

	// ── Postgres (reads from driver-service DB) ───────────────────────────────
	pool, err := pgxpool.New(context.Background(), os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal().Err(err).Msg("connect postgres")
	}
	defer pool.Close()

	// ── NATS JetStream ────────────────────────────────────────────────────────
	nc, err := natspkg.Connect(envOr("NATS_URL", natspkg.DefaultURL))
	if err != nil {
		log.Fatal().Err(err).Msg("connect nats")
	}
	defer nc.Drain()

	js, err := jetstream.New(nc)
	if err != nil {
		log.Fatal().Err(err).Msg("jetstream init")
	}

	// Ensure the stream exists (idempotent).
	_, _ = js.CreateOrUpdateStream(context.Background(), jetstream.StreamConfig{
		Name:     "FLEET_EVENTS",
		Subjects: []string{"fleet.>"},
	})

	// ── Jobs ──────────────────────────────────────────────────────────────────
	expiryScanner := jobs.NewExpiryScanner(pool, js, log)
	assignmentReaper := jobs.NewAssignmentReaper(pool, js, log)

	// ── Scheduler ─────────────────────────────────────────────────────────────
	c := cron.New(cron.WithLogger(cron.VerbosePrintfLogger(&cronLogger{log})))

	// Document Expiry Scanner — daily at 02:00 UTC
	_, err = c.AddJob("0 2 * * *", cron.NewChain(
		cron.SkipIfStillRunning(cron.VerbosePrintfLogger(&cronLogger{log})),
	).Then(expiryScanner))
	if err != nil {
		log.Fatal().Err(err).Msg("add expiry scanner job")
	}

	// Assignment Reaper — every 5 minutes
	_, err = c.AddJob("*/5 * * * *", cron.NewChain(
		cron.SkipIfStillRunning(cron.VerbosePrintfLogger(&cronLogger{log})),
	).Then(assignmentReaper))
	if err != nil {
		log.Fatal().Err(err).Msg("add assignment reaper job")
	}

	c.Start()
	log.Info().Msg("document-worker started")

	// Run once immediately on startup for visibility in dev
	go expiryScanner.Run()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info().Msg("shutting down")
	<-c.Stop().Done()
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// cronLogger adapts zerolog for robfig/cron.
type cronLogger struct{ log zerolog.Logger }

func (l *cronLogger) Printf(format string, args ...any) {
	l.log.Debug().Msgf(format, args...)
}

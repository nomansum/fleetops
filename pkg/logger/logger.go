package logger

import (
	"os"
	"time"

	"github.com/rs/zerolog"
)

// New returns a zerolog.Logger configured with service name and level from env.
// Every log line includes: service, trace_id (set via context), tenant_id when available.
func New(service string) zerolog.Logger {
	level, err := zerolog.ParseLevel(os.Getenv("LOG_LEVEL"))
	if err != nil || level == zerolog.NoLevel {
		level = zerolog.InfoLevel
	}

	zerolog.TimeFieldFormat = time.RFC3339

	return zerolog.New(os.Stdout).
		Level(level).
		With().
		Timestamp().
		Str("service", service).
		Logger()
}

// WithTraceID adds trace_id and span_id to a sub-logger derived from the given logger.
func WithTraceID(log zerolog.Logger, traceID, spanID string) zerolog.Logger {
	return log.With().
		Str("trace_id", traceID).
		Str("span_id", spanID).
		Logger()
}

// WithTenant adds tenant_id to a sub-logger.
func WithTenant(log zerolog.Logger, tenantID string) zerolog.Logger {
	return log.With().Str("tenant_id", tenantID).Logger()
}

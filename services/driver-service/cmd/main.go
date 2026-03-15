package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	driverv1 "github.com/fleetops/gen/driver/v1"
	consulpkg "github.com/fleetops/pkg/consul"
	drivergrpc "github.com/fleetops/driver-service/internal/interfaces/grpc"
	natsinfra "github.com/fleetops/driver-service/internal/infrastructure/nats"
	pginfra "github.com/fleetops/driver-service/internal/infrastructure/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
	natspkg "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

func main() {
	log := zerolog.New(os.Stdout).With().Timestamp().Str("service", "driver-service").Logger()

	// ── Postgres ──────────────────────────────────────────────────────────────
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

	// Ensure the FLEET_EVENTS stream exists (idempotent).
	_, _ = js.CreateOrUpdateStream(context.Background(), jetstream.StreamConfig{
		Name:     "FLEET_EVENTS",
		Subjects: []string{"fleet.>"},
	})

	// ── Wire ──────────────────────────────────────────────────────────────────
	repo := pginfra.NewDriverRepo(pool)
	publisher := natsinfra.NewEventPublisher(js)

	// ── gRPC server ───────────────────────────────────────────────────────────
	grpcServer := grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
	)

	driverv1.RegisterDriverServiceServer(grpcServer, drivergrpc.NewServer(repo, publisher))

	healthSvc := health.NewServer()
	healthSvc.SetServingStatus("driver-service", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(grpcServer, healthSvc)
	reflection.Register(grpcServer)

	// ── Listen ────────────────────────────────────────────────────────────────
	port := envOr("GRPC_PORT", "9002")
	lis, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%s", port))
	if err != nil {
		log.Fatal().Err(err).Msg("listen")
	}

	log.Info().Str("port", port).Msg("driver-service started")

	go func() {
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatal().Err(err).Msg("serve")
		}
	}()

	// ── Consul registration ───────────────────────────────────────────────────
	portInt, _ := strconv.Atoi(port)
	deregister, err := consulpkg.Register("driver-service", "driver-service", envOr("SERVICE_HOST", "localhost"), portInt)
	if err != nil {
		log.Warn().Err(err).Msg("consul registration failed")
	} else {
		defer deregister()
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info().Msg("shutting down")
	grpcServer.GracefulStop()
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

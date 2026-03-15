package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	dispatchv1 "github.com/fleetops/gen/dispatch/v1"
	dispatchgrpc "github.com/fleetops/dispatch-service/internal/interfaces/grpc"
	pginfra "github.com/fleetops/dispatch-service/internal/infrastructure/postgres"
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
	log := zerolog.New(os.Stdout).With().Timestamp().Str("service", "dispatch-service").Logger()

	pool, err := pgxpool.New(context.Background(), os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal().Err(err).Msg("connect postgres")
	}
	defer pool.Close()

	nc, err := natspkg.Connect(envOr("NATS_URL", natspkg.DefaultURL))
	if err != nil {
		log.Fatal().Err(err).Msg("connect nats")
	}
	defer nc.Drain()

	js, err := jetstream.New(nc)
	if err != nil {
		log.Fatal().Err(err).Msg("jetstream init")
	}

	_, _ = js.CreateOrUpdateStream(context.Background(), jetstream.StreamConfig{
		Name:     "FLEET_EVENTS",
		Subjects: []string{"fleet.>"},
	})

	repo := pginfra.NewOrderRepo(pool)
	grpcServer := grpc.NewServer(grpc.StatsHandler(otelgrpc.NewServerHandler()))

	dispatchv1.RegisterDispatchServiceServer(grpcServer, dispatchgrpc.NewServer(repo, js))

	healthSvc := health.NewServer()
	healthSvc.SetServingStatus("dispatch-service", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(grpcServer, healthSvc)
	reflection.Register(grpcServer)

	port := envOr("GRPC_PORT", "9003")
	lis, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%s", port))
	if err != nil {
		log.Fatal().Err(err).Msg("listen")
	}

	log.Info().Str("port", port).Msg("dispatch-service started")

	go func() {
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatal().Err(err).Msg("serve")
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	grpcServer.GracefulStop()
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

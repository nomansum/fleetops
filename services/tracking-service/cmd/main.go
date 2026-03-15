package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	trackingv1 "github.com/fleetops/gen/tracking/v1"
	trackinggrpc "github.com/fleetops/tracking-service/internal/interfaces/grpc"
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
	log := zerolog.New(os.Stdout).With().Timestamp().Str("service", "tracking-service").Logger()

	pool, err := pgxpool.New(context.Background(), os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal().Err(err).Msg("connect timescaledb")
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

	grpcServer := grpc.NewServer(grpc.StatsHandler(otelgrpc.NewServerHandler()))
	trackingv1.RegisterTrackingServiceServer(grpcServer, trackinggrpc.NewServer(pool, js))

	healthSvc := health.NewServer()
	healthSvc.SetServingStatus("tracking-service", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(grpcServer, healthSvc)
	reflection.Register(grpcServer)

	port := envOr("GRPC_PORT", "9004")
	lis, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%s", port))
	if err != nil {
		log.Fatal().Err(err).Msg("listen")
	}

	log.Info().Str("port", port).Msg("tracking-service started")

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

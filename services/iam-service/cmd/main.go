package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	iamv1 "github.com/fleetops/gen/iam/v1"
	"github.com/fleetops/iam-service/internal/application/commands"
	iamgrpc "github.com/fleetops/iam-service/internal/interfaces/grpc"
	iamjwt "github.com/fleetops/iam-service/internal/infrastructure/jwt"
	iampostgres "github.com/fleetops/iam-service/internal/infrastructure/postgres"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	log := zerolog.New(os.Stdout).With().Timestamp().Str("service", "iam-service").Logger()

	// ── Database ──────────────────────────────────────────────────────────────
	pool, err := pgxpool.New(context.Background(), os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal().Err(err).Msg("connect to postgres")
	}
	defer pool.Close()

	// ── JWT ───────────────────────────────────────────────────────────────────
	tokenSvc, err := iamjwt.NewTokenService()
	if err != nil {
		log.Fatal().Err(err).Msg("init jwt token service")
	}

	// ── Repositories ──────────────────────────────────────────────────────────
	userRepo := iampostgres.NewUserRepo(pool)
	tenantRepo := iampostgres.NewTenantRepo(pool)

	// ── Command handlers ──────────────────────────────────────────────────────
	registerHandler := commands.NewRegisterHandler(tenantRepo, userRepo)
	loginHandler := commands.NewLoginHandler(userRepo, tokenSvc)

	// ── gRPC server ───────────────────────────────────────────────────────────
	grpcServer := grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
	)

	iamv1.RegisterIAMServiceServer(grpcServer, iamgrpc.NewServer(
		registerHandler,
		loginHandler,
		userRepo,
		tokenSvc,
	))

	// Register gRPC health + reflection (Consul health check + grpcurl dev tool)
	healthSvc := health.NewServer()
	healthSvc.SetServingStatus("iam-service", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(grpcServer, healthSvc)
	reflection.Register(grpcServer)

	// ── Listen ────────────────────────────────────────────────────────────────
	port := envOr("GRPC_PORT", "9001")
	lis, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%s", port))
	if err != nil {
		log.Fatal().Err(err).Msg("listen")
	}

	log.Info().Str("port", port).Msg("iam-service started")

	go func() {
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatal().Err(err).Msg("serve")
		}
	}()

	// ── Graceful shutdown ─────────────────────────────────────────────────────
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

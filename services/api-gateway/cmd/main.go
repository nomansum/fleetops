package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	billingv1 "github.com/fleetops/gen/billing/v1"
	dispatchv1 "github.com/fleetops/gen/dispatch/v1"
	driverv1 "github.com/fleetops/gen/driver/v1"
	iamv1 "github.com/fleetops/gen/iam/v1"
	trackingv1 "github.com/fleetops/gen/tracking/v1"
	"github.com/fleetops/api-gateway/internal/handler"
	"github.com/fleetops/api-gateway/internal/router"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc/keepalive"
)

func main() {
	log := zerolog.New(os.Stdout).With().Timestamp().Str("service", "api-gateway").Logger()

	// ── gRPC clients (one connection per downstream service) ──────────────────
	// Addresses from env — in mesh mode these point to Consul DNS names
	iamConn := mustDial(log, envOr("IAM_SERVICE_ADDR", "localhost:9001"))
	driverConn := mustDial(log, envOr("DRIVER_SERVICE_ADDR", "localhost:9002"))
	dispatchConn := mustDial(log, envOr("DISPATCH_SERVICE_ADDR", "localhost:9003"))
	trackingConn := mustDial(log, envOr("TRACKING_SERVICE_ADDR", "localhost:9004"))
	billingConn := mustDial(log, envOr("BILLING_SERVICE_ADDR", "localhost:9005"))

	defer iamConn.Close()
	defer driverConn.Close()
	defer dispatchConn.Close()
	defer trackingConn.Close()
	defer billingConn.Close()

	clients := handler.Clients{
		IAM:      iamv1.NewIAMServiceClient(iamConn),
		Driver:   driverv1.NewDriverServiceClient(driverConn),
		Dispatch: dispatchv1.NewDispatchServiceClient(dispatchConn),
		Tracking: trackingv1.NewTrackingServiceClient(trackingConn),
		Billing:  billingv1.NewBillingServiceClient(billingConn),
	}

	// ── Redis (JWT cache + rate limiting) ─────────────────────────────────────
	rdb := redis.NewClient(&redis.Options{
		Addr: envOr("REDIS_ADDR", "localhost:6379"),
	})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		log.Warn().Err(err).Msg("redis ping failed — cache disabled")
	}

	// ── HTTP server ───────────────────────────────────────────────────────────
	r := router.New(clients, clients.IAM, rdb)
	port := envOr("HTTP_PORT", "8080")

	srv := &http.Server{
		Addr:         "0.0.0.0:" + port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	log.Info().Str("port", port).Msg("api-gateway started")

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("listen")
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info().Msg("shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

func mustDial(log zerolog.Logger, addr string) *grpc.ClientConn {
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                30 * time.Second,
			Timeout:             10 * time.Second,
			PermitWithoutStream: true,
		}),
	)
	if err != nil {
		log.Fatal().Str("addr", addr).Err(err).Msg("dial grpc")
	}
	return conn
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

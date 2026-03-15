package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	billingv1 "github.com/fleetops/gen/billing/v1"
	"github.com/fleetops/billing-service/internal/domain"
	natsinfra "github.com/fleetops/billing-service/internal/infrastructure/nats"
	"github.com/jackc/pgx/v5/pgxpool"
	natspkg "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
)

func main() {
	log := zerolog.New(os.Stdout).With().Timestamp().Str("service", "billing-service").Logger()

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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// ── NATS consumer: fleet.order.delivered → auto-create invoice ────────────
	if err := natsinfra.StartOrderDeliveredConsumer(ctx, js, log, 5, func(ctx context.Context, event natsinfra.OrderDeliveredEvent) error {
		inv := domain.NewInvoice(event.TenantID, event.OrderID)
		// Base charge: $5.00 + $0.50/km
		baseCents := int64(500)
		kmCents := int64(event.DistanceKM * 50)
		inv.AddLineItem("Base delivery charge", 1, domain.Money{AmountCents: baseCents, CurrencyCode: "USD"})
		inv.AddLineItem(fmt.Sprintf("Distance charge (%.1f km)", event.DistanceKM), 1, domain.Money{AmountCents: kmCents, CurrencyCode: "USD"})

		due := time.Now().UTC().Add(30 * 24 * time.Hour)
		if err := inv.Issue(due); err != nil {
			return err
		}

		_, err := pool.Exec(ctx, `
			INSERT INTO invoices (id,tenant_id,order_id,reference,status,subtotal_cents,tax_cents,total_cents,currency,issued_at,due_date,created_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
			ON CONFLICT (order_id) DO NOTHING`,
			inv.ID, inv.TenantID, inv.OrderID, inv.Reference, string(inv.Status),
			inv.Subtotal.AmountCents, inv.Tax.AmountCents, inv.Total.AmountCents, inv.Total.CurrencyCode,
			inv.IssuedAt, inv.DueDate, inv.CreatedAt)
		return err
	}); err != nil {
		log.Warn().Err(err).Msg("start order.delivered consumer (FLEET_EVENTS stream may not exist yet)")
	}

	// ── Minimal gRPC server (full implementation follows same pattern as other services) ──
	grpcServer := grpc.NewServer(grpc.StatsHandler(otelgrpc.NewServerHandler()))
	billingv1.RegisterBillingServiceServer(grpcServer, &billingServer{db: pool, js: js})

	healthSvc := health.NewServer()
	healthSvc.SetServingStatus("billing-service", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(grpcServer, healthSvc)
	reflection.Register(grpcServer)

	port := envOr("GRPC_PORT", "9005")
	lis, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%s", port))
	if err != nil {
		log.Fatal().Err(err).Msg("listen")
	}

	log.Info().Str("port", port).Msg("billing-service started")

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

// billingServer is a minimal stub implementation — extend per service pattern.
type billingServer struct {
	billingv1.UnimplementedBillingServiceServer
	db *pgxpool.Pool
	js jetstream.JetStream
}

func (s *billingServer) GetInvoice(ctx context.Context, req *billingv1.GetInvoiceRequest) (*billingv1.Invoice, error) {
	row := s.db.QueryRow(ctx, `SELECT id,tenant_id,order_id,reference,status,total_cents,currency,created_at FROM invoices WHERE id=$1`, req.InvoiceId)
	var id, tenantID, orderID, ref, stat, currency, createdAt string
	var totalCents int64
	if err := row.Scan(&id, &tenantID, &orderID, &ref, &stat, &totalCents, &currency, &createdAt); err != nil {
		return nil, status.Error(codes.NotFound, "invoice not found")
	}
	return &billingv1.Invoice{Id: id, TenantId: tenantID, OrderId: orderID, Reference: ref, CreatedAt: createdAt}, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

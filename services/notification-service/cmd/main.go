package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"text/template"
	"time"

	notificationv1 "github.com/fleetops/gen/notification/v1"
	consulpkg "github.com/fleetops/pkg/consul"
	"github.com/google/uuid"
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
	log := zerolog.New(os.Stdout).With().Timestamp().Str("service", "notification-service").Logger()

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

	// ── NATS consumer: subscribe to all fleet.* events and fan-out ────────────
	startEventConsumer(ctx, js, log)

	// ── gRPC server ───────────────────────────────────────────────────────────
	grpcServer := grpc.NewServer(grpc.StatsHandler(otelgrpc.NewServerHandler()))
	notificationv1.RegisterNotificationServiceServer(grpcServer, &notificationServer{js: js, log: log})

	healthSvc := health.NewServer()
	healthSvc.SetServingStatus("notification-service", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(grpcServer, healthSvc)
	reflection.Register(grpcServer)

	port := envOr("GRPC_PORT", "9006")
	lis, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%s", port))
	if err != nil {
		log.Fatal().Err(err).Msg("listen")
	}

	log.Info().Str("port", port).Msg("notification-service started")
	go func() {
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatal().Err(err).Msg("serve")
		}
	}()

	// ── Consul registration ───────────────────────────────────────────────────
	portInt, _ := strconv.Atoi(port)
	deregister, err := consulpkg.Register("notification-service", "notification-service", envOr("SERVICE_HOST", "localhost"), portInt)
	if err != nil {
		log.Warn().Err(err).Msg("consul registration failed")
	} else {
		defer deregister()
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	grpcServer.GracefulStop()
}

// startEventConsumer subscribes to FLEET_EVENTS and maps events to notifications.
func startEventConsumer(ctx context.Context, js jetstream.JetStream, log zerolog.Logger) {
	stream, err := js.Stream(ctx, "FLEET_EVENTS")
	if err != nil {
		log.Warn().Err(err).Msg("FLEET_EVENTS stream not found yet, consumer will retry")
		return
	}

	cons, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:        "notification-fleet-events",
		FilterSubjects: []string{"fleet.>"},
		AckPolicy:      jetstream.AckExplicitPolicy,
		MaxDeliver:     3,
	})
	if err != nil {
		log.Error().Err(err).Msg("create consumer")
		return
	}

	for range 3 {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}

				msgs, err := cons.Fetch(10, jetstream.FetchMaxWait(2*time.Second))
				if err != nil {
					continue
				}

				for msg := range msgs.Messages() {
					handleFleetEvent(ctx, msg.Subject(), msg.Data(), log)
					_ = msg.Ack()
				}
			}
		}()
	}
}

func handleFleetEvent(ctx context.Context, subject string, data []byte, log zerolog.Logger) {
	var payload map[string]any
	_ = json.Unmarshal(data, &payload)

	switch subject {
	case "fleet.order.assigned":
		log.Info().Str("subject", subject).Msg("→ notify driver of new assignment")
		// TODO: look up driver contact, send SMS via Twilio / email via SendGrid
	case "fleet.order.delivered":
		log.Info().Str("subject", subject).Msg("→ notify tenant of delivery")
	case "fleet.document.expiring":
		log.Info().Str("subject", subject).Msg("→ notify driver of document expiry")
	case "fleet.invoice.issued":
		log.Info().Str("subject", subject).Msg("→ send invoice email to tenant")
	}
}

// notificationServer implements the gRPC NotificationService.
type notificationServer struct {
	notificationv1.UnimplementedNotificationServiceServer
	js  jetstream.JetStream
	log zerolog.Logger
}

func (s *notificationServer) SendNotification(ctx context.Context, req *notificationv1.SendNotificationRequest) (*notificationv1.SendNotificationResponse, error) {
	// Render template body with vars
	body := renderTemplate("Hello {{.recipient_id}}", req.Vars)

	s.log.Info().
		Str("tenant_id", req.TenantId).
		Str("recipient_id", req.RecipientId).
		Str("channel", req.Channel.String()).
		Str("template_id", req.TemplateId).
		Str("body_preview", body[:min(len(body), 50)]).
		Msg("send notification")

	return &notificationv1.SendNotificationResponse{
		NotificationId: uuid.NewString(),
		Status:         notificationv1.NotificationStatus_NOTIFICATION_STATUS_SENT,
	}, nil
}

func (s *notificationServer) GetNotificationStatus(ctx context.Context, req *notificationv1.GetNotificationStatusRequest) (*notificationv1.Notification, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}

func (s *notificationServer) ListTemplates(ctx context.Context, req *notificationv1.ListTemplatesRequest) (*notificationv1.ListTemplatesResponse, error) {
	return &notificationv1.ListTemplatesResponse{}, nil
}

func (s *notificationServer) UpsertTemplate(ctx context.Context, req *notificationv1.UpsertTemplateRequest) (*notificationv1.NotificationTemplate, error) {
	return &notificationv1.NotificationTemplate{Name: req.Name, Channel: req.Channel}, nil
}

func renderTemplate(tmpl string, vars map[string]string) string {
	t, err := template.New("").Parse(tmpl)
	if err != nil {
		return tmpl
	}
	data := make(map[string]any, len(vars))
	for k, v := range vars {
		data[k] = v
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return tmpl
	}
	return buf.String()
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

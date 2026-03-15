package router

import (
	"net/http"
	"time"

	iamv1 "github.com/fleetops/gen/iam/v1"
	"github.com/fleetops/api-gateway/internal/handler"
	"github.com/fleetops/api-gateway/internal/middleware"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

// New builds and returns the Gin engine with all routes wired.
func New(clients handler.Clients, iamClient iamv1.IAMServiceClient, rdb *redis.Client) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()

	// ── Global middleware ─────────────────────────────────────────────────────
	r.Use(gin.Recovery())
	r.Use(otelgin.Middleware("api-gateway")) // OTel trace per request
	r.Use(requestLogger())

	// ── Health / readiness ────────────────────────────────────────────────────
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	v1 := r.Group("/v1")

	// ── Public auth routes (no JWT required) ──────────────────────────────────
	auth := v1.Group("/auth")
	{
		auth.POST("/register", handler.Register(clients))
		auth.POST("/login", handler.Login(clients))
	}

	// ── Authenticated routes ──────────────────────────────────────────────────
	// Auth middleware: validates JWT via iam-service (cached in Redis)
	// Rate limit: 300 requests per minute per user
	protected := v1.Group("/")
	protected.Use(
		middleware.AuthMiddleware(iamClient, rdb),
		middleware.RateLimitMiddleware(rdb, 300, time.Minute),
	)

	// Drivers
	drivers := protected.Group("drivers")
	{
		drivers.POST("", handler.CreateDriver(clients))
		drivers.GET("", handler.ListAvailableDrivers(clients))
		drivers.GET(":id", handler.GetDriver(clients))
		drivers.GET("nearby", handler.FindNearbyDrivers(clients))
	}

	// Orders
	orders := protected.Group("orders")
	{
		orders.POST("", handler.CreateOrder(clients))
		orders.GET("", handler.ListOrders(clients))
		orders.GET(":id", handler.GetOrder(clients))
		orders.POST(":id/assign", handler.AssignDriver(clients))
	}

	// Tracking (live location)
	tracking := protected.Group("tracking")
	{
		tracking.GET("drivers/:id/location", handler.GetLastKnownLocation(clients))
	}

	// Billing
	billing := protected.Group("billing")
	{
		billing.GET("invoices/:id", handler.GetInvoice(clients))
	}

	return r
}

func requestLogger() gin.HandlerFunc {
	return gin.LoggerWithFormatter(func(p gin.LogFormatterParams) string {
		return ""  // zerolog handles logging via OTel
	})
}

// Package handler contains Gin HTTP handlers that delegate to gRPC services.
package handler

import (
	"fmt"
	"net/http"

	billingv1 "github.com/fleetops/gen/billing/v1"
	dispatchv1 "github.com/fleetops/gen/dispatch/v1"
	driverv1 "github.com/fleetops/gen/driver/v1"
	iamv1 "github.com/fleetops/gen/iam/v1"
	trackingv1 "github.com/fleetops/gen/tracking/v1"
	"github.com/gin-gonic/gin"
)

// Clients bundles all downstream gRPC clients.
type Clients struct {
	IAM      iamv1.IAMServiceClient
	Driver   driverv1.DriverServiceClient
	Dispatch dispatchv1.DispatchServiceClient
	Tracking trackingv1.TrackingServiceClient
	Billing  billingv1.BillingServiceClient
}

// ── Auth ──────────────────────────────────────────────────────────────────────

func Register(clients Clients) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req iamv1.RegisterRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		resp, err := clients.IAM.Register(c.Request.Context(), &req)
		if err != nil {
			c.JSON(grpcToHTTP(err), gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusCreated, resp)
	}
}

func Login(clients Clients) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req iamv1.LoginRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		resp, err := clients.IAM.Login(c.Request.Context(), &req)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
			return
		}
		c.JSON(http.StatusOK, resp)
	}
}

// ── Drivers ───────────────────────────────────────────────────────────────────

func CreateDriver(clients Clients) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req driverv1.CreateDriverRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if tid, ok := c.Get("tenant_id"); ok {
			req.TenantId = fmt.Sprint(tid)
		}
		resp, err := clients.Driver.CreateDriver(c.Request.Context(), &req)
		if err != nil {
			c.JSON(grpcToHTTP(err), gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusCreated, resp)
	}
}

func GetDriver(clients Clients) gin.HandlerFunc {
	return func(c *gin.Context) {
		resp, err := clients.Driver.GetDriver(c.Request.Context(), &driverv1.GetDriverRequest{
			DriverId: c.Param("id"),
		})
		if err != nil {
			c.JSON(grpcToHTTP(err), gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, resp)
	}
}

func ListAvailableDrivers(clients Clients) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID, _ := c.Get("tenant_id")
		resp, err := clients.Driver.ListAvailableDrivers(c.Request.Context(), &driverv1.ListAvailableDriversRequest{
			TenantId: tenantID.(string),
			PageSize: 20,
		})
		if err != nil {
			c.JSON(grpcToHTTP(err), gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, resp)
	}
}

// ── Orders ────────────────────────────────────────────────────────────────────

func CreateOrder(clients Clients) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req dispatchv1.CreateOrderRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		tenantID, _ := c.Get("tenant_id")
		req.TenantId = tenantID.(string)
		resp, err := clients.Dispatch.CreateOrder(c.Request.Context(), &req)
		if err != nil {
			c.JSON(grpcToHTTP(err), gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusCreated, resp)
	}
}

func GetOrder(clients Clients) gin.HandlerFunc {
	return func(c *gin.Context) {
		resp, err := clients.Dispatch.GetOrder(c.Request.Context(), &dispatchv1.GetOrderRequest{
			OrderId: c.Param("id"),
		})
		if err != nil {
			c.JSON(grpcToHTTP(err), gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, resp)
	}
}

func AssignDriver(clients Clients) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req dispatchv1.AssignDriverRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		req.OrderId = c.Param("id")
		resp, err := clients.Dispatch.AssignDriver(c.Request.Context(), &req)
		if err != nil {
			c.JSON(grpcToHTTP(err), gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, resp)
	}
}

func ListOrders(clients Clients) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID, _ := c.Get("tenant_id")
		resp, err := clients.Dispatch.ListOrders(c.Request.Context(), &dispatchv1.ListOrdersRequest{
			TenantId: tenantID.(string),
			PageSize: 20,
		})
		if err != nil {
			c.JSON(grpcToHTTP(err), gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, resp)
	}
}

func FindNearbyDrivers(clients Clients) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID, _ := c.Get("tenant_id")
		resp, err := clients.Dispatch.FindNearbyDrivers(c.Request.Context(), &dispatchv1.FindNearbyDriversRequest{
			TenantId: tenantID.(string),
			Lat:      parseFloat(c.Query("lat")),
			Lon:      parseFloat(c.Query("lon")),
			RadiusKm: parseFloatDefault(c.Query("radius_km"), 5.0),
			Limit:    10,
		})
		if err != nil {
			c.JSON(grpcToHTTP(err), gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, resp)
	}
}

// ── Tracking ──────────────────────────────────────────────────────────────────

func GetLastKnownLocation(clients Clients) gin.HandlerFunc {
	return func(c *gin.Context) {
		resp, err := clients.Tracking.GetLastKnownLocation(c.Request.Context(), &trackingv1.GetLastKnownLocationRequest{
			DriverId: c.Param("id"),
		})
		if err != nil {
			c.JSON(grpcToHTTP(err), gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, resp)
	}
}

// ── Billing ───────────────────────────────────────────────────────────────────

func GetInvoice(clients Clients) gin.HandlerFunc {
	return func(c *gin.Context) {
		resp, err := clients.Billing.GetInvoice(c.Request.Context(), &billingv1.GetInvoiceRequest{
			InvoiceId: c.Param("id"),
		})
		if err != nil {
			c.JSON(grpcToHTTP(err), gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, resp)
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func grpcToHTTP(err error) int {
	msg := err.Error()
	switch {
	case contains(msg, "NotFound"):
		return http.StatusNotFound
	case contains(msg, "AlreadyExists"):
		return http.StatusConflict
	case contains(msg, "InvalidArgument"):
		return http.StatusBadRequest
	case contains(msg, "Unauthenticated"):
		return http.StatusUnauthorized
	case contains(msg, "PermissionDenied"):
		return http.StatusForbidden
	case contains(msg, "FailedPrecondition"):
		return http.StatusUnprocessableEntity
	default:
		return http.StatusInternalServerError
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func parseFloat(s string) float64 {
	var f float64
	_, _ = fmt.Sscanf(s, "%f", &f)
	return f
}

func parseFloatDefault(s string, def float64) float64 {
	if s == "" {
		return def
	}
	return parseFloat(s)
}


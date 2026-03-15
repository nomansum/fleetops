package domain

import "time"

const (
	SubjectOrderAssigned  = "fleet.order.assigned"
	SubjectOrderDelivered = "fleet.order.delivered"
	SubjectOrderFailed    = "fleet.order.failed"
)

type OrderAssignedEvent struct {
	OrderID    string    `json:"order_id"`
	TenantID   string    `json:"tenant_id"`
	DriverID   string    `json:"driver_id"`
	AssignedAt time.Time `json:"assigned_at"`
}

type OrderDeliveredEvent struct {
	OrderID     string    `json:"order_id"`
	TenantID    string    `json:"tenant_id"`
	DriverID    string    `json:"driver_id"`
	DistanceKM  float64   `json:"distance_km"`
	DeliveredAt time.Time `json:"delivered_at"`
}

type OrderFailedEvent struct {
	OrderID    string      `json:"order_id"`
	TenantID   string      `json:"tenant_id"`
	Reason     OrderStatus `json:"reason"`
	OccurredAt time.Time   `json:"occurred_at"`
}

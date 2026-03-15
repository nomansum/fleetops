package domain

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ── Value Objects ─────────────────────────────────────────────────────────────

type OrderStatus string

const (
	StatusPending    OrderStatus = "PENDING"
	StatusAssigned   OrderStatus = "ASSIGNED"
	StatusPickedUp   OrderStatus = "PICKED_UP"
	StatusInTransit  OrderStatus = "IN_TRANSIT"
	StatusDelivered  OrderStatus = "DELIVERED"
	StatusFailed     OrderStatus = "FAILED"
	StatusTimedOut   OrderStatus = "TIMED_OUT"
	StatusCancelled  OrderStatus = "CANCELLED"
)

type OrderPriority string

const (
	PriorityStandard OrderPriority = "STANDARD"
	PriorityExpress  OrderPriority = "EXPRESS"
	PrioritySameDay  OrderPriority = "SAME_DAY"
)

// Address is a value object with geospatial coordinates.
type Address struct {
	Line1    string
	Line2    string
	City     string
	Postcode string
	Country  string
	Lat      float64
	Lon      float64
}

type TimeWindow struct {
	Earliest time.Time
	Latest   time.Time
}

// ── Entities ──────────────────────────────────────────────────────────────────

// Waypoint is an entity within the Order aggregate.
type Waypoint struct {
	ID           string
	Sequence     int
	Address      Address
	ContactName  string
	ContactPhone string
	Window       TimeWindow
	ArrivedAt    *time.Time
	CompletedAt  *time.Time
}

// DispatchAssignment is an entity representing the driver assignment.
type DispatchAssignment struct {
	ID         string
	OrderID    string
	DriverID   string
	AssignedAt time.Time
	ETA        *time.Time
	Active     bool
}

// ── Aggregate Root ────────────────────────────────────────────────────────────

// Order is the central aggregate of the Dispatch bounded context.
// Invariants:
//   - Only PENDING orders can be assigned
//   - Only one active assignment at a time
//   - Status transitions must follow the allowed FSM
type Order struct {
	ID          string
	TenantID    string
	Reference   string
	Status      OrderStatus
	Priority    OrderPriority
	Waypoints   []*Waypoint
	Assignment  *DispatchAssignment
	DistanceKM  float64
	Notes       string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// allowed transitions: from → set of allowed next statuses
var transitions = map[OrderStatus][]OrderStatus{
	StatusPending:   {StatusAssigned, StatusCancelled},
	StatusAssigned:  {StatusPickedUp, StatusFailed, StatusTimedOut, StatusCancelled},
	StatusPickedUp:  {StatusInTransit, StatusFailed},
	StatusInTransit: {StatusDelivered, StatusFailed},
}

func NewOrder(tenantID string, priority OrderPriority, waypoints []*Waypoint, notes string) (*Order, error) {
	if tenantID == "" {
		return nil, errors.New("order: tenant_id is required")
	}
	if len(waypoints) < 2 {
		return nil, errors.New("order: at least 2 waypoints (pickup + dropoff) are required")
	}

	now := time.Now().UTC()
	return &Order{
		ID:        uuid.NewString(),
		TenantID:  tenantID,
		Reference: generateRef(),
		Status:    StatusPending,
		Priority:  priority,
		Waypoints: waypoints,
		Notes:     notes,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// Assign sets a driver on a PENDING order.
func (o *Order) Assign(driverID string, eta *time.Time) (*DispatchAssignment, error) {
	if o.Status != StatusPending {
		return nil, fmt.Errorf("order: cannot assign driver to order in status %s", o.Status)
	}

	assignment := &DispatchAssignment{
		ID:         uuid.NewString(),
		OrderID:    o.ID,
		DriverID:   driverID,
		AssignedAt: time.Now().UTC(),
		ETA:        eta,
		Active:     true,
	}

	o.Assignment = assignment
	o.Status = StatusAssigned
	o.UpdatedAt = time.Now().UTC()

	return assignment, nil
}

// Unassign removes the current driver assignment, reverting to PENDING.
func (o *Order) Unassign() error {
	if o.Status != StatusAssigned {
		return fmt.Errorf("order: cannot unassign from status %s", o.Status)
	}
	if o.Assignment != nil {
		o.Assignment.Active = false
	}
	o.Assignment = nil
	o.Status = StatusPending
	o.UpdatedAt = time.Now().UTC()
	return nil
}

// Transition moves the order through its status FSM.
func (o *Order) Transition(next OrderStatus) error {
	allowed, ok := transitions[o.Status]
	if !ok {
		return fmt.Errorf("order: no transitions defined from %s", o.Status)
	}

	for _, a := range allowed {
		if a == next {
			o.Status = next
			o.UpdatedAt = time.Now().UTC()
			return nil
		}
	}

	return fmt.Errorf("order: transition %s→%s is not allowed", o.Status, next)
}

// ── Repository Interface ──────────────────────────────────────────────────────

type OrderRepository interface {
	Save(o *Order) error
	FindByID(id string) (*Order, error)
	List(tenantID string, statusFilter OrderStatus, limit, offset int) ([]*Order, error)
	Update(o *Order) error
	FindStaleAssignments(olderThan time.Duration) ([]*Order, error)
	FindNearbyDrivers(tenantID string, lat, lon, radiusKM float64, limit int) ([]*NearbyDriver, error)
}

// NearbyDriver is a read model for the FindNearbyDrivers query (PostGIS).
type NearbyDriver struct {
	DriverID   string
	DriverName string
	DistanceKM float64
	Lat        float64
	Lon        float64
}

func generateRef() string {
	return fmt.Sprintf("ORD-%d", time.Now().UnixNano()%1_000_000)
}

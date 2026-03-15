package grpc

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	dispatchv1 "github.com/fleetops/gen/dispatch/v1"
	"github.com/fleetops/dispatch-service/internal/domain"
	"github.com/fleetops/dispatch-service/internal/infrastructure/postgres"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Server implements dispatchv1.DispatchServiceServer.
type Server struct {
	repo *postgres.OrderRepo
	js   jetstream.JetStream
}

func NewServer(repo *postgres.OrderRepo, js jetstream.JetStream) *Server {
	return &Server{repo: repo, js: js}
}

func (s *Server) CreateOrder(ctx context.Context, req *dispatchv1.CreateOrderRequest) (*dispatchv1.Order, error) {
	wps := make([]*domain.Waypoint, len(req.Waypoints))
	for i, wp := range req.Waypoints {
		wps[i] = &domain.Waypoint{
			ID:       uuid.NewString(),
			Sequence: int(wp.Sequence),
			Address: domain.Address{
				Line1:    wp.Address.Line1,
				Line2:    wp.Address.Line2,
				City:     wp.Address.City,
				Postcode: wp.Address.Postcode,
				Country:  wp.Address.Country,
				Lat:      wp.Address.Lat,
				Lon:      wp.Address.Lon,
			},
			ContactName:  wp.ContactName,
			ContactPhone: wp.ContactPhone,
		}
		if wp.Window != nil {
			wps[i].Window.Earliest, _ = time.Parse(time.RFC3339, wp.Window.Earliest)
			wps[i].Window.Latest, _ = time.Parse(time.RFC3339, wp.Window.Latest)
		}
	}

	priority := domain.OrderPriority(req.Priority.String()[len("ORDER_PRIORITY_"):])
	order, err := domain.NewOrder(req.TenantId, priority, wps, req.Notes)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	if err := s.repo.Save(order); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return orderToProto(order), nil
}

func (s *Server) GetOrder(ctx context.Context, req *dispatchv1.GetOrderRequest) (*dispatchv1.Order, error) {
	o, err := s.repo.FindByID(req.OrderId)
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}
	return orderToProto(o), nil
}

func (s *Server) AssignDriver(ctx context.Context, req *dispatchv1.AssignDriverRequest) (*dispatchv1.DispatchAssignment, error) {
	o, err := s.repo.FindByID(req.OrderId)
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}

	assignment, err := o.Assign(req.DriverId, nil)
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}

	if err := s.repo.Update(o); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if err := s.repo.SaveAssignment(assignment); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	s.publish(ctx, domain.SubjectOrderAssigned, domain.OrderAssignedEvent{
		OrderID:    o.ID,
		TenantID:   o.TenantID,
		DriverID:   req.DriverId,
		AssignedAt: assignment.AssignedAt,
	})

	return assignmentToProto(assignment), nil
}

func (s *Server) UnassignDriver(ctx context.Context, req *dispatchv1.UnassignDriverRequest) (*dispatchv1.DispatchAssignment, error) {
	o, err := s.repo.FindByID(req.OrderId)
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}

	prev := o.Assignment
	if err := o.Unassign(); err != nil {
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}

	if err := s.repo.Update(o); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if prev != nil {
		_ = s.repo.SaveAssignment(prev) // marks active=false
	}

	return &dispatchv1.DispatchAssignment{Active: false}, nil
}

func (s *Server) UpdateOrderStatus(ctx context.Context, req *dispatchv1.UpdateOrderStatusRequest) (*dispatchv1.Order, error) {
	o, err := s.repo.FindByID(req.OrderId)
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}

	next := domain.OrderStatus(req.Status.String()[len("ORDER_STATUS_"):])
	if err := o.Transition(next); err != nil {
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}

	if err := s.repo.Update(o); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	if next == domain.StatusDelivered && o.Assignment != nil {
		s.publish(ctx, domain.SubjectOrderDelivered, domain.OrderDeliveredEvent{
			OrderID:     o.ID,
			TenantID:    o.TenantID,
			DriverID:    o.Assignment.DriverID,
			DistanceKM:  o.DistanceKM,
			DeliveredAt: time.Now().UTC(),
		})
	}

	return orderToProto(o), nil
}

func (s *Server) ListOrders(ctx context.Context, req *dispatchv1.ListOrdersRequest) (*dispatchv1.ListOrdersResponse, error) {
	pageSize := int(req.PageSize)
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}

	var statusFilter domain.OrderStatus
	if req.Status != dispatchv1.OrderStatus_ORDER_STATUS_UNSPECIFIED {
		statusFilter = domain.OrderStatus(req.Status.String()[len("ORDER_STATUS_"):])
	}

	orders, err := s.repo.List(req.TenantId, statusFilter, pageSize, 0)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	proto := make([]*dispatchv1.Order, len(orders))
	for i, o := range orders {
		proto[i] = orderToProto(o)
	}
	return &dispatchv1.ListOrdersResponse{Orders: proto}, nil
}

func (s *Server) FindNearbyDrivers(ctx context.Context, req *dispatchv1.FindNearbyDriversRequest) (*dispatchv1.FindNearbyDriversResponse, error) {
	limit := int(req.Limit)
	if limit <= 0 {
		limit = 10
	}

	nearby, err := s.repo.FindNearbyDrivers(req.TenantId, req.Lat, req.Lon, req.RadiusKm, limit)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	proto := make([]*dispatchv1.NearbyDriver, len(nearby))
	for i, nd := range nearby {
		proto[i] = &dispatchv1.NearbyDriver{
			DriverId:   nd.DriverID,
			DriverName: nd.DriverName,
			DistanceKm: nd.DistanceKM,
			Lat:        nd.Lat,
			Lon:        nd.Lon,
		}
	}
	return &dispatchv1.FindNearbyDriversResponse{Drivers: proto}, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func (s *Server) publish(ctx context.Context, subject string, payload any) {
	b, _ := json.Marshal(payload)
	_, _ = s.js.Publish(ctx, subject, b)
}

func orderToProto(o *domain.Order) *dispatchv1.Order {
	p := &dispatchv1.Order{
		Id:        o.ID,
		TenantId:  o.TenantID,
		Reference: o.Reference,
		Notes:     o.Notes,
		CreatedAt: o.CreatedAt.Format(time.RFC3339),
		UpdatedAt: o.UpdatedAt.Format(time.RFC3339),
	}

	for _, wp := range o.Waypoints {
		p.Waypoints = append(p.Waypoints, waypointToProto(wp))
	}

	if o.Assignment != nil {
		p.Assignment = assignmentToProto(o.Assignment)
	}
	return p
}

func waypointToProto(wp *domain.Waypoint) *dispatchv1.Waypoint {
	return &dispatchv1.Waypoint{
		Id:           wp.ID,
		Sequence:     int32(wp.Sequence),
		ContactName:  wp.ContactName,
		ContactPhone: wp.ContactPhone,
		Address: &dispatchv1.Address{
			Line1:    wp.Address.Line1,
			Line2:    wp.Address.Line2,
			City:     wp.Address.City,
			Postcode: wp.Address.Postcode,
			Country:  wp.Address.Country,
			Lat:      wp.Address.Lat,
			Lon:      wp.Address.Lon,
		},
		Window: &dispatchv1.TimeWindow{
			Earliest: fmt.Sprintf("%v", wp.Window.Earliest),
			Latest:   fmt.Sprintf("%v", wp.Window.Latest),
		},
	}
}

func assignmentToProto(a *domain.DispatchAssignment) *dispatchv1.DispatchAssignment {
	p := &dispatchv1.DispatchAssignment{
		Id:         a.ID,
		OrderId:    a.OrderID,
		DriverId:   a.DriverID,
		AssignedAt: a.AssignedAt.Format(time.RFC3339),
		Active:     a.Active,
	}
	if a.ETA != nil {
		p.Eta = a.ETA.Format(time.RFC3339)
	}
	return p
}

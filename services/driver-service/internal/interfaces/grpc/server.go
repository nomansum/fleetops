package grpc

import (
	"context"
	"time"

	driverv1 "github.com/fleetops/gen/driver/v1"
	"github.com/fleetops/driver-service/internal/domain"
	natspub "github.com/fleetops/driver-service/internal/infrastructure/nats"
	"github.com/fleetops/driver-service/internal/infrastructure/postgres"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Server implements driverv1.DriverServiceServer.
type Server struct {
	repo      *postgres.DriverRepo
	publisher *natspub.EventPublisher
}

func NewServer(repo *postgres.DriverRepo, pub *natspub.EventPublisher) *Server {
	return &Server{repo: repo, publisher: pub}
}

func (s *Server) CreateDriver(ctx context.Context, req *driverv1.CreateDriverRequest) (*driverv1.Driver, error) {
	d, err := domain.NewDriver(req.TenantId, req.UserId, req.Name, req.Phone, req.LicenseNumber)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	if err := s.repo.Save(d); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return domainToProto(d), nil
}

func (s *Server) GetDriver(ctx context.Context, req *driverv1.GetDriverRequest) (*driverv1.Driver, error) {
	d, err := s.repo.FindByID(req.DriverId)
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}
	return domainToProto(d), nil
}

func (s *Server) UpdateDriverStatus(ctx context.Context, req *driverv1.UpdateDriverStatusRequest) (*driverv1.Driver, error) {
	d, err := s.repo.FindByID(req.DriverId)
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}

	oldStatus := d.Status
	newStatus := domain.DriverStatus(req.Status.String()[len("DRIVER_STATUS_"):])

	if err := d.ChangeStatus(newStatus); err != nil {
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}

	if err := s.repo.Update(d); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	// Publish domain event
	_ = s.publisher.Publish(ctx, domain.SubjectDriverStatusChanged, domain.DriverStatusChangedEvent{
		DriverID:   d.ID,
		TenantID:   d.TenantID,
		OldStatus:  oldStatus,
		NewStatus:  d.Status,
		OccurredAt: time.Now().UTC(),
	})

	return domainToProto(d), nil
}

func (s *Server) SuspendDriver(ctx context.Context, req *driverv1.SuspendDriverRequest) (*driverv1.Driver, error) {
	d, err := s.repo.FindByID(req.DriverId)
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}

	oldStatus := d.Status
	if err := d.Suspend(req.Reason); err != nil {
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}

	if err := s.repo.Update(d); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	_ = s.publisher.Publish(ctx, domain.SubjectDriverStatusChanged, domain.DriverStatusChangedEvent{
		DriverID:   d.ID,
		TenantID:   d.TenantID,
		OldStatus:  oldStatus,
		NewStatus:  d.Status,
		OccurredAt: time.Now().UTC(),
	})

	return domainToProto(d), nil
}

func (s *Server) ListAvailableDrivers(ctx context.Context, req *driverv1.ListAvailableDriversRequest) (*driverv1.ListAvailableDriversResponse, error) {
	pageSize := int(req.PageSize)
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}

	drivers, err := s.repo.ListAvailable(req.TenantId, pageSize, 0)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	proto := make([]*driverv1.Driver, len(drivers))
	for i, d := range drivers {
		proto[i] = domainToProto(d)
	}

	return &driverv1.ListAvailableDriversResponse{Drivers: proto}, nil
}

func (s *Server) AssignVehicle(ctx context.Context, req *driverv1.AssignVehicleRequest) (*driverv1.Vehicle, error) {
	d, err := s.repo.FindByID(req.DriverId)
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}

	v := &domain.Vehicle{
		ID:          uuid.NewString(),
		DriverID:    req.DriverId,
		PlateNumber: req.PlateNumber,
		Type:        domain.VehicleType(req.Type.String()[len("VEHICLE_TYPE_"):]),
		CapacityKG:  req.CapacityKg,
		Make:        req.Make,
		Model:       req.Model,
		Year:        int(req.Year),
	}

	d.AssignVehicle(v)

	if err := s.repo.SaveVehicle(v); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return vehicleToProto(v), nil
}

func (s *Server) AddDocument(ctx context.Context, req *driverv1.AddDocumentRequest) (*driverv1.Document, error) {
	expiry, err := time.Parse("2006-01-02", req.ExpiryDate)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "expiry_date must be YYYY-MM-DD")
	}

	doc := &domain.Document{
		ID:                 uuid.NewString(),
		DriverID:           req.DriverId,
		Type:               domain.DocumentType(req.Type.String()[len("DOCUMENT_TYPE_"):]),
		ReferenceNumber:    req.ReferenceNumber,
		ExpiryDate:         expiry,
		VerificationStatus: domain.VerificationPending,
		FileURL:            req.FileUrl,
		UploadedAt:         time.Now().UTC(),
	}

	if err := s.repo.SaveDocument(doc); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return documentToProto(doc), nil
}

func (s *Server) ListExpiringDocuments(ctx context.Context, req *driverv1.ListExpiringDocumentsRequest) (*driverv1.ListExpiringDocumentsResponse, error) {
	pageSize := int(req.PageSize)
	if pageSize <= 0 {
		pageSize = 50
	}

	docs, err := s.repo.ListExpiringDocuments(int(req.WithinDays), pageSize, 0)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	proto := make([]*driverv1.Document, len(docs))
	for i, d := range docs {
		proto[i] = documentToProto(d)
	}

	return &driverv1.ListExpiringDocumentsResponse{Documents: proto}, nil
}

// ── proto mappers ─────────────────────────────────────────────────────────────

func domainToProto(d *domain.Driver) *driverv1.Driver {
	p := &driverv1.Driver{
		Id:            d.ID,
		TenantId:      d.TenantID,
		UserId:        d.UserID,
		Name:          d.Name,
		Phone:         d.Phone,
		LicenseNumber: d.LicenseNumber,
		Status:        protoStatus(d.Status),
		CreatedAt:     d.CreatedAt.Format(time.RFC3339),
		UpdatedAt:     d.UpdatedAt.Format(time.RFC3339),
	}
	if d.Vehicle != nil {
		p.Vehicle = vehicleToProto(d.Vehicle)
	}
	for _, doc := range d.Documents {
		p.Documents = append(p.Documents, documentToProto(doc))
	}
	return p
}

func vehicleToProto(v *domain.Vehicle) *driverv1.Vehicle {
	return &driverv1.Vehicle{
		Id:          v.ID,
		DriverId:    v.DriverID,
		PlateNumber: v.PlateNumber,
		CapacityKg:  v.CapacityKG,
		Make:        v.Make,
		Model:       v.Model,
		Year:        int32(v.Year),
		Active:      v.Active,
	}
}

func documentToProto(d *domain.Document) *driverv1.Document {
	return &driverv1.Document{
		Id:              d.ID,
		DriverId:        d.DriverID,
		ReferenceNumber: d.ReferenceNumber,
		ExpiryDate:      d.ExpiryDate.Format("2006-01-02"),
		FileUrl:         d.FileURL,
		UploadedAt:      d.UploadedAt.Format(time.RFC3339),
	}
}

func protoStatus(s domain.DriverStatus) driverv1.DriverStatus {
	switch s {
	case domain.StatusAvailable:
		return driverv1.DriverStatus_DRIVER_STATUS_AVAILABLE
	case domain.StatusOnDelivery:
		return driverv1.DriverStatus_DRIVER_STATUS_ON_DELIVERY
	case domain.StatusOffline:
		return driverv1.DriverStatus_DRIVER_STATUS_OFFLINE
	case domain.StatusSuspended:
		return driverv1.DriverStatus_DRIVER_STATUS_SUSPENDED
	default:
		return driverv1.DriverStatus_DRIVER_STATUS_UNSPECIFIED
	}
}

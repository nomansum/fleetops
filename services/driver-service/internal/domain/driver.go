package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// ── Value Objects ─────────────────────────────────────────────────────────────

type DriverStatus string

const (
	StatusAvailable  DriverStatus = "AVAILABLE"
	StatusOnDelivery DriverStatus = "ON_DELIVERY"
	StatusOffline    DriverStatus = "OFFLINE"
	StatusSuspended  DriverStatus = "SUSPENDED"
)

type VehicleType string

const (
	VehicleMotorcycle VehicleType = "MOTORCYCLE"
	VehicleCar        VehicleType = "CAR"
	VehicleVan        VehicleType = "VAN"
	VehicleTruck      VehicleType = "TRUCK"
)

type DocumentType string

const (
	DocumentLicense   DocumentType = "LICENSE"
	DocumentInsurance DocumentType = "INSURANCE"
	DocumentMOT       DocumentType = "MOT"
	DocumentPermit    DocumentType = "PERMIT"
)

type VerificationStatus string

const (
	VerificationPending  VerificationStatus = "PENDING"
	VerificationVerified VerificationStatus = "VERIFIED"
	VerificationRejected VerificationStatus = "REJECTED"
	VerificationExpired  VerificationStatus = "EXPIRED"
)

// ── Entities ──────────────────────────────────────────────────────────────────

// Vehicle is an entity owned by Driver.
type Vehicle struct {
	ID          string
	DriverID    string
	PlateNumber string
	Type        VehicleType
	CapacityKG  float64
	Make        string
	Model       string
	Year        int
	Active      bool
}

// Document is a compliance document entity owned by Driver.
type Document struct {
	ID                 string
	DriverID           string
	Type               DocumentType
	ReferenceNumber    string
	ExpiryDate         time.Time
	VerificationStatus VerificationStatus
	FileURL            string
	UploadedAt         time.Time
}

func (d *Document) IsExpired() bool {
	return time.Now().UTC().After(d.ExpiryDate)
}

// ── Aggregate Root ────────────────────────────────────────────────────────────

// Driver is the aggregate root for the Driver Management bounded context.
type Driver struct {
	ID            string
	TenantID      string
	UserID        string
	Name          string
	Phone         string
	LicenseNumber string
	Status        DriverStatus
	Vehicle       *Vehicle
	Documents     []*Document
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// NewDriver validates and constructs a Driver aggregate.
func NewDriver(tenantID, userID, name, phone, licenseNumber string) (*Driver, error) {
	if tenantID == "" {
		return nil, errors.New("driver: tenant_id is required")
	}
	if name == "" {
		return nil, errors.New("driver: name is required")
	}
	if licenseNumber == "" {
		return nil, errors.New("driver: license_number is required")
	}

	now := time.Now().UTC()
	return &Driver{
		ID:            uuid.NewString(),
		TenantID:      tenantID,
		UserID:        userID,
		Name:          name,
		Phone:         phone,
		LicenseNumber: licenseNumber,
		Status:        StatusOffline,
		Documents:     []*Document{},
		CreatedAt:     now,
		UpdatedAt:     now,
	}, nil
}

// ChangeStatus enforces status transition rules.
func (d *Driver) ChangeStatus(s DriverStatus) error {
	if d.Status == StatusSuspended && s != StatusSuspended {
		return errors.New("driver: suspended driver cannot change status")
	}
	d.Status = s
	d.UpdatedAt = time.Now().UTC()
	return nil
}

// Suspend marks the driver as suspended (compliance violation, document expiry, etc.)
func (d *Driver) Suspend(reason string) error {
	if d.Status == StatusSuspended {
		return nil
	}
	d.Status = StatusSuspended
	d.UpdatedAt = time.Now().UTC()
	return nil
}

// HasExpiredDocuments returns true if any required document is expired.
func (d *Driver) HasExpiredDocuments() bool {
	for _, doc := range d.Documents {
		if doc.IsExpired() && doc.VerificationStatus == VerificationVerified {
			return true
		}
	}
	return false
}

// AssignVehicle replaces the driver's active vehicle.
func (d *Driver) AssignVehicle(v *Vehicle) {
	if d.Vehicle != nil {
		d.Vehicle.Active = false
	}
	v.Active = true
	d.Vehicle = v
	d.UpdatedAt = time.Now().UTC()
}

// AddDocument appends a new compliance document.
func (d *Driver) AddDocument(doc *Document) {
	d.Documents = append(d.Documents, doc)
	d.UpdatedAt = time.Now().UTC()
}

// ── Repository Interface ──────────────────────────────────────────────────────

// DriverRepository defines persistence operations for the Driver aggregate.
type DriverRepository interface {
	Save(d *Driver) error
	FindByID(id string) (*Driver, error)
	ListAvailable(tenantID string, limit, offset int) ([]*Driver, error)
	Update(d *Driver) error
	ListExpiringDocuments(withinDays int, limit, offset int) ([]*Document, error)
}

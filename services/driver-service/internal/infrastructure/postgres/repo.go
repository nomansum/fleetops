package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/fleetops/driver-service/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DriverRepo implements domain.DriverRepository.
type DriverRepo struct{ db *pgxpool.Pool }

func NewDriverRepo(db *pgxpool.Pool) *DriverRepo { return &DriverRepo{db: db} }

func (r *DriverRepo) Save(d *domain.Driver) error {
	tx, err := r.db.Begin(context.Background())
	if err != nil {
		return wrap("begin tx", err)
	}
	defer tx.Rollback(context.Background())

	_, err = tx.Exec(context.Background(), `
		INSERT INTO drivers (id,tenant_id,user_id,name,phone,license_number,status,created_at,updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		d.ID, d.TenantID, d.UserID, d.Name, d.Phone, d.LicenseNumber,
		string(d.Status), d.CreatedAt, d.UpdatedAt,
	)
	if err != nil {
		return wrap("insert driver", err)
	}

	return wrap("commit", tx.Commit(context.Background()))
}

func (r *DriverRepo) FindByID(id string) (*domain.Driver, error) {
	row := r.db.QueryRow(context.Background(), `
		SELECT id,tenant_id,user_id,name,phone,license_number,status,created_at,updated_at
		FROM drivers WHERE id=$1`, id)

	d, err := scanDriver(row)
	if err != nil {
		return nil, err
	}

	// Load vehicle
	d.Vehicle, _ = r.findVehicle(id)

	// Load documents
	d.Documents, _ = r.findDocuments(id)

	return d, nil
}

func (r *DriverRepo) ListAvailable(tenantID string, limit, offset int) ([]*domain.Driver, error) {
	rows, err := r.db.Query(context.Background(), `
		SELECT id,tenant_id,user_id,name,phone,license_number,status,created_at,updated_at
		FROM drivers WHERE tenant_id=$1 AND status='AVAILABLE'
		ORDER BY updated_at DESC LIMIT $2 OFFSET $3`,
		tenantID, limit, offset)
	if err != nil {
		return nil, wrap("list available", err)
	}
	defer rows.Close()

	return collectDrivers(rows)
}

func (r *DriverRepo) Update(d *domain.Driver) error {
	d.UpdatedAt = time.Now().UTC()
	_, err := r.db.Exec(context.Background(), `
		UPDATE drivers SET name=$1,phone=$2,status=$3,updated_at=$4 WHERE id=$5`,
		d.Name, d.Phone, string(d.Status), d.UpdatedAt, d.ID)
	return wrap("update driver", err)
}

func (r *DriverRepo) SaveVehicle(v *domain.Vehicle) error {
	_, err := r.db.Exec(context.Background(), `
		INSERT INTO vehicles (id,driver_id,plate_number,type,capacity_kg,make,model,year,active)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (id) DO UPDATE SET active=$9,updated_at=NOW()`,
		v.ID, v.DriverID, v.PlateNumber, string(v.Type), v.CapacityKG,
		v.Make, v.Model, v.Year, v.Active)
	return wrap("save vehicle", err)
}

func (r *DriverRepo) SaveDocument(doc *domain.Document) error {
	_, err := r.db.Exec(context.Background(), `
		INSERT INTO documents (id,driver_id,type,reference_number,expiry_date,verification_status,file_url,uploaded_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		doc.ID, doc.DriverID, string(doc.Type), doc.ReferenceNumber,
		doc.ExpiryDate, string(doc.VerificationStatus), doc.FileURL, doc.UploadedAt)
	return wrap("save document", err)
}

func (r *DriverRepo) ListExpiringDocuments(withinDays, limit, offset int) ([]*domain.Document, error) {
	rows, err := r.db.Query(context.Background(), `
		SELECT d.id,d.driver_id,d.type,d.reference_number,d.expiry_date,
		       d.verification_status,d.file_url,d.uploaded_at
		FROM documents d
		JOIN drivers dr ON dr.id=d.driver_id
		WHERE d.expiry_date BETWEEN NOW() AND NOW() + ($1 || ' days')::interval
		  AND d.verification_status='VERIFIED'
		ORDER BY d.expiry_date ASC LIMIT $2 OFFSET $3`,
		withinDays, limit, offset)
	if err != nil {
		return nil, wrap("list expiring", err)
	}
	defer rows.Close()

	var docs []*domain.Document
	for rows.Next() {
		doc := &domain.Document{}
		var docType, verStatus string
		if err := rows.Scan(&doc.ID, &doc.DriverID, &docType, &doc.ReferenceNumber,
			&doc.ExpiryDate, &verStatus, &doc.FileURL, &doc.UploadedAt); err != nil {
			return nil, wrap("scan document", err)
		}
		doc.Type = domain.DocumentType(docType)
		doc.VerificationStatus = domain.VerificationStatus(verStatus)
		docs = append(docs, doc)
	}
	return docs, rows.Err()
}

// ── helpers ───────────────────────────────────────────────────────────────────

func (r *DriverRepo) findVehicle(driverID string) (*domain.Vehicle, error) {
	row := r.db.QueryRow(context.Background(), `
		SELECT id,driver_id,plate_number,type,capacity_kg,make,model,year,active
		FROM vehicles WHERE driver_id=$1 AND active=TRUE LIMIT 1`, driverID)

	v := &domain.Vehicle{}
	var vtype string
	err := row.Scan(&v.ID, &v.DriverID, &v.PlateNumber, &vtype,
		&v.CapacityKG, &v.Make, &v.Model, &v.Year, &v.Active)
	if err != nil {
		return nil, err
	}
	v.Type = domain.VehicleType(vtype)
	return v, nil
}

func (r *DriverRepo) findDocuments(driverID string) ([]*domain.Document, error) {
	rows, err := r.db.Query(context.Background(), `
		SELECT id,driver_id,type,reference_number,expiry_date,verification_status,file_url,uploaded_at
		FROM documents WHERE driver_id=$1 ORDER BY uploaded_at DESC`, driverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var docs []*domain.Document
	for rows.Next() {
		doc := &domain.Document{}
		var docType, verStatus string
		if err := rows.Scan(&doc.ID, &doc.DriverID, &docType, &doc.ReferenceNumber,
			&doc.ExpiryDate, &verStatus, &doc.FileURL, &doc.UploadedAt); err != nil {
			continue
		}
		doc.Type = domain.DocumentType(docType)
		doc.VerificationStatus = domain.VerificationStatus(verStatus)
		docs = append(docs, doc)
	}
	return docs, nil
}

type rowScanner interface{ Scan(dest ...any) error }

func scanDriver(s rowScanner) (*domain.Driver, error) {
	d := &domain.Driver{}
	var status string
	err := s.Scan(&d.ID, &d.TenantID, &d.UserID, &d.Name, &d.Phone,
		&d.LicenseNumber, &status, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("driver not found")
		}
		return nil, wrap("scan driver", err)
	}
	d.Status = domain.DriverStatus(status)
	return d, nil
}

func collectDrivers(rows pgx.Rows) ([]*domain.Driver, error) {
	var drivers []*domain.Driver
	for rows.Next() {
		d, err := scanDriver(rows)
		if err != nil {
			return nil, err
		}
		drivers = append(drivers, d)
	}
	return drivers, rows.Err()
}

func wrap(op string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("driver postgres %s: %w", op, err)
}

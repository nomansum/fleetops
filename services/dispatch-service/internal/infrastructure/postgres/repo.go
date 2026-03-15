package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/fleetops/dispatch-service/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// OrderRepo implements domain.OrderRepository using PostgreSQL + PostGIS.
type OrderRepo struct{ db *pgxpool.Pool }

func NewOrderRepo(db *pgxpool.Pool) *OrderRepo { return &OrderRepo{db: db} }

func (r *OrderRepo) Save(o *domain.Order) error {
	tx, err := r.db.Begin(context.Background())
	if err != nil {
		return wrap("begin", err)
	}
	defer tx.Rollback(context.Background())

	_, err = tx.Exec(context.Background(), `
		INSERT INTO orders (id,tenant_id,reference,status,priority,distance_km,notes,created_at,updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		o.ID, o.TenantID, o.Reference, string(o.Status), string(o.Priority),
		o.DistanceKM, o.Notes, o.CreatedAt, o.UpdatedAt,
	)
	if err != nil {
		return wrap("insert order", err)
	}

	for _, wp := range o.Waypoints {
		_, err = tx.Exec(context.Background(), `
			INSERT INTO waypoints (id,order_id,sequence,line1,line2,city,postcode,country,
				location,contact_name,contact_phone,window_earliest,window_latest)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,
				ST_SetSRID(ST_MakePoint($10,$9),4326),
				$11,$12,$13,$14)`,
			wp.ID, o.ID, wp.Sequence,
			wp.Address.Line1, wp.Address.Line2, wp.Address.City,
			wp.Address.Postcode, wp.Address.Country,
			wp.Address.Lat, wp.Address.Lon,
			wp.ContactName, wp.ContactPhone,
			wp.Window.Earliest, wp.Window.Latest,
		)
		if err != nil {
			return wrap("insert waypoint", err)
		}
	}

	return wrap("commit", tx.Commit(context.Background()))
}

func (r *OrderRepo) FindByID(id string) (*domain.Order, error) {
	row := r.db.QueryRow(context.Background(), `
		SELECT id,tenant_id,reference,status,priority,distance_km,notes,created_at,updated_at
		FROM orders WHERE id=$1`, id)

	o, err := scanOrder(row)
	if err != nil {
		return nil, err
	}

	o.Waypoints, _ = r.loadWaypoints(o.ID)
	o.Assignment, _ = r.loadAssignment(o.ID)

	return o, nil
}

func (r *OrderRepo) List(tenantID string, statusFilter domain.OrderStatus, limit, offset int) ([]*domain.Order, error) {
	query := `SELECT id,tenant_id,reference,status,priority,distance_km,notes,created_at,updated_at
		FROM orders WHERE tenant_id=$1`
	args := []any{tenantID}

	if statusFilter != "" {
		query += " AND status=$2 ORDER BY created_at DESC LIMIT $3 OFFSET $4"
		args = append(args, string(statusFilter), limit, offset)
	} else {
		query += " ORDER BY created_at DESC LIMIT $2 OFFSET $3"
		args = append(args, limit, offset)
	}

	rows, err := r.db.Query(context.Background(), query, args...)
	if err != nil {
		return nil, wrap("list orders", err)
	}
	defer rows.Close()

	var orders []*domain.Order
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			return nil, err
		}
		orders = append(orders, o)
	}
	return orders, rows.Err()
}

func (r *OrderRepo) Update(o *domain.Order) error {
	o.UpdatedAt = time.Now().UTC()
	_, err := r.db.Exec(context.Background(), `
		UPDATE orders SET status=$1,distance_km=$2,updated_at=$3 WHERE id=$4`,
		string(o.Status), o.DistanceKM, o.UpdatedAt, o.ID)
	return wrap("update order", err)
}

func (r *OrderRepo) SaveAssignment(a *domain.DispatchAssignment) error {
	_, err := r.db.Exec(context.Background(), `
		INSERT INTO assignments (id,order_id,driver_id,assigned_at,eta,active)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (id) DO UPDATE SET active=$6`,
		a.ID, a.OrderID, a.DriverID, a.AssignedAt, a.ETA, a.Active)
	return wrap("save assignment", err)
}

// FindStaleAssignments returns ASSIGNED orders older than the given duration.
func (r *OrderRepo) FindStaleAssignments(olderThan time.Duration) ([]*domain.Order, error) {
	cutoff := time.Now().UTC().Add(-olderThan)
	rows, err := r.db.Query(context.Background(), `
		SELECT id,tenant_id,reference,status,priority,distance_km,notes,created_at,updated_at
		FROM orders WHERE status='ASSIGNED' AND updated_at < $1`, cutoff)
	if err != nil {
		return nil, wrap("stale assignments", err)
	}
	defer rows.Close()

	var orders []*domain.Order
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			return nil, err
		}
		orders = append(orders, o)
	}
	return orders, rows.Err()
}

// FindNearbyDrivers uses PostGIS ST_DWithin to find available drivers within radiusKM.
func (r *OrderRepo) FindNearbyDrivers(tenantID string, lat, lon, radiusKM float64, limit int) ([]*domain.NearbyDriver, error) {
	rows, err := r.db.Query(context.Background(), `
		SELECT driver_id, driver_name,
		       ST_Distance(location, ST_SetSRID(ST_MakePoint($3,$2),4326)::geography) / 1000 AS dist_km,
		       ST_Y(location::geometry) AS lat,
		       ST_X(location::geometry) AS lon
		FROM driver_locations
		WHERE tenant_id=$1
		  AND ST_DWithin(location, ST_SetSRID(ST_MakePoint($3,$2),4326)::geography, $4 * 1000)
		ORDER BY dist_km ASC
		LIMIT $5`,
		tenantID, lat, lon, radiusKM, limit,
	)
	if err != nil {
		return nil, wrap("nearby drivers", err)
	}
	defer rows.Close()

	var result []*domain.NearbyDriver
	for rows.Next() {
		nd := &domain.NearbyDriver{}
		if err := rows.Scan(&nd.DriverID, &nd.DriverName, &nd.DistanceKM, &nd.Lat, &nd.Lon); err != nil {
			return nil, wrap("scan nearby driver", err)
		}
		result = append(result, nd)
	}
	return result, rows.Err()
}

func (r *OrderRepo) UpsertDriverLocation(driverID, tenantID, driverName string, lat, lon float64) error {
	_, err := r.db.Exec(context.Background(), `
		INSERT INTO driver_locations (driver_id,tenant_id,driver_name,location,updated_at)
		VALUES ($1,$2,$3,ST_SetSRID(ST_MakePoint($5,$4),4326)::geography,NOW())
		ON CONFLICT (driver_id) DO UPDATE SET location=EXCLUDED.location, updated_at=NOW()`,
		driverID, tenantID, driverName, lat, lon)
	return wrap("upsert driver location", err)
}

// ── helpers ───────────────────────────────────────────────────────────────────

type scanner interface{ Scan(dest ...any) error }

func scanOrder(s scanner) (*domain.Order, error) {
	o := &domain.Order{}
	var status, priority string
	err := s.Scan(&o.ID, &o.TenantID, &o.Reference, &status, &priority,
		&o.DistanceKM, &o.Notes, &o.CreatedAt, &o.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("order not found")
		}
		return nil, wrap("scan order", err)
	}
	o.Status = domain.OrderStatus(status)
	o.Priority = domain.OrderPriority(priority)
	return o, nil
}

func (r *OrderRepo) loadWaypoints(orderID string) ([]*domain.Waypoint, error) {
	rows, err := r.db.Query(context.Background(), `
		SELECT id,sequence,line1,line2,city,postcode,country,
		       ST_Y(location::geometry),ST_X(location::geometry),
		       contact_name,contact_phone,window_earliest,window_latest
		FROM waypoints WHERE order_id=$1 ORDER BY sequence`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var wps []*domain.Waypoint
	for rows.Next() {
		wp := &domain.Waypoint{Address: domain.Address{}}
		err := rows.Scan(&wp.ID, &wp.Sequence,
			&wp.Address.Line1, &wp.Address.Line2, &wp.Address.City,
			&wp.Address.Postcode, &wp.Address.Country,
			&wp.Address.Lat, &wp.Address.Lon,
			&wp.ContactName, &wp.ContactPhone,
			&wp.Window.Earliest, &wp.Window.Latest)
		if err != nil {
			continue
		}
		wps = append(wps, wp)
	}
	return wps, nil
}

func (r *OrderRepo) loadAssignment(orderID string) (*domain.DispatchAssignment, error) {
	row := r.db.QueryRow(context.Background(), `
		SELECT id,order_id,driver_id,assigned_at,eta,active
		FROM assignments WHERE order_id=$1 AND active=TRUE LIMIT 1`, orderID)

	a := &domain.DispatchAssignment{}
	err := row.Scan(&a.ID, &a.OrderID, &a.DriverID, &a.AssignedAt, &a.ETA, &a.Active)
	if err != nil {
		return nil, err
	}
	return a, nil
}

func wrap(op string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("dispatch postgres %s: %w", op, err)
}

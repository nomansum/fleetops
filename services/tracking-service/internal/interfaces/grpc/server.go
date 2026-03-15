package grpc

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	trackingv1 "github.com/fleetops/gen/tracking/v1"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const subjectLocationUpdated = "fleet.location.updated"

// Server implements trackingv1.TrackingServiceServer.
type Server struct {
	db *pgxpool.Pool
	js jetstream.JetStream
}

func NewServer(db *pgxpool.Pool, js jetstream.JetStream) *Server {
	return &Server{db: db, js: js}
}

// IngestLocation writes a location ping to the TimescaleDB hypertable and
// publishes a fleet.location.updated event to NATS.
func (s *Server) IngestLocation(ctx context.Context, req *trackingv1.IngestLocationRequest) (*trackingv1.IngestLocationResponse, error) {
	ts, err := time.Parse(time.RFC3339, req.Timestamp)
	if err != nil {
		ts = time.Now().UTC()
	}

	_, err = s.db.Exec(ctx, `
		INSERT INTO location_pings (driver_id, session_id, ts, lat, lon, speed_kmh, heading)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		req.DriverId, req.SessionId, ts,
		req.Position.Lat, req.Position.Lon,
		req.SpeedKmh, req.Heading,
	)
	if err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("insert ping: %v", err))
	}

	// Publish for dispatch-service to update driver_locations (PostGIS) and for UI streaming
	event := map[string]any{
		"driver_id":  req.DriverId,
		"session_id": req.SessionId,
		"lat":        req.Position.Lat,
		"lon":        req.Position.Lon,
		"speed_kmh":  req.SpeedKmh,
		"heading":    req.Heading,
		"ts":         ts.Format(time.RFC3339),
	}
	b, _ := json.Marshal(event)
	_, _ = s.js.Publish(ctx, subjectLocationUpdated, b)

	return &trackingv1.IngestLocationResponse{Accepted: true}, nil
}

// StreamDriverLocation is a server-streaming RPC.
// The api-gateway holds this open and forwards pings to a WebSocket client.
// Here we poll the DB every second — in production replace with a NATS subscriber.
func (s *Server) StreamDriverLocation(req *trackingv1.StreamDriverLocationRequest, stream trackingv1.TrackingService_StreamDriverLocationServer) error {
	ctx := stream.Context()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			row := s.db.QueryRow(ctx, `
				SELECT lat, lon, speed_kmh, heading, ts
				FROM location_pings
				WHERE driver_id=$1
				ORDER BY ts DESC LIMIT 1`,
				req.DriverId,
			)

			var lat, lon, speed, heading float64
			var ts time.Time
			if err := row.Scan(&lat, &lon, &speed, &heading, &ts); err != nil {
				continue // no data yet
			}

			if err := stream.Send(&trackingv1.LocationEvent{
				DriverId:  req.DriverId,
				Position:  &trackingv1.GeoPoint{Lat: lat, Lon: lon},
				SpeedKmh:  speed,
				Heading:   heading,
				Timestamp: ts.Format(time.RFC3339),
			}); err != nil {
				return err
			}
		}
	}
}

func (s *Server) GetLastKnownLocation(ctx context.Context, req *trackingv1.GetLastKnownLocationRequest) (*trackingv1.LocationEvent, error) {
	row := s.db.QueryRow(ctx, `
		SELECT driver_id, session_id, lat, lon, speed_kmh, heading, ts
		FROM location_pings WHERE driver_id=$1 ORDER BY ts DESC LIMIT 1`,
		req.DriverId,
	)

	var driverID, sessionID string
	var lat, lon, speed, heading float64
	var ts time.Time

	if err := row.Scan(&driverID, &sessionID, &lat, &lon, &speed, &heading, &ts); err != nil {
		return nil, status.Error(codes.NotFound, "no location data")
	}

	return &trackingv1.LocationEvent{
		DriverId:  driverID,
		SessionId: sessionID,
		Position:  &trackingv1.GeoPoint{Lat: lat, Lon: lon},
		SpeedKmh:  speed,
		Heading:   heading,
		Timestamp: ts.Format(time.RFC3339),
	}, nil
}

func (s *Server) GetRouteHistory(ctx context.Context, req *trackingv1.GetRouteHistoryRequest) (*trackingv1.GetRouteHistoryResponse, error) {
	from, _ := time.Parse(time.RFC3339, req.From)
	to, _ := time.Parse(time.RFC3339, req.To)

	rows, err := s.db.Query(ctx, `
		SELECT lat, lon, speed_kmh, ts
		FROM location_pings
		WHERE driver_id=$1 AND session_id=$2 AND ts BETWEEN $3 AND $4
		ORDER BY ts ASC`,
		req.DriverId, req.SessionId, from, to,
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	defer rows.Close()

	var points []*trackingv1.RoutePoint
	var totalSpeed float64

	for rows.Next() {
		var lat, lon, speed float64
		var ts time.Time
		if err := rows.Scan(&lat, &lon, &speed, &ts); err != nil {
			continue
		}
		points = append(points, &trackingv1.RoutePoint{
			Position:  &trackingv1.GeoPoint{Lat: lat, Lon: lon},
			SpeedKmh:  speed,
			Timestamp: ts.Format(time.RFC3339),
		})
		totalSpeed += speed
	}

	var avgSpeed float64
	if len(points) > 0 {
		avgSpeed = totalSpeed / float64(len(points))
	}

	return &trackingv1.GetRouteHistoryResponse{
		Points:   points,
		AvgSpeed: avgSpeed,
	}, nil
}

func (s *Server) UpsertGeofence(ctx context.Context, req *trackingv1.UpsertGeofenceRequest) (*trackingv1.Geofence, error) {
	id := fmt.Sprintf("gf-%d", time.Now().UnixNano())
	polygon, _ := json.Marshal(req.Polygon.Points)

	_, err := s.db.Exec(ctx, `
		INSERT INTO geofences (id,tenant_id,name,type,polygon,active)
		VALUES ($1,$2,$3,$4,$5,TRUE)
		ON CONFLICT (id) DO UPDATE SET name=$3,type=$4,polygon=$5`,
		id, req.TenantId, req.Name, req.Type.String(), string(polygon))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &trackingv1.Geofence{
		Id:       id,
		TenantId: req.TenantId,
		Name:     req.Name,
		Type:     req.Type,
		Polygon:  req.Polygon,
		Active:   true,
	}, nil
}

func (s *Server) CheckGeofence(ctx context.Context, req *trackingv1.CheckGeofenceRequest) (*trackingv1.CheckGeofenceResponse, error) {
	// Simple point-in-polygon check using stored polygon JSON
	// In production, use PostGIS geography types for geofence storage too
	rows, err := s.db.Query(ctx, `
		SELECT id,name,type,polygon FROM geofences WHERE tenant_id=$1 AND active=TRUE`,
		req.TenantId)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	defer rows.Close()

	var matched []*trackingv1.Geofence
	for rows.Next() {
		var id, name, gtype, polygonJSON string
		if err := rows.Scan(&id, &name, &gtype, &polygonJSON); err != nil {
			continue
		}
		// TODO: implement point-in-polygon check
		_ = polygonJSON
		matched = append(matched, &trackingv1.Geofence{
			Id: id, Name: name,
		})
	}

	return &trackingv1.CheckGeofenceResponse{MatchedZones: matched}, nil
}

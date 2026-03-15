-- Dispatch Service schema (requires PostGIS extension)

CREATE EXTENSION IF NOT EXISTS postgis;

CREATE TABLE IF NOT EXISTS orders (
    id           TEXT        PRIMARY KEY,
    tenant_id    TEXT        NOT NULL,
    reference    TEXT        NOT NULL UNIQUE,
    status       TEXT        NOT NULL DEFAULT 'PENDING',
    priority     TEXT        NOT NULL DEFAULT 'STANDARD',
    distance_km  DOUBLE PRECISION NOT NULL DEFAULT 0,
    notes        TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS waypoints (
    id            TEXT        PRIMARY KEY,
    order_id      TEXT        NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    sequence      INT         NOT NULL,
    line1         TEXT,
    line2         TEXT,
    city          TEXT,
    postcode      TEXT,
    country       TEXT,
    -- PostGIS geography point (lon,lat) for radius queries
    location      GEOGRAPHY(POINT, 4326),
    contact_name  TEXT,
    contact_phone TEXT,
    window_earliest TIMESTAMPTZ,
    window_latest   TIMESTAMPTZ,
    arrived_at    TIMESTAMPTZ,
    completed_at  TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS assignments (
    id          TEXT        PRIMARY KEY,
    order_id    TEXT        NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    driver_id   TEXT        NOT NULL,
    assigned_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    eta         TIMESTAMPTZ,
    active      BOOLEAN     NOT NULL DEFAULT TRUE
);

-- Driver last-known location table (updated by tracking events consumed by dispatch)
-- Used for the FindNearbyDrivers PostGIS query
CREATE TABLE IF NOT EXISTS driver_locations (
    driver_id   TEXT        PRIMARY KEY,
    tenant_id   TEXT        NOT NULL,
    driver_name TEXT,
    location    GEOGRAPHY(POINT, 4326) NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_orders_tenant_status   ON orders(tenant_id, status);
CREATE INDEX IF NOT EXISTS idx_assignments_order      ON assignments(order_id, active);
CREATE INDEX IF NOT EXISTS idx_driver_locations_geom  ON driver_locations USING GIST(location);
CREATE INDEX IF NOT EXISTS idx_waypoints_order        ON waypoints(order_id, sequence);

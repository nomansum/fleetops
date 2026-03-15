-- Tracking Service schema (TimescaleDB)
-- TimescaleDB must be installed: CREATE EXTENSION IF NOT EXISTS timescaledb;

CREATE EXTENSION IF NOT EXISTS timescaledb;

-- Hypertable: raw location pings partitioned by time (1-day chunks)
CREATE TABLE IF NOT EXISTS location_pings (
    driver_id   TEXT             NOT NULL,
    session_id  TEXT             NOT NULL,
    ts          TIMESTAMPTZ      NOT NULL,
    lat         DOUBLE PRECISION NOT NULL,
    lon         DOUBLE PRECISION NOT NULL,
    speed_kmh   DOUBLE PRECISION NOT NULL DEFAULT 0,
    heading     DOUBLE PRECISION NOT NULL DEFAULT 0
);

-- Convert to TimescaleDB hypertable (partitioned on ts, 1 day per chunk)
SELECT create_hypertable('location_pings', 'ts', if_not_exists => TRUE);

-- Continuous aggregate: hourly route summaries (refreshed every hour by TimescaleDB)
CREATE MATERIALIZED VIEW IF NOT EXISTS hourly_route_summary
WITH (timescaledb.continuous) AS
SELECT
    driver_id,
    time_bucket('1 hour', ts) AS hour,
    COUNT(*)                   AS ping_count,
    SUM(speed_kmh) / COUNT(*) AS avg_speed_kmh,
    MAX(speed_kmh)             AS max_speed_kmh
FROM location_pings
GROUP BY driver_id, hour
WITH NO DATA;

-- Refresh policy: run every hour, covering the last 2 hours
SELECT add_continuous_aggregate_policy('hourly_route_summary',
    start_offset => INTERVAL '2 hours',
    end_offset   => INTERVAL '5 minutes',
    schedule_interval => INTERVAL '1 hour',
    if_not_exists => TRUE
);

-- Geofence zones table (regular table, not hypertable)
CREATE TABLE IF NOT EXISTS geofences (
    id          TEXT    PRIMARY KEY,
    tenant_id   TEXT    NOT NULL,
    name        TEXT    NOT NULL,
    type        TEXT    NOT NULL,
    polygon     JSONB   NOT NULL, -- [{lat, lon}, ...]
    active      BOOLEAN NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_location_pings_driver ON location_pings(driver_id, ts DESC);
CREATE INDEX IF NOT EXISTS idx_geofences_tenant      ON geofences(tenant_id, active);

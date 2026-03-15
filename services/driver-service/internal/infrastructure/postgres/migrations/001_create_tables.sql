-- Driver Service schema

CREATE TABLE IF NOT EXISTS drivers (
    id             TEXT        PRIMARY KEY,
    tenant_id      TEXT        NOT NULL,
    user_id        TEXT,
    name           TEXT        NOT NULL,
    phone          TEXT,
    license_number TEXT        NOT NULL,
    status         TEXT        NOT NULL DEFAULT 'OFFLINE',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS vehicles (
    id          TEXT        PRIMARY KEY,
    driver_id   TEXT        NOT NULL REFERENCES drivers(id) ON DELETE CASCADE,
    plate_number TEXT       NOT NULL,
    type        TEXT        NOT NULL,
    capacity_kg DOUBLE PRECISION NOT NULL DEFAULT 0,
    make        TEXT,
    model       TEXT,
    year        INT,
    active      BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS documents (
    id                  TEXT        PRIMARY KEY,
    driver_id           TEXT        NOT NULL REFERENCES drivers(id) ON DELETE CASCADE,
    type                TEXT        NOT NULL,
    reference_number    TEXT,
    expiry_date         TIMESTAMPTZ NOT NULL,
    verification_status TEXT        NOT NULL DEFAULT 'PENDING',
    file_url            TEXT,
    uploaded_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_drivers_tenant_status ON drivers(tenant_id, status);
CREATE INDEX IF NOT EXISTS idx_documents_expiry      ON documents(expiry_date) WHERE verification_status='VERIFIED';
CREATE INDEX IF NOT EXISTS idx_vehicles_driver_active ON vehicles(driver_id, active);

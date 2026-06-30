CREATE TABLE generations (
    id               TEXT PRIMARY KEY,
    status           TEXT NOT NULL CHECK (status IN ('queued','running','completed','failed')),
    created_at       TIMESTAMPTZ NOT NULL,
    started_at       TIMESTAMPTZ,
    completed_at     TIMESTAMPTZ,
    request_json     JSONB NOT NULL,
    attempts         INTEGER NOT NULL DEFAULT 0,
    failure_reason   TEXT,
    error_detail     TEXT,
    trace_json       JSONB,
    lease_expires_at TIMESTAMPTZ,
    lease_owner      TEXT
);
CREATE INDEX idx_generations_status_created ON generations (status, created_at);
CREATE INDEX idx_generations_lease ON generations (status, lease_expires_at);

CREATE TABLE saved_trajectories (
    id                TEXT PRIMARY KEY REFERENCES generations(id),
    created_at        TIMESTAMPTZ NOT NULL,
    class             TEXT NOT NULL,
    server            TEXT NOT NULL,
    playstyle         TEXT NOT NULL,
    mode              TEXT NOT NULL,
    description       TEXT,
    primary_json      JSONB NOT NULL,
    alternatives_json JSONB,
    final_text        TEXT,
    gate_summary      JSONB NOT NULL,
    calc_version      TEXT,
    catalog_version   INTEGER
);
CREATE INDEX idx_saved_trajectories_lookup ON saved_trajectories (server, class, created_at DESC);

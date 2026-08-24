-- Dual-write failure / outbox table. It lives in the v1 database and is the shared, durable
-- record of every v2 mirror write that failed (its columns double as the reconcile work list:
-- op / table_name / v1_key, plus reconciled_at once a replay has healed the row).
--
-- Provisioned OUT-OF-BAND (DBA / schema-migration step) BEFORE dual-write is enabled — the
-- runtime app role is assumed DML-only and MUST NOT run DDL. The v1.1 app only INSERTs/SELECTs
-- into this table; it probes for existence at startup but never creates it.
--
-- Portable across PostgreSQL (v1 prod) and SQLite (unit tests): every column is TEXT.
CREATE TABLE IF NOT EXISTS v2_dual_write_failures (
    id            TEXT PRIMARY KEY,
    code          TEXT NOT NULL,
    entity        TEXT NOT NULL,
    op            TEXT NOT NULL,
    table_name    TEXT NOT NULL,
    v1_key        TEXT NOT NULL,
    org_uuid      TEXT,
    error         TEXT,
    replica       TEXT,
    occurred_at   TEXT NOT NULL,
    reconciled_at TEXT
);

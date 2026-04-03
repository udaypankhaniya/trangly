-- Trangly initial schema
-- All tables created in one migration; subsequent migrations add new files.

CREATE TABLE IF NOT EXISTS users (
    id           TEXT PRIMARY KEY,
    username     TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,              -- bcrypt, cost >= 12
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- JWT signing secret (one row, seeded at first run).
CREATE TABLE IF NOT EXISTS jwt_secrets (
    id         INTEGER PRIMARY KEY CHECK (id = 1), -- enforce single row
    secret_hex TEXT NOT NULL,                       -- 32 random bytes, hex-encoded
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- GitHub App credentials (one row, written after manifest callback).
CREATE TABLE IF NOT EXISTS github_app (
    id                INTEGER PRIMARY KEY CHECK (id = 1),
    app_id            INTEGER NOT NULL,
    app_slug          TEXT NOT NULL,
    webhook_secret    BLOB NOT NULL,  -- AES-256-GCM encrypted
    pem               BLOB NOT NULL,  -- AES-256-GCM encrypted
    installation_id   INTEGER,        -- set after user installs the app
    created_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- One-time state tokens for GitHub OAuth callback.
CREATE TABLE IF NOT EXISTS state_tokens (
    token      TEXT PRIMARY KEY,    -- 32 random bytes, hex-encoded
    expires_at DATETIME NOT NULL,
    used       INTEGER NOT NULL DEFAULT 0  -- 1 once consumed
);

CREATE TABLE IF NOT EXISTS projects (
    id               TEXT PRIMARY KEY,
    name             TEXT NOT NULL,
    repo_full_name   TEXT NOT NULL UNIQUE,
    default_branch   TEXT NOT NULL DEFAULT 'main',
    compose_path     TEXT NOT NULL DEFAULT 'docker-compose.yml',
    mem_limit_mb     INTEGER NOT NULL DEFAULT 0,
    webhook_secret   BLOB,         -- AES-256-GCM encrypted; may be null until GitHub connected
    env_vars         BLOB,         -- AES-256-GCM encrypted KEY=VALUE pairs
    installation_id  INTEGER NOT NULL DEFAULT 0,
    created_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS deploy_jobs (
    id               TEXT PRIMARY KEY,
    project_id       TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    commit_sha       TEXT NOT NULL,
    branch           TEXT NOT NULL,
    status           TEXT NOT NULL DEFAULT 'pending',
    worker_id        INTEGER NOT NULL DEFAULT 0,
    ram_estimate_mb  INTEGER NOT NULL DEFAULT 0,
    peak_rss_mb      INTEGER NOT NULL DEFAULT 0,
    estimate_source  TEXT NOT NULL DEFAULT 'bootstrap',
    hold_reason      TEXT,
    log_path         TEXT,
    queued_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    held_at          DATETIME,
    started_at       DATETIME,
    finished_at      DATETIME,
    error            TEXT
);

CREATE INDEX IF NOT EXISTS idx_deploy_jobs_project_id ON deploy_jobs(project_id);
CREATE INDEX IF NOT EXISTS idx_deploy_jobs_status     ON deploy_jobs(status);
CREATE INDEX IF NOT EXISTS idx_state_tokens_expires   ON state_tokens(expires_at);

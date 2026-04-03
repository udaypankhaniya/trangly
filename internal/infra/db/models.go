package db

import "time"

// These types mirror the SQLite table rows exactly.
// They are internal to the db package; the app layer maps them to domain types.

// UserRow mirrors the users table.
type UserRow struct {
	ID           string
	Username     string
	Email        string
	PasswordHash string
	CreatedAt    time.Time
}

// JWTSecretRow mirrors the jwt_secrets table (single row).
type JWTSecretRow struct {
	SecretHex string
	CreatedAt time.Time
}

// GitHubAppRow mirrors the github_app table (single row).
type GitHubAppRow struct {
	AppID          int64
	AppSlug        string
	WebhookSecret  []byte // AES-256-GCM encrypted
	PEM            []byte // AES-256-GCM encrypted
	InstallationID int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// StateTokenRow mirrors the state_tokens table.
type StateTokenRow struct {
	Token     string
	ExpiresAt time.Time
	Used      bool
}

// ProjectRow mirrors the projects table.
type ProjectRow struct {
	ID             string
	Name           string
	RepoFullName   string
	DefaultBranch  string
	ComposePath    string
	MemLimitMB     int64
	DeployMode     string // "rebuild" or "hot_swap"
	WebhookSecret  []byte // AES-256-GCM encrypted; may be nil
	EnvVars        []byte // AES-256-GCM encrypted; may be nil
	InstallationID int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// DeploymentQueryParams holds the parameters for the advanced deployment list query.
type DeploymentQueryParams struct {
	Limit     int
	Offset    int
	SortBy    string // validated column name: queued_at, status, branch, commit_sha, finished_at
	SortDir   string // "ASC" or "DESC"
	Search    string // LIKE match on commit_sha, branch, error, projects.name, projects.repo_full_name
	ProjectID string // optional filter
	Status    string // optional filter
}

// DeployJobWithProjectRow extends DeployJobRow with the project name resolved via JOIN.
type DeployJobWithProjectRow struct {
	DeployJobRow
	ProjectName string
}

// DeployJobRow mirrors the deploy_jobs table.
type DeployJobRow struct {
	ID             string
	ProjectID      string
	CommitSHA      string
	Branch         string
	Status         string
	WorkerID       int
	RAMEstimateMB  int64
	PeakRSSMB      int64
	EstimateSource string
	HoldReason     *string
	LogPath        *string
	QueuedAt       time.Time
	HeldAt         *time.Time
	StartedAt      *time.Time
	FinishedAt     *time.Time
	Error          *string
}

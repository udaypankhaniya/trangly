package domain

import "time"

// Deployment is a summary view of a deploy job used by the history API.
// It is intentionally lighter than DeployJob — no internal fields.
type Deployment struct {
	ID            string     `json:"id"`
	ProjectID     string     `json:"project_id"`
	ProjectName   string     `json:"project_name"`
	CommitSHA     string     `json:"commit_sha"`
	Branch        string     `json:"branch"`
	Status        string     `json:"status"`
	RAMEstimateMB int64      `json:"ram_estimate_mb"`
	QueuedAt      time.Time  `json:"queued_at"`
	StartedAt     *time.Time `json:"started_at,omitempty"`
	FinishedAt    *time.Time `json:"finished_at,omitempty"`
	Error         string     `json:"error,omitempty"`
}

// DeploymentListResult is the response envelope for paginated deployment queries
// with server-side sorting, filtering, and search.
type DeploymentListResult struct {
	Items  []*DeployJob `json:"deployments"`
	Total  int          `json:"total"`
	Limit  int          `json:"limit"`
	Offset int          `json:"offset"`
}

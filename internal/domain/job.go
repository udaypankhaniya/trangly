package domain

import "time"

// DeployJob represents one deployment execution unit.
// It maps 1:1 to a row in the deploy_jobs SQLite table.
type DeployJob struct {
	ID             string     `json:"id"`
	ProjectID      string     `json:"project_id"`
	CommitSHA      string     `json:"commit_sha"`
	Branch         string     `json:"branch"`
	Status         string     `json:"status"`          // one of the Status* constants in enums.go
	WorkerID       int        `json:"worker_id"`       // internal worker slot; 0 = unassigned
	RAMEstimateMB  int64      `json:"ram_estimate_mb"` // estimated RAM required (MB)
	PeakRSSMB      int64      `json:"peak_rss_mb"`     // observed peak RSS during this build (MB)
	EstimateSource string     `json:"estimate_source"` // EstimateSource* constant
	HoldReason     string     `json:"hold_reason"`     // HoldReason* constant or empty
	LogPath        string     `json:"log_path"`        // absolute path to job log file
	QueuedAt       time.Time  `json:"queued_at"`
	HeldAt         *time.Time `json:"held_at,omitempty"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
	Error          string     `json:"error,omitempty"` // human-readable failure message
	ProjectName    string     `json:"project_name,omitempty"`
}

// ShortSHA returns the first 7 characters of the commit SHA for use in image tags and paths.
func (j *DeployJob) ShortSHA() string {
	if len(j.CommitSHA) >= 7 {
		return j.CommitSHA[:7]
	}
	return j.CommitSHA
}

// IsTerminal returns true if the job has reached a final state (success or failed).
func (j *DeployJob) IsTerminal() bool {
	return j.Status == StatusSuccess || j.Status == StatusFailed
}

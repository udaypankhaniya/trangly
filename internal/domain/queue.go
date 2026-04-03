package domain

// QueueEntry is a lightweight view of a job as it appears in the live queue.
type QueueEntry struct {
	JobID         string `json:"job_id"`
	ProjectID     string `json:"project_id"`
	ProjectName   string `json:"project_name"`
	CommitSHA     string `json:"commit_sha"`
	Branch        string `json:"branch"`
	Status        string `json:"status"`
	RAMEstimateMB int64  `json:"ram_estimate_mb"`
	HoldReason    string `json:"hold_reason,omitempty"`
	Position      int    `json:"position"` // 1-based position within the project queue
}

// QueueStats is a snapshot of queue health returned by GET /api/queue.
type QueueStats struct {
	TotalJobs    int `json:"total_jobs"`
	PendingJobs  int `json:"pending_jobs"`
	HeldJobs     int `json:"held_jobs"`
	RunningJobs  int `json:"running_jobs"`
	AvailableRAM int `json:"available_ram_mb"` // current scheduler available RAM budget (MB)
}

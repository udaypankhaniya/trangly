// Package queue provides per-project FIFO job queues backed by SQLite.
// All state transitions go through the centralized state machine in state_machine.go.
package queue

import (
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/udaypankhaniya/trangly/internal/api/sse"
	"github.com/udaypankhaniya/trangly/internal/domain"
	"github.com/udaypankhaniya/trangly/internal/infra/db"
)

// Manager maintains per-project FIFO queues backed by SQLite.
// After a crash, pending and held jobs are recovered from SQLite on startup.
type Manager struct {
	db          *db.DB
	broadcaster *sse.Broadcaster
	mu          sync.RWMutex
	log         *slog.Logger
}

// NewManager creates a Manager and recovers any interrupted jobs from the previous run.
func NewManager(database *db.DB, broadcaster *sse.Broadcaster) (*Manager, error) {
	m := &Manager{
		db:          database,
		broadcaster: broadcaster,
		log:         slog.Default().With("component", "queue"),
	}
	if err := m.recoverInterruptedJobs(); err != nil {
		return nil, fmt.Errorf("queue: recover interrupted jobs: %w", err)
	}
	return m, nil
}

// Enqueue adds a new job to the project's queue. The job must already be
// persisted to the database (by DeployService.Trigger) before calling this.
func (m *Manager) Enqueue(job *domain.DeployJob) error {
	m.log.Info("job enqueued", "job_id", job.ID, "project_id", job.ProjectID)
	return nil // persistence is handled by DeployService; this is a hook for in-memory state if added later
}

// NextPending returns the next pending job for a project, or nil if none.
// The caller (scheduler) is responsible for transitioning it to running/held.
func (m *Manager) NextPending(projectID string) (*domain.DeployJob, error) {
	rows, err := m.db.ListJobsByProject(projectID, 50)
	if err != nil {
		return nil, fmt.Errorf("queue: list jobs: %w", err)
	}
	for _, r := range rows {
		if r.Status == domain.StatusPending || r.Status == domain.StatusHeld {
			return rowToJob(r), nil
		}
	}
	return nil, nil
}

// NextPendingAny returns the next pending job across all projects (oldest first).
func (m *Manager) NextPendingAny() (*domain.DeployJob, error) {
	rows, err := m.db.ListActiveJobs()
	if err != nil {
		return nil, fmt.Errorf("queue: list active jobs: %w", err)
	}
	for _, r := range rows {
		if r.Status == domain.StatusPending {
			return rowToJob(r), nil
		}
	}
	return nil, nil
}

// ListActive returns all non-terminal jobs across all projects.
func (m *Manager) ListActive() ([]*domain.DeployJob, error) {
	rows, err := m.db.ListActiveJobs()
	if err != nil {
		return nil, fmt.Errorf("queue: list active: %w", err)
	}
	jobs := make([]*domain.DeployJob, 0, len(rows))
	for _, r := range rows {
		jobs = append(jobs, rowToJob(r))
	}
	return jobs, nil
}

// ListHeld returns all held jobs across all projects.
func (m *Manager) ListHeld() ([]*domain.DeployJob, error) {
	active, err := m.ListActive()
	if err != nil {
		return nil, err
	}
	var held []*domain.DeployJob
	for _, j := range active {
		if j.Status == domain.StatusHeld {
			held = append(held, j)
		}
	}
	return held, nil
}

// UpdateStatus transitions a job from its current status to the new status.
// The transition is validated by the state machine before writing.
func (m *Manager) UpdateStatus(jobID, newStatus string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	row, err := m.db.GetJob(jobID)
	if errors.Is(err, db.ErrNotFound) {
		return fmt.Errorf("queue: job %s not found", jobID)
	}
	if err != nil {
		return fmt.Errorf("queue: get job: %w", err)
	}
	if err := ValidateTransition(row.Status, newStatus); err != nil {
		return err
	}
	if err := m.db.UpdateJobStatus(jobID, newStatus); err != nil {
		return err
	}
	m.publishStatusChange(jobID, newStatus)
	return nil
}

// Hold transitions a pending job to the held state with a reason.
func (m *Manager) Hold(jobID, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	row, err := m.db.GetJob(jobID)
	if errors.Is(err, db.ErrNotFound) {
		return fmt.Errorf("queue: job %s not found", jobID)
	}
	if err != nil {
		return fmt.Errorf("queue: get job: %w", err)
	}
	if err := ValidateTransition(row.Status, domain.StatusHeld); err != nil {
		return err
	}
	if err := m.db.UpdateJobHeld(jobID, reason, time.Now().UTC()); err != nil {
		return err
	}
	m.publishStatusChange(jobID, domain.StatusHeld)
	return nil
}

// MarkStarted transitions a job to running and records the worker and start time.
func (m *Manager) MarkStarted(jobID string, workerID int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	row, err := m.db.GetJob(jobID)
	if errors.Is(err, db.ErrNotFound) {
		return fmt.Errorf("queue: job %s not found", jobID)
	}
	if err != nil {
		return fmt.Errorf("queue: get job: %w", err)
	}
	if err := ValidateTransition(row.Status, domain.StatusRunning); err != nil {
		return err
	}
	if err := m.db.UpdateJobStarted(jobID, workerID, time.Now().UTC()); err != nil {
		return err
	}
	m.publishStatusChange(jobID, domain.StatusRunning)
	return nil
}

// MarkFinished transitions a job to a terminal state (success or failed).
func (m *Manager) MarkFinished(jobID, status, errMsg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	row, err := m.db.GetJob(jobID)
	if errors.Is(err, db.ErrNotFound) {
		return fmt.Errorf("queue: job %s not found", jobID)
	}
	if err != nil {
		return fmt.Errorf("queue: get job: %w", err)
	}
	if err := ValidateTransition(row.Status, status); err != nil {
		return err
	}
	if err := m.db.UpdateJobFinished(jobID, status, errMsg, time.Now().UTC()); err != nil {
		return err
	}
	m.publishStatusChange(jobID, status)
	return nil
}

// Cancel attempts to transition a job to failed state, regardless of current status.
// Used by DELETE /api/queue/:job_id. Only pending or held jobs can be cancelled.
func (m *Manager) Cancel(jobID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	row, err := m.db.GetJob(jobID)
	if errors.Is(err, db.ErrNotFound) {
		return fmt.Errorf("queue: job %s not found", jobID)
	}
	if err != nil {
		return fmt.Errorf("queue: get job: %w", err)
	}
	if row.Status != domain.StatusPending && row.Status != domain.StatusHeld {
		return fmt.Errorf("queue: can only cancel pending or held jobs, got %q", row.Status)
	}
	if err := m.db.UpdateJobFinished(jobID, domain.StatusFailed, "cancelled by user", time.Now().UTC()); err != nil {
		return err
	}
	m.publishStatusChange(jobID, domain.StatusFailed)
	return nil
}

// recoverInterruptedJobs resets any jobs that were left in a non-terminal, non-pending
// state from a previous crashed run. They go back to pending so the scheduler can retry.
func (m *Manager) recoverInterruptedJobs() error {
	rows, err := m.db.ListActiveJobs()
	if err != nil {
		return err
	}
	recovered := 0
	for _, r := range rows {
		switch r.Status {
		case domain.StatusRunning, domain.StatusBuilding, domain.StatusHealthCheck, domain.StatusSwapping:
			// These were interrupted mid-execution — reset to failed.
			if err := m.db.UpdateJobFinished(r.ID, domain.StatusFailed,
				"interrupted by Trangly restart", time.Now().UTC()); err != nil {
				m.log.Warn("could not reset interrupted job", "job_id", r.ID, "err", err)
			}
			recovered++
		}
	}
	if recovered > 0 {
		m.log.Info("recovered interrupted jobs from previous run", "count", recovered)
	}
	return nil
}

func rowToJob(r *db.DeployJobRow) *domain.DeployJob {
	j := &domain.DeployJob{
		ID:             r.ID,
		ProjectID:      r.ProjectID,
		CommitSHA:      r.CommitSHA,
		Branch:         r.Branch,
		Status:         r.Status,
		WorkerID:       r.WorkerID,
		RAMEstimateMB:  r.RAMEstimateMB,
		PeakRSSMB:      r.PeakRSSMB,
		EstimateSource: r.EstimateSource,
		QueuedAt:       r.QueuedAt,
		HeldAt:         r.HeldAt,
		StartedAt:      r.StartedAt,
		FinishedAt:     r.FinishedAt,
	}
	if r.HoldReason != nil {
		j.HoldReason = *r.HoldReason
	}
	if r.LogPath != nil {
		j.LogPath = *r.LogPath
	}
	if r.Error != nil {
		j.Error = *r.Error
	}
	return j
}

// JobStatusEvent is the SSE payload published on every job status change.
type JobStatusEvent struct {
	Type      string `json:"type"`
	JobID     string `json:"job_id"`
	ProjectID string `json:"project_id"`
	Status    string `json:"status"`
	CommitSHA string `json:"commit_sha"`
	Branch    string `json:"branch"`
	Error     string `json:"error,omitempty"`
}

// publishStatusChange sends a dashboard SSE event for a job status change.
func (m *Manager) publishStatusChange(jobID, newStatus string) {
	if m.broadcaster == nil {
		return
	}
	row, err := m.db.GetJob(jobID)
	if err != nil {
		return
	}
	m.broadcaster.PublishJSON(sse.TopicDashboard, JobStatusEvent{
		Type:      "job_status",
		JobID:     row.ID,
		ProjectID: row.ProjectID,
		Status:    newStatus,
		CommitSHA: row.CommitSHA,
		Branch:    row.Branch,
		Error:     deref(row.Error),
	})
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/udaypankhaniya/trangly/internal/domain"
	"github.com/udaypankhaniya/trangly/internal/infra/db"
)

const (
	holdWarnDuration     = 10 * time.Minute
	holdAutoFailDuration = 30 * time.Minute
	stuckJobDuration     = 10 * time.Minute
)

// Monitor watches held jobs and enforces escalation and auto-fail policies.
type Monitor struct {
	db  *db.DB
	log *slog.Logger
}

// NewMonitor creates a Monitor.
func NewMonitor(database *db.DB) *Monitor {
	return &Monitor{
		db:  database,
		log: slog.Default().With("component", "monitor"),
	}
}

// CheckHeld inspects all held jobs and:
//   - Emits a WARNING log if a job has been held for more than 10 minutes.
//   - Marks a job FAILED if it has been held for more than 30 minutes.
//   - Immediately marks a job FAILED if its estimate exceeds available RAM
//     (it can never run on this machine).
func (m *Monitor) CheckHeld(ctx context.Context, heldJobs []*domain.DeployJob, availableRAMMB int64) {
	now := time.Now().UTC()
	for _, job := range heldJobs {
		if job.HeldAt == nil {
			continue
		}

		// Impossible jobs: estimate exceeds the entire available RAM budget.
		if job.RAMEstimateMB > availableRAMMB && job.RAMEstimateMB > 0 {
			m.log.WarnContext(ctx, "job estimate exceeds available RAM — failing immediately",
				"job_id", job.ID,
				"estimate_mb", job.RAMEstimateMB,
				"available_mb", availableRAMMB,
			)
			if err := m.db.UpdateJobFinished(job.ID, domain.StatusFailed,
				"job RAM estimate exceeds machine capacity — cannot run", now); err != nil {
				m.log.Error("could not auto-fail impossible job", "job_id", job.ID, "err", err)
			}
			continue
		}

		held := now.Sub(*job.HeldAt)
		switch {
		case held > holdAutoFailDuration:
			m.log.WarnContext(ctx, "auto-failing job held too long",
				"job_id", job.ID, "held_duration", held.Round(time.Second))
			if err := m.db.UpdateJobFinished(job.ID, domain.StatusFailed,
				"held for more than 30 minutes — insufficient memory to run", now); err != nil {
				m.log.Error("could not auto-fail held job", "job_id", job.ID, "err", err)
			}
		case held > holdWarnDuration:
			m.log.WarnContext(ctx, "job has been held for over 10 minutes",
				"job_id", job.ID, "held_duration", held.Round(time.Second),
				"hold_reason", job.HoldReason)
		}
	}
}

// IsImpossible returns true if the job's RAM estimate exceeds the machine's available budget.
// These jobs should be failed immediately rather than held.
func IsImpossible(job *domain.DeployJob, availableRAMMB int64) bool {
	return job.RAMEstimateMB > 0 && job.RAMEstimateMB > availableRAMMB
}

// CheckStuck inspects all running (non-held, non-terminal) jobs and auto-fails
// any that have been in running/building/health_check/swapping for longer than
// stuckJobDuration. This is a safety net in case a pipeline stage hangs despite
// context timeouts and process kill.
func (m *Monitor) CheckStuck(ctx context.Context, activeJobs []*domain.DeployJob) {
	now := time.Now().UTC()
	for _, job := range activeJobs {
		if job.StartedAt == nil {
			continue
		}
		switch job.Status {
		case domain.StatusRunning, domain.StatusBuilding, domain.StatusHealthCheck, domain.StatusSwapping, domain.StatusHotSwapping:
		default:
			continue
		}
		elapsed := now.Sub(*job.StartedAt)
		if elapsed > stuckJobDuration {
			m.log.WarnContext(ctx, "auto-failing stuck job",
				"job_id", job.ID,
				"status", job.Status,
				"stuck_duration", elapsed.Round(time.Second),
			)
			if err := m.db.UpdateJobFinished(job.ID, domain.StatusFailed,
				fmt.Sprintf("stuck in %s for %s — auto-failed", job.Status, elapsed.Round(time.Second)),
				now); err != nil {
				m.log.Error("could not auto-fail stuck job", "job_id", job.ID, "err", err)
			}
		}
	}
}

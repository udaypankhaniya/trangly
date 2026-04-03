package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/udaypankhaniya/trangly/internal/domain"
	"github.com/udaypankhaniya/trangly/internal/infra/db"
	"github.com/udaypankhaniya/trangly/internal/infra/system"
	"github.com/udaypankhaniya/trangly/internal/queue"
)

const (
	schedulerInterval = 5 * time.Second
	tranglyResvMB     = 50  // MB reserved for Trangly's own process
	safetyBufferPct   = 0.2 // 20% of total RAM kept as safety buffer
)

// JobRunner is the interface the scheduler uses to start a job.
// Implemented by engine.Runner — kept as an interface so the scheduler
// does not import the engine package directly.
type JobRunner interface {
	Run(ctx context.Context, job *domain.DeployJob) error
}

// Scheduler runs a 5-second loop that decides which jobs can execute given RAM availability.
// It NEVER executes jobs itself — it delegates to the engine via JobRunner.
type Scheduler struct {
	db           *db.DB
	queue        *queue.Manager
	monitor      *Monitor
	runner       JobRunner
	availableRAM int64 // computed once at startup (MB)
	log          *slog.Logger
}

// NewScheduler creates a Scheduler and computes the RAM budget at startup.
func NewScheduler(database *db.DB, qm *queue.Manager, mon *Monitor, runner JobRunner) (*Scheduler, error) {
	mem, err := system.ReadMemInfo()
	if err != nil {
		return nil, err
	}

	safetyBuffer := int64(float64(mem.TotalMB) * safetyBufferPct)
	availableRAM := mem.TotalMB - safetyBuffer - tranglyResvMB
	if availableRAM < 1 {
		availableRAM = 1
	}

	s := &Scheduler{
		db:           database,
		queue:        qm,
		monitor:      mon,
		runner:       runner,
		availableRAM: availableRAM,
		log: slog.Default().With(
			"component", "scheduler",
			"available_ram_mb", availableRAM,
		),
	}
	s.log.Info("scheduler initialized",
		"total_ram_mb", mem.TotalMB,
		"safety_buffer_mb", safetyBuffer,
		"trangly_rsv_mb", tranglyResvMB,
		"available_ram_mb", availableRAM,
	)
	return s, nil
}

// Run starts the scheduler loop. It blocks until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(schedulerInterval)
	defer ticker.Stop()
	s.log.Info("scheduler started")

	for {
		select {
		case <-ctx.Done():
			s.log.Info("scheduler stopped")
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

// tick is one iteration of the scheduler loop.
func (s *Scheduler) tick(ctx context.Context) {
	// Load all active jobs to compute running RAM usage and check held jobs.
	activeJobs, err := s.queue.ListActive()
	if err != nil {
		s.log.ErrorContext(ctx, "failed to list active jobs", "err", err)
		return
	}

	// Compute current RAM in use by running/building/health-check/swapping jobs.
	var runningRAM int64
	for _, j := range activeJobs {
		switch j.Status {
		case domain.StatusRunning, domain.StatusBuilding, domain.StatusHealthCheck, domain.StatusSwapping, domain.StatusHotSwapping:
			runningRAM += j.RAMEstimateMB
		}
	}

	// Read current free RAM from /proc/meminfo.
	mem, err := system.ReadMemInfo()
	if err != nil {
		s.log.ErrorContext(ctx, "failed to read meminfo", "err", err)
		return
	}

	effective := min64(mem.AvailableMB, s.availableRAM-runningRAM)
	s.log.DebugContext(ctx, "scheduler tick",
		"free_ram_mb", mem.AvailableMB,
		"running_ram_mb", runningRAM,
		"effective_mb", effective,
	)

	// Check monitor escalation for held jobs.
	var heldJobs []*domain.DeployJob
	for _, j := range activeJobs {
		if j.Status == domain.StatusHeld {
			heldJobs = append(heldJobs, j)
		}
	}
	s.monitor.CheckHeld(ctx, heldJobs, s.availableRAM)

	// Check for stuck jobs (running/building/swapping too long).
	s.monitor.CheckStuck(ctx, activeJobs)

	// Pick next pending job for execution.
	next, err := s.queue.NextPendingAny()
	if err != nil {
		s.log.ErrorContext(ctx, "failed to get next job", "err", err)
		return
	}
	if next == nil {
		return
	}

	// Impossible job: estimate exceeds the total budget — fail immediately.
	if IsImpossible(next, s.availableRAM) {
		s.log.WarnContext(ctx, "job impossible, failing",
			"job_id", next.ID, "estimate_mb", next.RAMEstimateMB, "available_mb", s.availableRAM)
		_ = s.queue.MarkFinished(next.ID, domain.StatusFailed,
			"RAM estimate exceeds machine capacity")
		return
	}

	// Not enough RAM right now — hold the job.
	if next.RAMEstimateMB > effective {
		s.log.InfoContext(ctx, "insufficient memory, holding job",
			"job_id", next.ID, "estimate_mb", next.RAMEstimateMB, "effective_mb", effective)
		_ = s.queue.Hold(next.ID, domain.HoldReasonMemory)
		return
	}

	// Enough RAM — delegate to engine.
	s.log.InfoContext(ctx, "delegating job to engine",
		"job_id", next.ID, "estimate_mb", next.RAMEstimateMB)
	go func(job *domain.DeployJob) {
		if err := s.runner.Run(ctx, job); err != nil {
			s.log.ErrorContext(ctx, "job execution error", "job_id", job.ID, "err", err)
		}
	}(next)
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

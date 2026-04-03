package app

import (
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/udaypankhaniya/trangly/internal/domain"
	"github.com/udaypankhaniya/trangly/internal/infra/db"
)

// DeployService handles triggering and querying deployments.
// It may only enqueue jobs; execution is handled exclusively by the scheduler → engine path.
type DeployService struct {
	db      *db.DB
	logsDir string
	log     *slog.Logger
}

// NewDeployService creates a DeployService.
func NewDeployService(database *db.DB, logsDir string) *DeployService {
	return &DeployService{
		db:      database,
		logsDir: logsDir,
		log:     slog.Default().With("service", "deploy"),
	}
}

// Trigger creates a new pending DeployJob for the given project.
// It validates the project exists and returns the enqueued job.
func (s *DeployService) Trigger(projectID, commitSHA, branch string) (*domain.DeployJob, error) {
	// Validate project exists.
	_, err := s.db.GetProject(projectID)
	if errors.Is(err, db.ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("deploy: get project: %w", err)
	}

	id, err := newID()
	if err != nil {
		return nil, err
	}

	logPath := filepath.Join(s.logsDir, id+".log")
	now := time.Now().UTC()

	row := &db.DeployJobRow{
		ID:             id,
		ProjectID:      projectID,
		CommitSHA:      commitSHA,
		Branch:         branch,
		Status:         domain.StatusPending,
		EstimateSource: domain.EstimateSourceBootstrap,
		LogPath:        &logPath,
		QueuedAt:       now,
	}
	if err := s.db.InsertJob(row); err != nil {
		return nil, fmt.Errorf("deploy: insert job: %w", err)
	}
	if err := s.db.SetJobLogPath(id, logPath); err != nil {
		return nil, fmt.Errorf("deploy: set log path: %w", err)
	}

	s.log.Info("deploy job enqueued", "job_id", id, "project_id", projectID, "sha", commitSHA[:min(7, len(commitSHA))])

	return s.rowToJob(row), nil
}

// GetJob returns a single deploy job by ID.
func (s *DeployService) GetJob(id string) (*domain.DeployJob, error) {
	row, err := s.db.GetJob(id)
	if errors.Is(err, db.ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("deploy: get job: %w", err)
	}
	return s.rowToJob(row), nil
}

// GetHistory returns the last 50 deploy jobs for a project, newest first.
func (s *DeployService) GetHistory(projectID string) ([]*domain.DeployJob, error) {
	rows, err := s.db.ListJobsByProject(projectID, 50)
	if err != nil {
		return nil, fmt.Errorf("deploy: list jobs: %w", err)
	}
	jobs := make([]*domain.DeployJob, 0, len(rows))
	for _, r := range rows {
		jobs = append(jobs, s.rowToJob(r))
	}
	return jobs, nil
}

// GetHistoryPaged returns paginated deploy jobs for a project.
func (s *DeployService) GetHistoryPaged(projectID string, limit, offset int) ([]*domain.DeployJob, bool, error) {
	rows, hasMore, err := s.db.ListJobsByProjectPaged(projectID, limit, offset)
	if err != nil {
		return nil, false, fmt.Errorf("deploy: list jobs paged: %w", err)
	}
	jobs := make([]*domain.DeployJob, 0, len(rows))
	for _, r := range rows {
		jobs = append(jobs, s.rowToJob(r))
	}
	return jobs, hasMore, nil
}

// ListAllPaged returns paginated deploy jobs across all projects with optional status filter.
func (s *DeployService) ListAllPaged(limit, offset int, status string) ([]*domain.DeployJob, bool, error) {
	rows, hasMore, err := s.db.ListAllJobsPaged(limit, offset, status)
	if err != nil {
		return nil, false, fmt.Errorf("deploy: list all paged: %w", err)
	}
	jobs := make([]*domain.DeployJob, 0, len(rows))
	for _, r := range rows {
		jobs = append(jobs, s.rowToJob(r))
	}
	return jobs, hasMore, nil
}

// ListDeploymentsAdvanced returns deploy jobs with server-side sorting, searching,
// filtering, and pagination. It uses the advanced JOIN query that resolves project names.
func (s *DeployService) ListDeploymentsAdvanced(params db.DeploymentQueryParams) (*domain.DeploymentListResult, error) {
	rows, total, err := s.db.ListDeploymentsAdvanced(params)
	if err != nil {
		return nil, fmt.Errorf("deploy: list advanced: %w", err)
	}
	jobs := make([]*domain.DeployJob, 0, len(rows))
	for _, r := range rows {
		j := s.rowToJob(&r.DeployJobRow)
		j.ProjectName = r.ProjectName
		jobs = append(jobs, j)
	}
	return &domain.DeploymentListResult{
		Items:  jobs,
		Total:  total,
		Limit:  params.Limit,
		Offset: params.Offset,
	}, nil
}

// GetLastDeploy returns the most recent terminal deploy for a project, or ErrNotFound.
func (s *DeployService) GetLastDeploy(projectID string) (*domain.DeployJob, error) {
	row, err := s.db.GetLastDeployForProject(projectID)
	if err != nil {
		return nil, err
	}
	return s.rowToJob(row), nil
}

// ListAllActive returns all jobs in non-terminal states (for queue API).
func (s *DeployService) ListAllActive() ([]*domain.DeployJob, error) {
	rows, err := s.db.ListActiveJobs()
	if err != nil {
		return nil, fmt.Errorf("deploy: list active: %w", err)
	}
	jobs := make([]*domain.DeployJob, 0, len(rows))
	for _, r := range rows {
		jobs = append(jobs, s.rowToJob(r))
	}
	return jobs, nil
}

func (s *DeployService) rowToJob(r *db.DeployJobRow) *domain.DeployJob {
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

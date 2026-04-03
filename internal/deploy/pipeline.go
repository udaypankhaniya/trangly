// Package deploy implements the five-stage deployment pipeline.
// Execution order: fetch → build → healthcheck → swap → cleanup.
// The pipeline is orchestrated by internal/engine/runner.go — this package
// must never be called directly from API handlers or the scheduler.
package deploy

import (
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/udaypankhaniya/trangly/internal/domain"
	"github.com/udaypankhaniya/trangly/internal/infra/docker"
)

// Pipeline holds the shared dependencies used across all pipeline stages.
type Pipeline struct {
	Docker        *docker.Client
	WorkspacesDir string
	Log           *slog.Logger
}

// NewPipeline creates a Pipeline.
func NewPipeline(dc *docker.Client, workspacesDir string) *Pipeline {
	return &Pipeline{
		Docker:        dc,
		WorkspacesDir: workspacesDir,
		Log:           slog.Default().With("component", "pipeline"),
	}
}

// StageError is a typed error returned by a pipeline stage.
// It carries the stage name so the caller can log and report accurately.
type StageError struct {
	Stage string
	Err   error
}

func (e *StageError) Error() string {
	return fmt.Sprintf("pipeline[%s]: %v", e.Stage, e.Err)
}

func (e *StageError) Unwrap() error { return e.Err }

// stageErr wraps an error as a StageError.
func stageErr(stage string, err error) error {
	return &StageError{Stage: stage, Err: err}
}

// WorkspaceDir returns the absolute path of the git workspace for a job.
// Uses the job ID (not the SHA) so the path is stable even when HEAD is resolved.
func (p *Pipeline) WorkspaceDir(job *domain.DeployJob) string {
	return filepath.Join(p.WorkspacesDir, job.ProjectID, job.ID)
}

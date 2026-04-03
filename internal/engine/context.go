// Package engine manages job execution lifecycle.
// Three-file boundary — do NOT break:
//   - context.go  — cancellation and timeout propagation ONLY
//   - supervisor.go — goroutine lifecycle ONLY (spawn, track, cancel, drain)
//   - runner.go   — the ONLY file that calls pipeline stages
package engine

import (
	"context"
	"time"

	"github.com/udaypankhaniya/trangly/internal/domain"
)

const (
	// defaultJobTimeout is the maximum wall-clock time allowed for a single deployment.
	defaultJobTimeout = 30 * time.Minute
)

// WithJobTimeout wraps parent with a project-specific deadline.
// If the project has no custom timeout configured, defaultJobTimeout is used.
func WithJobTimeout(parent context.Context, job *domain.DeployJob) (context.Context, context.CancelFunc) {
	// TODO: per-project timeout from project config when available.
	_ = job
	return context.WithTimeout(parent, defaultJobTimeout)
}

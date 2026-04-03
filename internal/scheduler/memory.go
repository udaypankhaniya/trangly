// Package scheduler implements RAM-aware job execution decisions.
// The scheduler is the ONLY component allowed to start jobs — it delegates to engine.
// It MUST NEVER call pipeline stages directly or import internal/deploy.
package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/udaypankhaniya/trangly/internal/infra/docker"
)

const statsSampleInterval = 2 * time.Second

// StatSample is a single RSS measurement taken during a running job.
type StatSample = docker.StatSample

// Sampler collects docker stats for running containers and computes RAM estimates.
type Sampler struct {
	docker *docker.Client
	log    *slog.Logger
}

// NewSampler creates a Sampler.
func NewSampler(dc *docker.Client) *Sampler {
	return &Sampler{
		docker: dc,
		log:    slog.Default().With("component", "sampler"),
	}
}

// StartSampling begins sampling RSS for containerID every 2 seconds until ctx is cancelled.
// The returned channel carries samples; it is closed when sampling stops.
func (s *Sampler) StartSampling(ctx context.Context, containerID string) (<-chan StatSample, error) {
	return s.docker.Stats(ctx, containerID, statsSampleInterval)
}

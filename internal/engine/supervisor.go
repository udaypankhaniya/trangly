package engine

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

// Supervisor manages goroutine lifecycle for the engine.
// It tracks all active goroutines and provides a clean shutdown path.
// No pipeline logic belongs here.
type Supervisor struct {
	mu     sync.Mutex
	active map[string]context.CancelFunc // job ID → cancel func
	wg     sync.WaitGroup
	log    *slog.Logger
}

// NewSupervisor creates a Supervisor.
func NewSupervisor() *Supervisor {
	return &Supervisor{
		active: make(map[string]context.CancelFunc),
		log:    slog.Default().With("component", "supervisor"),
	}
}

// Spawn starts fn in a goroutine tracked under jobID.
// Returns an error if jobID is already active.
func (s *Supervisor) Spawn(jobID string, fn func(ctx context.Context)) error {
	s.mu.Lock()
	if _, exists := s.active[jobID]; exists {
		s.mu.Unlock()
		return fmt.Errorf("supervisor: job %s is already running", jobID)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.active[jobID] = cancel
	s.mu.Unlock()

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer s.release(jobID)
		s.log.Info("goroutine spawned", "job_id", jobID)
		fn(ctx)
		s.log.Info("goroutine finished", "job_id", jobID)
	}()
	return nil
}

// Cancel stops a running goroutine by cancelling its context.
func (s *Supervisor) Cancel(jobID string) {
	s.mu.Lock()
	cancel, ok := s.active[jobID]
	s.mu.Unlock()
	if ok {
		s.log.Info("cancelling goroutine", "job_id", jobID)
		cancel()
	}
}

// ActiveCount returns the number of goroutines currently tracked.
func (s *Supervisor) ActiveCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.active)
}

// Drain waits for all active goroutines to exit, or until ctx is done.
func (s *Supervisor) Drain(ctx context.Context) {
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		s.log.Info("all goroutines exited cleanly")
	case <-ctx.Done():
		s.log.Warn("drain timeout — some goroutines may still be running")
	}
}

func (s *Supervisor) release(jobID string) {
	s.mu.Lock()
	delete(s.active, jobID)
	s.mu.Unlock()
}

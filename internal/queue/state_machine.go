// Package queue manages per-project job queues and job state transitions.
// The state machine is the single source of truth for all status changes.
package queue

import (
	"fmt"

	"github.com/udaypankhaniya/trangly/internal/domain"
)

// allowedTransitions defines every valid (from → to) transition.
// This is the authoritative state graph for Trangly jobs.
// Any code that changes job status MUST call ValidateTransition first.
var allowedTransitions = map[string][]string{
	domain.StatusPending:     {domain.StatusHeld, domain.StatusRunning, domain.StatusFailed},
	domain.StatusHeld:        {domain.StatusPending, domain.StatusFailed},
	domain.StatusRunning:     {domain.StatusBuilding, domain.StatusHotSwapping, domain.StatusFailed},
	domain.StatusBuilding:    {domain.StatusHealthCheck, domain.StatusFailed},
	domain.StatusHealthCheck: {domain.StatusSwapping, domain.StatusFailed},
	domain.StatusSwapping:    {domain.StatusSuccess, domain.StatusFailed},
	domain.StatusHotSwapping: {domain.StatusSuccess, domain.StatusFailed},
	domain.StatusSuccess:     {}, // terminal
	domain.StatusFailed:      {}, // terminal
}

// ValidateTransition checks whether a transition from → to is allowed.
// Returns a descriptive error if the transition is forbidden.
func ValidateTransition(from, to string) error {
	allowed, ok := allowedTransitions[from]
	if !ok {
		return fmt.Errorf("state_machine: unknown status %q", from)
	}
	for _, s := range allowed {
		if s == to {
			return nil
		}
	}
	return fmt.Errorf("state_machine: transition %q → %q is not allowed", from, to)
}

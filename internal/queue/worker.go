package queue

import "github.com/udaypankhaniya/trangly/internal/domain"

// Worker represents a slot for executing one deploy job.
// The actual execution is delegated to the engine layer — the worker itself
// carries no pipeline logic.
type Worker struct {
	ID    int
	JobID string
}

// IsIdle returns true when the worker is not currently assigned to a job.
func (w *Worker) IsIdle() bool {
	return w.JobID == ""
}

// Assign marks the worker as busy with the given job.
func (w *Worker) Assign(job *domain.DeployJob) {
	w.JobID = job.ID
}

// Release marks the worker as idle again.
func (w *Worker) Release() {
	w.JobID = ""
}

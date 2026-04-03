package handlers

import (
	"github.com/gofiber/fiber/v2"

	"github.com/udaypankhaniya/trangly/internal/app"
	"github.com/udaypankhaniya/trangly/internal/queue"
)

// QueueHandler handles queue management endpoints.
type QueueHandler struct {
	deploySvc *app.DeployService
	queueMgr  *queue.Manager
}

// NewQueueHandler creates a QueueHandler.
func NewQueueHandler(deploySvc *app.DeployService, queueMgr *queue.Manager) *QueueHandler {
	return &QueueHandler{deploySvc: deploySvc, queueMgr: queueMgr}
}

// List handles GET /api/queue.
// Returns all active (non-terminal) jobs across all projects.
func (h *QueueHandler) List(c *fiber.Ctx) error {
	jobs, err := h.deploySvc.ListAllActive()
	if err != nil {
		return respondError(c, fiber.StatusInternalServerError, err.Error())
	}
	return respondJSON(c, fiber.StatusOK, fiber.Map{"jobs": jobs})
}

// Cancel handles DELETE /api/queue/:job_id.
// Only pending or held jobs can be cancelled.
func (h *QueueHandler) Cancel(c *fiber.Ctx) error {
	jobID := c.Params("job_id")
	if jobID == "" {
		return respondError(c, fiber.StatusBadRequest, "missing job ID")
	}
	if err := h.queueMgr.Cancel(jobID); err != nil {
		return respondError(c, fiber.StatusBadRequest, err.Error())
	}
	return c.SendStatus(fiber.StatusNoContent)
}

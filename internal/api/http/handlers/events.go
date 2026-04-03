package handlers

import (
	"encoding/json"

	"github.com/gofiber/fiber/v2"

	"github.com/udaypankhaniya/trangly/internal/api/sse"
	"github.com/udaypankhaniya/trangly/internal/app"
)

// EventsHandler handles the SSE dashboard event stream.
type EventsHandler struct {
	deploySvc   *app.DeployService
	broadcaster *sse.Broadcaster
}

// NewEventsHandler creates an EventsHandler.
func NewEventsHandler(deploySvc *app.DeployService, broadcaster *sse.Broadcaster) *EventsHandler {
	return &EventsHandler{deploySvc: deploySvc, broadcaster: broadcaster}
}

// Stream handles GET /api/events (SSE).
// Sends an initial snapshot of active jobs, then streams live status changes.
func (h *EventsHandler) Stream(c *fiber.Ctx) error {
	// Build initial snapshot of active jobs.
	jobs, err := h.deploySvc.ListAllActive()
	if err != nil {
		return respondError(c, fiber.StatusInternalServerError, err.Error())
	}

	initial, _ := json.Marshal(fiber.Map{"jobs": jobs})
	return h.broadcaster.StreamDashboard(c, string(initial))
}

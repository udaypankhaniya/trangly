package handlers

import (
	"errors"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"

	"github.com/udaypankhaniya/trangly/internal/api/sse"
	"github.com/udaypankhaniya/trangly/internal/app"
	"github.com/udaypankhaniya/trangly/internal/infra/db"
)

// DeployHandler handles deployment endpoints.
type DeployHandler struct {
	deploySvc   *app.DeployService
	broadcaster *sse.Broadcaster
}

// NewDeployHandler creates a DeployHandler.
func NewDeployHandler(deploySvc *app.DeployService, broadcaster *sse.Broadcaster) *DeployHandler {
	return &DeployHandler{deploySvc: deploySvc, broadcaster: broadcaster}
}

// TriggerDeploy handles POST /api/projects/:id/deploy.
func (h *DeployHandler) TriggerDeploy(c *fiber.Ctx) error {
	projectID := c.Params("id")
	if projectID == "" {
		return respondError(c, fiber.StatusBadRequest, "missing project ID")
	}

	var req struct {
		CommitSHA string `json:"commit_sha"`
		Branch    string `json:"branch"`
	}
	if err := c.BodyParser(&req); err != nil {
		return respondError(c, fiber.StatusBadRequest, "invalid JSON: "+err.Error())
	}
	if req.CommitSHA == "" {
		return respondError(c, fiber.StatusBadRequest, "commit_sha is required")
	}
	if req.Branch == "" {
		req.Branch = "main"
	}

	job, err := h.deploySvc.Trigger(projectID, req.CommitSHA, req.Branch)
	if errors.Is(err, app.ErrNotFound) {
		return respondError(c, fiber.StatusNotFound, "project not found")
	}
	if err != nil {
		return respondError(c, fiber.StatusInternalServerError, err.Error())
	}
	return respondJSON(c, fiber.StatusCreated, job)
}

// allowedSortBy is the set of column names the client may sort by.
var allowedSortBy = map[string]bool{
	"queued_at":   true,
	"status":      true,
	"branch":      true,
	"commit_sha":  true,
	"finished_at": true,
}

// ListDeployments handles GET /api/deployments.
// Supports server-side sorting, searching, filtering, and pagination.
//
//	Query params:
//	  limit      int    (1–100, default 20)
//	  offset     int    (≥0, default 0)
//	  sort_by    string (queued_at|status|branch|commit_sha|finished_at)
//	  sort_dir   string (asc|desc, default desc)
//	  search     string (partial match on commit, branch, error, project name/repo)
//	  project_id string (optional filter)
//	  status     string (optional filter)
func (h *DeployHandler) ListDeployments(c *fiber.Ctx) error {
	limit := 20
	offset := 0
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	if v := c.Query("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	sortBy := "queued_at"
	if v := c.Query("sort_by"); v != "" && allowedSortBy[v] {
		sortBy = v
	}
	sortDir := "DESC"
	if v := strings.ToUpper(c.Query("sort_dir")); v == "ASC" {
		sortDir = "ASC"
	}

	search := strings.TrimSpace(c.Query("search"))
	if len(search) > 100 {
		search = search[:100]
	}

	params := db.DeploymentQueryParams{
		Limit:     limit,
		Offset:    offset,
		SortBy:    sortBy,
		SortDir:   sortDir,
		Search:    search,
		ProjectID: c.Query("project_id"),
		Status:    c.Query("status"),
	}

	result, err := h.deploySvc.ListDeploymentsAdvanced(params)
	if err != nil {
		return respondError(c, fiber.StatusInternalServerError, err.Error())
	}
	return respondJSON(c, fiber.StatusOK, result)
}

// StreamLogs handles GET /api/deployments/:id/logs (SSE).
func (h *DeployHandler) StreamLogs(c *fiber.Ctx) error {
	jobID := c.Params("id")
	if jobID == "" {
		return respondError(c, fiber.StatusBadRequest, "missing deployment ID")
	}

	job, err := h.deploySvc.GetJob(jobID)
	if errors.Is(err, app.ErrNotFound) {
		return respondError(c, fiber.StatusNotFound, "deployment not found")
	}
	if err != nil {
		return respondError(c, fiber.StatusInternalServerError, err.Error())
	}

	return h.broadcaster.StreamLog(c, jobID, job.LogPath)
}

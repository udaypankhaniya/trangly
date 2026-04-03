package handlers

import (
	"math"

	"github.com/gofiber/fiber/v2"

	"github.com/udaypankhaniya/trangly/internal/app"
	"github.com/udaypankhaniya/trangly/internal/infra/db"
	"github.com/udaypankhaniya/trangly/internal/infra/docker"
	"github.com/udaypankhaniya/trangly/internal/infra/system"
	"github.com/udaypankhaniya/trangly/pkg/version"
)

// SystemHandler handles system-level endpoints.
type SystemHandler struct {
	db      *db.DB
	authSvc *app.AuthService
	docker  *docker.Client
}

// NewSystemHandler creates a SystemHandler.
func NewSystemHandler(database *db.DB, authSvc *app.AuthService, dc *docker.Client) *SystemHandler {
	return &SystemHandler{db: database, authSvc: authSvc, docker: dc}
}

// Version handles GET /api/version.
// Returns the structured version info from pkg/version.
func (h *SystemHandler) Version(c *fiber.Ctx) error {
	return respondJSON(c, fiber.StatusOK, version.Info())
}

// Preflight handles GET /api/system/preflight.
// Returns the full preflight check results as structured JSON.
func (h *SystemHandler) Preflight(c *fiber.Ctx) error {
	results := system.RunChecks(c.Context())
	return respondJSON(c, fiber.StatusOK, fiber.Map{
		"checks":     results,
		"all_passed": system.AllPassed(results),
	})
}

// RAM handles GET /api/system/ram.
// Returns current host memory statistics.
func (h *SystemHandler) RAM(c *fiber.Ctx) error {
	mem, err := system.ReadMemInfo()
	if err != nil {
		return respondError(c, fiber.StatusInternalServerError, "failed to read memory info: "+err.Error())
	}
	return respondJSON(c, fiber.StatusOK, fiber.Map{
		"total_mb":     mem.TotalMB,
		"available_mb": mem.AvailableMB,
	})
}

// SetupStatus handles GET /api/setup/status (public).
// Returns whether the initial admin account has been created.
func (h *SystemHandler) SetupStatus(c *fiber.Ctx) error {
	exists, err := h.db.UserExists()
	if err != nil {
		return respondError(c, fiber.StatusInternalServerError, "failed to check setup status")
	}
	return respondJSON(c, fiber.StatusOK, fiber.Map{
		"setup_complete": exists,
	})
}

// Setup handles POST /api/setup (public, one-shot).
// Creates the admin account. Rejects with 409 if an admin already exists.
func (h *SystemHandler) Setup(c *fiber.Ctx) error {
	exists, err := h.db.UserExists()
	if err != nil {
		return respondError(c, fiber.StatusInternalServerError, "failed to check setup status")
	}
	if exists {
		return respondError(c, fiber.StatusConflict, "admin account already exists")
	}

	var req struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := c.BodyParser(&req); err != nil {
		return respondError(c, fiber.StatusBadRequest, "invalid request body")
	}
	if req.Username == "" {
		return respondError(c, fiber.StatusBadRequest, "username is required")
	}
	if req.Email == "" {
		return respondError(c, fiber.StatusBadRequest, "email is required")
	}
	if len(req.Password) < 8 {
		return respondError(c, fiber.StatusBadRequest, "password must be at least 8 characters")
	}
	if len(req.Password) > app.MaxPasswordLen {
		return respondError(c, fiber.StatusBadRequest, "password too long")
	}

	if err := h.authSvc.CreateAdmin(req.Username, req.Email, req.Password); err != nil {
		return respondError(c, fiber.StatusInternalServerError, "failed to create admin: "+err.Error())
	}

	return respondJSON(c, fiber.StatusCreated, fiber.Map{
		"message": "admin account created",
	})
}

// Stats handles GET /api/system/stats.
// Returns aggregated system statistics for the dashboard footer.
func (h *SystemHandler) Stats(c *fiber.Ctx) error {
	// CPU usage
	cpuPercent := 0.0
	cpuInfo, err := system.ReadCPUInfo()
	if err == nil {
		cpuPercent = math.Round(cpuInfo.UsagePercent*10) / 10
	}

	// Running containers
	containers := 0
	if h.docker != nil {
		n, err := h.docker.RunningContainerCount(c.Context())
		if err == nil {
			containers = n
		}
	}

	// Deploy stats
	var totalDeploys int64
	var successRate float64
	counts, err := h.db.DeployJobCounts()
	if err == nil {
		totalDeploys = counts.Total
		if counts.Total > 0 {
			successRate = math.Round(float64(counts.Success)/float64(counts.Total)*1000) / 10
		}
	}

	return respondJSON(c, fiber.StatusOK, fiber.Map{
		"cpu_usage_percent":    cpuPercent,
		"active_containers":    containers,
		"total_deploys":        totalDeploys,
		"success_rate_percent": successRate,
	})
}

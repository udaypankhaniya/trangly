package handlers

import (
	"errors"

	"github.com/gofiber/fiber/v2"

	"github.com/udaypankhaniya/trangly/internal/app"
	"github.com/udaypankhaniya/trangly/internal/domain"
)

// ProjectHandler handles project CRUD endpoints.
type ProjectHandler struct {
	projectSvc *app.ProjectService
	deploySvc  *app.DeployService
	ghAuthSvc  *app.GitHubAuthService
}

// NewProjectHandler creates a ProjectHandler.
func NewProjectHandler(projectSvc *app.ProjectService, deploySvc *app.DeployService, ghAuthSvc *app.GitHubAuthService) *ProjectHandler {
	return &ProjectHandler{projectSvc: projectSvc, deploySvc: deploySvc, ghAuthSvc: ghAuthSvc}
}

// Get handles GET /api/projects/:id.
func (h *ProjectHandler) Get(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return respondError(c, fiber.StatusBadRequest, "missing project ID")
	}
	project, err := h.projectSvc.GetByID(id)
	if errors.Is(err, app.ErrNotFound) {
		return respondError(c, fiber.StatusNotFound, "project not found")
	}
	if err != nil {
		return respondError(c, fiber.StatusInternalServerError, err.Error())
	}
	type projectWithDeploy struct {
		*domain.Project
		LastDeploy *domain.DeployJob `json:"last_deploy"`
	}
	pd := projectWithDeploy{Project: project}
	if last, err := h.deploySvc.GetLastDeploy(project.ID); err == nil {
		pd.LastDeploy = last
	}
	return respondJSON(c, fiber.StatusOK, pd)
}

// List handles GET /api/projects.
func (h *ProjectHandler) List(c *fiber.Ctx) error {
	projects, err := h.projectSvc.List()
	if err != nil {
		return respondError(c, fiber.StatusInternalServerError, err.Error())
	}

	type projectWithDeploy struct {
		*domain.Project
		LastDeploy *domain.DeployJob `json:"last_deploy"`
	}

	result := make([]projectWithDeploy, 0, len(projects))
	for _, p := range projects {
		pd := projectWithDeploy{Project: p}
		if last, err := h.deploySvc.GetLastDeploy(p.ID); err == nil {
			pd.LastDeploy = last
		}
		result = append(result, pd)
	}
	return respondJSON(c, fiber.StatusOK, fiber.Map{"projects": result})
}

// Create handles POST /api/projects.
func (h *ProjectHandler) Create(c *fiber.Ctx) error {
	ctx, cancel := requestCtx(c)
	defer cancel()

	var req struct {
		Name           string `json:"name"`
		RepoFullName   string `json:"repo_full_name"`
		DefaultBranch  string `json:"default_branch"`
		ComposePath    string `json:"compose_path"`
		MemLimitMB     int64  `json:"mem_limit_mb"`
		DeployMode     string `json:"deploy_mode"`
		WebhookSecret  string `json:"webhook_secret"`
		InstallationID int64  `json:"installation_id"`
		EnvVars        string `json:"env_vars"`
	}
	if err := c.BodyParser(&req); err != nil {
		return respondError(c, fiber.StatusBadRequest, "invalid JSON: "+err.Error())
	}
	if req.Name == "" || req.RepoFullName == "" {
		return respondError(c, fiber.StatusBadRequest, "name and repo_full_name are required")
	}
	if req.DefaultBranch == "" {
		req.DefaultBranch = "main"
	}
	if req.ComposePath == "" {
		req.ComposePath = "docker-compose.yml"
	}

	// Auto-fill installation_id from GitHub App if not provided.
	if req.InstallationID == 0 {
		status, err := h.ghAuthSvc.GetStatus(ctx)
		if err == nil && status.HasInstallation {
			req.InstallationID = status.InstallationID
		}
	}

	project, err := h.projectSvc.Create(
		req.Name, req.RepoFullName, req.DefaultBranch,
		req.ComposePath, req.MemLimitMB, req.DeployMode, req.WebhookSecret, req.InstallationID, req.EnvVars,
	)
	if err != nil {
		return respondError(c, fiber.StatusInternalServerError, err.Error())
	}
	return respondJSON(c, fiber.StatusCreated, project)
}

// Update handles PUT /api/projects/:id.
func (h *ProjectHandler) Update(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return respondError(c, fiber.StatusBadRequest, "missing project ID")
	}
	var req struct {
		Name          string `json:"name"`
		DefaultBranch string `json:"default_branch"`
		ComposePath   string `json:"compose_path"`
		MemLimitMB    int64  `json:"mem_limit_mb"`
		DeployMode    string `json:"deploy_mode"`
		EnvVars       string `json:"env_vars"`
	}
	if err := c.BodyParser(&req); err != nil {
		return respondError(c, fiber.StatusBadRequest, "invalid JSON: "+err.Error())
	}

	project, err := h.projectSvc.Update(id, req.Name, req.DefaultBranch, req.ComposePath, req.MemLimitMB, req.DeployMode, req.EnvVars)
	if errors.Is(err, app.ErrNotFound) {
		return respondError(c, fiber.StatusNotFound, "project not found")
	}
	if err != nil {
		return respondError(c, fiber.StatusInternalServerError, err.Error())
	}
	return respondJSON(c, fiber.StatusOK, project)
}

// Delete handles DELETE /api/projects/:id.
func (h *ProjectHandler) Delete(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return respondError(c, fiber.StatusBadRequest, "missing project ID")
	}
	if err := h.projectSvc.Delete(id); err != nil {
		return respondError(c, fiber.StatusInternalServerError, err.Error())
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// GetEnv handles GET /api/projects/:id/env.
// Returns the project's decrypted environment variables as key/value pairs.
// Values are returned in plaintext — only call this over an authenticated,
// encrypted connection. The JSON response is never cached.
func (h *ProjectHandler) GetEnv(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return respondError(c, fiber.StatusBadRequest, "missing project ID")
	}
	vars, err := h.projectSvc.GetEnvVars(id)
	if errors.Is(err, app.ErrNotFound) {
		return respondError(c, fiber.StatusNotFound, "project not found")
	}
	if err != nil {
		return respondError(c, fiber.StatusInternalServerError, err.Error())
	}
	c.Set("Cache-Control", "no-store")
	return respondJSON(c, fiber.StatusOK, fiber.Map{"vars": vars})
}

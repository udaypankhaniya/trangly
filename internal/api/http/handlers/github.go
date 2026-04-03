package handlers

import (
	"regexp"

	"github.com/gofiber/fiber/v2"

	"github.com/udaypankhaniya/trangly/internal/app"
)

// GitHubHandler handles GitHub App setup endpoints.
type GitHubHandler struct {
	ghAuthSvc *app.GitHubAuthService
	baseURL   string
}

// NewGitHubHandler creates a GitHubHandler.
func NewGitHubHandler(ghAuthSvc *app.GitHubAuthService, baseURL string) *GitHubHandler {
	return &GitHubHandler{ghAuthSvc: ghAuthSvc, baseURL: baseURL}
}

// Manifest handles GET /api/github/manifest.
// Returns the manifest JSON and the GitHub redirect URL.
// The frontend must POST the manifest as a form field named "manifest" to the redirect URL.
func (h *GitHubHandler) Manifest(c *fiber.Ctx) error {
	ctx, cancel := requestCtx(c)
	defer cancel()

	// Use configured base URL if provided; otherwise derive from the incoming request.
	// This covers the common case where --base-url is not passed to the binary.
	baseURL := h.baseURL
	if baseURL == "" {
		baseURL = c.Protocol() + "://" + c.Get("Host")
	}

	result, err := h.ghAuthSvc.GenerateManifest(ctx, baseURL)
	if err != nil {
		return respondError(c, fiber.StatusInternalServerError, "failed to generate manifest: "+err.Error())
	}
	return respondJSON(c, fiber.StatusOK, result)
}

// Callback handles GET /api/github/callback.
// Validates state token and exchanges code for app credentials.
func (h *GitHubHandler) Callback(c *fiber.Ctx) error {
	ctx, cancel := requestCtx(c)
	defer cancel()

	code := c.Query("code")
	state := c.Query("state")
	if code == "" || state == "" {
		return respondError(c, fiber.StatusBadRequest, "missing code or state parameter")
	}

	if err := h.ghAuthSvc.HandleCallback(ctx, code, state); err != nil {
		return respondError(c, fiber.StatusBadRequest, "callback failed: "+err.Error())
	}

	// Redirect to the UI settings page.
	return c.Redirect("/settings?github=connected", fiber.StatusFound)
}

// Status handles GET /api/github/status.
func (h *GitHubHandler) Status(c *fiber.Ctx) error {
	ctx, cancel := requestCtx(c)
	defer cancel()

	status, err := h.ghAuthSvc.GetStatus(ctx)
	if err != nil {
		return respondError(c, fiber.StatusInternalServerError, "failed to get status: "+err.Error())
	}
	return respondJSON(c, fiber.StatusOK, status)
}

// Repos handles GET /api/github/repos.
func (h *GitHubHandler) Repos(c *fiber.Ctx) error {
	ctx, cancel := requestCtx(c)
	defer cancel()

	repos, err := h.ghAuthSvc.ListRepos(ctx)
	if err != nil {
		return respondError(c, fiber.StatusInternalServerError, "failed to list repos: "+err.Error())
	}
	return respondJSON(c, fiber.StatusOK, fiber.Map{"repositories": repos})
}

// InstallURL handles GET /api/github/install-url.
// Returns the URL to redirect users to GitHub's installation page.
func (h *GitHubHandler) InstallURL(c *fiber.Ctx) error {
	ctx, cancel := requestCtx(c)
	defer cancel()

	url, err := h.ghAuthSvc.GetInstallURL(ctx)
	if err != nil {
		return respondError(c, fiber.StatusBadRequest, "GitHub App not connected")
	}
	return respondJSON(c, fiber.StatusOK, fiber.Map{"install_url": url})
}

// Disconnect handles DELETE /api/github/app.
// Removes the GitHub integration from Trangly.
// Query param ?remove_from_github=true will also delete the installation on GitHub.
func (h *GitHubHandler) Disconnect(c *fiber.Ctx) error {
	ctx, cancel := requestCtx(c)
	defer cancel()

	removeFromGitHub := c.Query("remove_from_github") == "true"

	if err := h.ghAuthSvc.Disconnect(ctx, removeFromGitHub); err != nil {
		return respondError(c, fiber.StatusInternalServerError, "failed to disconnect: "+err.Error())
	}
	return respondJSON(c, fiber.StatusOK, fiber.Map{"status": "disconnected"})
}

// repoNamePattern validates owner/repo path segments: alphanumeric, '-', '_', '.'.
var repoNamePattern = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

// branchNamePattern validates branch names: alphanumeric, '-', '_', '.', '/'.
var branchNamePattern = regexp.MustCompile(`^[a-zA-Z0-9._\-/]+$`)

// hexSHAPattern validates a commit SHA: 7-40 hex characters.
var hexSHAPattern = regexp.MustCompile(`^[0-9a-fA-F]{7,40}$`)

// Branches handles GET /api/github/repos/:owner/:repo/branches.
// Returns the list of branches for a repository.
func (h *GitHubHandler) Branches(c *fiber.Ctx) error {
	ctx, cancel := requestCtx(c)
	defer cancel()

	owner := c.Params("owner")
	repo := c.Params("repo")
	if !repoNamePattern.MatchString(owner) || !repoNamePattern.MatchString(repo) {
		return respondError(c, fiber.StatusBadRequest, "invalid owner or repo name")
	}

	branches, err := h.ghAuthSvc.ListBranches(ctx, owner+"/"+repo)
	if err != nil {
		return respondError(c, fiber.StatusInternalServerError, "failed to list branches: "+err.Error())
	}
	return respondJSON(c, fiber.StatusOK, fiber.Map{"branches": branches})
}

// ComposeFiles handles GET /api/github/repos/:owner/:repo/compose-files.
// Returns detected Docker Compose files at the repo root for the given ref.
func (h *GitHubHandler) ComposeFiles(c *fiber.Ctx) error {
	ctx, cancel := requestCtx(c)
	defer cancel()

	owner := c.Params("owner")
	repo := c.Params("repo")
	if !repoNamePattern.MatchString(owner) || !repoNamePattern.MatchString(repo) {
		return respondError(c, fiber.StatusBadRequest, "invalid owner or repo name")
	}

	ref := c.Query("ref", "")
	if ref != "" && !branchNamePattern.MatchString(ref) {
		return respondError(c, fiber.StatusBadRequest, "invalid ref parameter")
	}

	files, err := h.ghAuthSvc.DetectComposeFiles(ctx, owner+"/"+repo, ref)
	if err != nil {
		return respondError(c, fiber.StatusInternalServerError, "failed to detect compose files: "+err.Error())
	}
	if files == nil {
		files = []string{}
	}
	return respondJSON(c, fiber.StatusOK, fiber.Map{"files": files})
}

// SetCommitStatus handles POST /api/repos/:owner/:repo/status.
// Sets a commit status on GitHub for the given repository and SHA.
func (h *GitHubHandler) SetCommitStatus(c *fiber.Ctx) error {
	ctx, cancel := requestCtx(c)
	defer cancel()

	owner := c.Params("owner")
	repo := c.Params("repo")

	if !repoNamePattern.MatchString(owner) || !repoNamePattern.MatchString(repo) {
		return respondError(c, fiber.StatusBadRequest, "invalid owner or repo name")
	}
	repoFullName := owner + "/" + repo

	var body struct {
		SHA         string `json:"sha"`
		State       string `json:"state"`
		Description string `json:"description"`
	}
	if err := c.BodyParser(&body); err != nil {
		return respondError(c, fiber.StatusBadRequest, "invalid request body")
	}

	if !hexSHAPattern.MatchString(body.SHA) {
		return respondError(c, fiber.StatusBadRequest, "sha must be a 7-40 character hex string")
	}

	validStates := map[string]bool{"pending": true, "success": true, "failure": true, "error": true}
	if !validStates[body.State] {
		return respondError(c, fiber.StatusBadRequest, "state must be one of: pending, success, failure, error")
	}

	if err := h.ghAuthSvc.SetCommitStatus(ctx, repoFullName, body.SHA, body.State, body.Description); err != nil {
		return respondError(c, fiber.StatusInternalServerError, "failed to set commit status: "+err.Error())
	}

	return respondJSON(c, fiber.StatusOK, fiber.Map{"status": "created"})
}

package app

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/udaypankhaniya/trangly/internal/infra/crypto"
	"github.com/udaypankhaniya/trangly/internal/infra/db"
	githubapi "github.com/udaypankhaniya/trangly/internal/infra/github"
	"github.com/udaypankhaniya/trangly/pkg/version"
)

// GitHubAuthService manages the GitHub App magic-link setup flow.
// It owns: manifest generation, state token issuance, callback exchange, credential storage.
// It MUST NOT handle webhook routing (that belongs to GitHubWebhookService).
type GitHubAuthService struct {
	db         *db.DB
	masterKey  []byte
	tokenCache *githubapi.TokenCache
	log        *slog.Logger
}

// GitHubStatus is the response for GET /api/github/status.
type GitHubStatus struct {
	Connected       bool   `json:"connected"`
	AppSlug         string `json:"app_slug,omitempty"`
	InstallationID  int64  `json:"installation_id,omitempty"`
	HasInstallation bool   `json:"has_installation"`
}

// NewGitHubAuthService creates a GitHubAuthService.
func NewGitHubAuthService(database *db.DB, masterKey []byte, tokenCache *githubapi.TokenCache) *GitHubAuthService {
	return &GitHubAuthService{
		db:         database,
		masterKey:  masterKey,
		tokenCache: tokenCache,
		log:        slog.Default().With("service", "github_auth"),
	}
}

// ManifestResult holds the URL and manifest for the GitHub App creation form.
type ManifestResult struct {
	RedirectURL string         `json:"redirect_url"`
	Manifest    map[string]any `json:"manifest"`
}

// GenerateManifest builds the GitHub App manifest and a one-time state token URL.
// The frontend must POST the manifest JSON as a form field to the redirect URL.
// baseURL is the public URL of this Trangly instance (e.g. "https://my-vps.example.com").
func (s *GitHubAuthService) GenerateManifest(ctx context.Context, baseURL string) (*ManifestResult, error) {
	stateBytes, err := crypto.RandomBytes(32)
	if err != nil {
		return nil, fmt.Errorf("github_auth: generate state token: %w", err)
	}
	state := hex.EncodeToString(stateBytes)

	expiresAt := time.Now().UTC().Add(10 * time.Minute)
	if err := s.db.InsertStateToken(state, expiresAt); err != nil {
		return nil, fmt.Errorf("github_auth: store state token: %w", err)
	}

	// Purge stale tokens as a side effect (non-fatal).
	_ = s.db.PurgeExpiredStateTokens()

	callbackURL := baseURL + "/api/github/callback"
	// Use first 8 hex chars of the state token as a unique suffix so the GitHub
	// App name never collides with a previously created app.
	manifest := buildManifest(baseURL, callbackURL, state[:8])

	return &ManifestResult{
		RedirectURL: fmt.Sprintf("https://github.com/settings/apps/new?state=%s", state),
		Manifest:    manifest,
	}, nil
}

// HandleCallback is called by GET /api/github/callback after GitHub redirects back.
// It validates the state token (single-use, not expired), exchanges the code for
// app credentials, encrypts them, and stores them in the database.
func (s *GitHubAuthService) HandleCallback(ctx context.Context, code, state string) error {
	if err := s.db.ConsumeStateToken(state); err != nil {
		return fmt.Errorf("github_auth: invalid state token: %w", err)
	}

	creds, err := githubapi.ExchangeManifest(ctx, code)
	if err != nil {
		return fmt.Errorf("github_auth: exchange manifest: %w", err)
	}

	// Validate the PEM is a usable RSA key before encrypting and storing.
	// This prevents persisting unusable key material that would cause 401s forever.
	pemBytes := []byte(creds.PEM)
	pemInfo, err := githubapi.ValidatePEM(pemBytes)
	if err != nil {
		return fmt.Errorf("github_auth: PEM from GitHub is not a valid RSA key: %w", err)
	}

	// Log a public key fingerprint so we can compare store-time vs use-time.
	storeApp, err := githubapi.NewApp(creds.AppID, pemBytes)
	if err != nil {
		return fmt.Errorf("github_auth: PEM parse round-trip failed: %w", err)
	}
	_ = storeApp // logged by NewApp; fingerprint will appear as pubkey_fp

	s.log.Info("PEM validated from GitHub manifest exchange",
		"block_type", pemInfo.BlockType,
		"key_bits", pemInfo.KeyBits,
		"app_id", creds.AppID,
		"pem_len", len(pemBytes),
	)

	// Encrypt sensitive credentials immediately — never stored in plaintext.
	encWebhookSecret, err := crypto.Encrypt(s.masterKey, []byte(creds.WebhookSecret))
	if err != nil {
		return fmt.Errorf("github_auth: encrypt webhook secret: %w", err)
	}
	encPEM, err := crypto.Encrypt(s.masterKey, pemBytes)
	if err != nil {
		return fmt.Errorf("github_auth: encrypt PEM: %w", err)
	}

	if err := s.db.UpsertGitHubApp(creds.AppID, creds.AppSlug, encWebhookSecret, encPEM); err != nil {
		return fmt.Errorf("github_auth: store app credentials: %w", err)
	}

	s.log.Info("GitHub App connected", "app_slug", creds.AppSlug, "app_id", creds.AppID)
	return nil
}

// GetStatus returns whether a GitHub App is connected.
func (s *GitHubAuthService) GetStatus(ctx context.Context) (GitHubStatus, error) {
	row, err := s.db.GetGitHubApp()
	if errors.Is(err, db.ErrNotFound) {
		return GitHubStatus{Connected: false}, nil
	}
	if err != nil {
		return GitHubStatus{}, fmt.Errorf("github_auth: get app: %w", err)
	}
	return GitHubStatus{
		Connected:       true,
		AppSlug:         row.AppSlug,
		InstallationID:  row.InstallationID,
		HasInstallation: row.InstallationID > 0,
	}, nil
}

// ListRepos returns the repositories accessible via the GitHub App installation.
func (s *GitHubAuthService) ListRepos(ctx context.Context) ([]githubapi.Repo, error) {
	token, err := s.getInstallationToken(ctx)
	if err != nil {
		return nil, err
	}
	return githubapi.ListRepos(ctx, token)
}

// ListBranches returns the branches for a specific repository via the GitHub App installation.
func (s *GitHubAuthService) ListBranches(ctx context.Context, repoFullName string) ([]githubapi.Branch, error) {
	token, err := s.getInstallationToken(ctx)
	if err != nil {
		return nil, err
	}
	return githubapi.ListBranches(ctx, token, repoFullName)
}

// DetectComposeFiles returns Docker Compose files found at the root of a repository.
// It filters for files matching docker-compose*.yml, docker-compose*.yaml,
// compose*.yml, or compose*.yaml patterns.
func (s *GitHubAuthService) DetectComposeFiles(ctx context.Context, repoFullName, ref string) ([]string, error) {
	token, err := s.getInstallationToken(ctx)
	if err != nil {
		return nil, err
	}

	entries, err := githubapi.ListContents(ctx, token, repoFullName, "", ref)
	if err != nil {
		return nil, fmt.Errorf("github_auth: list repo contents: %w", err)
	}

	var files []string
	for _, e := range entries {
		if e.Type != "file" {
			continue
		}
		if isComposeFile(e.Name) {
			files = append(files, e.Name)
		}
	}
	return files, nil
}

// getInstallationToken returns a cached installation token for the configured GitHub App.
func (s *GitHubAuthService) getInstallationToken(ctx context.Context) (string, error) {
	row, err := s.db.GetGitHubApp()
	if errors.Is(err, db.ErrNotFound) {
		return "", fmt.Errorf("github_auth: app not connected")
	}
	if err != nil {
		return "", fmt.Errorf("github_auth: get app: %w", err)
	}
	if row.InstallationID == 0 {
		return "", fmt.Errorf("github_auth: app not installed on any repositories yet")
	}

	pemPlain, err := crypto.Decrypt(s.masterKey, row.PEM)
	if err != nil {
		return "", fmt.Errorf("github_auth: decrypt PEM: %w", err)
	}

	app, err := githubapi.NewApp(row.AppID, pemPlain)
	if err != nil {
		return "", fmt.Errorf("github_auth: parse private key: %w", err)
	}

	cachedApp := githubapi.NewCachedApp(app, s.tokenCache)
	return cachedApp.GetInstallationToken(ctx, row.InstallationID)
}

// isComposeFile checks if a filename matches Docker Compose file patterns.
func isComposeFile(name string) bool {
	patterns := []string{
		"docker-compose.yml", "docker-compose.yaml",
		"compose.yml", "compose.yaml",
	}
	// Exact match first.
	for _, p := range patterns {
		if name == p {
			return true
		}
	}
	// Prefix match for variants like docker-compose.prod.yml.
	for _, prefix := range []string{"docker-compose.", "compose."} {
		if len(name) > len(prefix) {
			suffix := name[len(prefix):]
			if hasSuffix(suffix, ".yml") || hasSuffix(suffix, ".yaml") {
				return true
			}
		}
	}
	return false
}

func hasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

// GetInstallURL returns the GitHub App installation URL for the user to
// select which repositories Trangly can access.
func (s *GitHubAuthService) GetInstallURL(ctx context.Context) (string, error) {
	row, err := s.db.GetGitHubApp()
	if errors.Is(err, db.ErrNotFound) {
		return "", fmt.Errorf("github_auth: app not connected")
	}
	if err != nil {
		return "", fmt.Errorf("github_auth: get app: %w", err)
	}
	return fmt.Sprintf("https://github.com/apps/%s/installations/new", row.AppSlug), nil
}

// UpdatePEM reads a new PEM private key from disk, validates it, encrypts it,
// and replaces the stored PEM in the database. Use this when a GitHub App's
// private key has been regenerated from GitHub settings.
func (s *GitHubAuthService) UpdatePEM(ctx context.Context, pemFilePath string) error {
	pemData, err := os.ReadFile(pemFilePath) //nolint:gosec
	if err != nil {
		return fmt.Errorf("github_auth: read PEM file: %w", err)
	}

	// Validate the PEM is a usable RSA key — not just valid PEM structure.
	pemInfo, err := githubapi.ValidatePEM(pemData)
	if err != nil {
		return fmt.Errorf("github_auth: PEM file is not a valid RSA key: %w", err)
	}
	s.log.Info("PEM file validated", "pem_len", len(pemData), "block_type", pemInfo.BlockType, "key_bits", pemInfo.KeyBits)

	row, err := s.db.GetGitHubApp()
	if errors.Is(err, db.ErrNotFound) {
		return fmt.Errorf("github_auth: no GitHub App configured — run the manifest flow first")
	}
	if err != nil {
		return fmt.Errorf("github_auth: get app: %w", err)
	}

	encPEM, err := crypto.Encrypt(s.masterKey, pemData)
	if err != nil {
		return fmt.Errorf("github_auth: encrypt PEM: %w", err)
	}

	if err := s.db.UpsertGitHubApp(row.AppID, row.AppSlug, row.WebhookSecret, encPEM); err != nil {
		return fmt.Errorf("github_auth: store updated PEM: %w", err)
	}

	// Invalidate cached token — the old PEM's token is no longer usable.
	s.tokenCache.Invalidate()

	s.log.Info("PEM updated successfully",
		"app_id", row.AppID,
		"app_slug", row.AppSlug,
		"new_pem_len", len(pemData),
	)
	return nil
}

// Disconnect removes the GitHub App integration from Trangly.
// If removeFromGitHub is true, it also deletes the installation on GitHub's side
// (best-effort — if the JWT is stale the local disconnect still succeeds).
func (s *GitHubAuthService) Disconnect(ctx context.Context, removeFromGitHub bool) error {
	row, err := s.db.GetGitHubApp()
	if errors.Is(err, db.ErrNotFound) {
		return nil // already disconnected
	}
	if err != nil {
		return fmt.Errorf("github_auth: get app: %w", err)
	}

	// Best-effort removal of the installation on GitHub.
	if removeFromGitHub && row.InstallationID > 0 {
		pemPlain, decErr := crypto.Decrypt(s.masterKey, row.PEM)
		if decErr != nil {
			s.log.Warn("disconnect: could not decrypt PEM for GitHub removal", "err", decErr)
		} else {
			app, appErr := githubapi.NewApp(row.AppID, pemPlain)
			if appErr != nil {
				s.log.Warn("disconnect: could not parse PEM for GitHub removal", "err", appErr)
			} else if ghErr := app.DeleteInstallation(ctx, row.InstallationID); ghErr != nil {
				s.log.Warn("disconnect: GitHub installation removal failed (best-effort)", "err", ghErr)
			} else {
				s.log.Info("GitHub installation removed from GitHub", "installation_id", row.InstallationID)
			}
		}
	}

	if err := s.db.DeleteGitHubApp(); err != nil {
		return fmt.Errorf("github_auth: delete app row: %w", err)
	}

	// Invalidate cached installation token — credentials are gone.
	s.tokenCache.Invalidate()

	s.log.Info("GitHub App disconnected", "app_slug", row.AppSlug, "removed_from_github", removeFromGitHub)
	return nil
}

// validCommitStates are the allowed GitHub commit status states.
var validCommitStates = map[string]bool{
	"pending": true, "success": true, "failure": true, "error": true,
}

// SetCommitStatus sets the commit status on a specific SHA via the GitHub Statuses API.
// repoFullName must be in "owner/repo" format.
// state must be one of: "pending", "success", "failure", "error".
func (s *GitHubAuthService) SetCommitStatus(ctx context.Context, repoFullName, sha, state, description string) error {
	if !validCommitStates[state] {
		return fmt.Errorf("github_auth: invalid commit status state %q", state)
	}

	row, err := s.db.GetGitHubApp()
	if errors.Is(err, db.ErrNotFound) {
		return fmt.Errorf("github_auth: app not connected")
	}
	if err != nil {
		return fmt.Errorf("github_auth: get app: %w", err)
	}
	if row.InstallationID == 0 {
		return fmt.Errorf("github_auth: app not installed on any repositories yet")
	}

	pemPlain, err := crypto.Decrypt(s.masterKey, row.PEM)
	if err != nil {
		return fmt.Errorf("github_auth: decrypt PEM: %w", err)
	}

	app, err := githubapi.NewApp(row.AppID, pemPlain)
	if err != nil {
		return fmt.Errorf("github_auth: parse private key: %w", err)
	}

	cachedApp := githubapi.NewCachedApp(app, s.tokenCache)
	token, err := cachedApp.GetInstallationToken(ctx, row.InstallationID)
	if err != nil {
		return fmt.Errorf("github_auth: get installation token: %w", err)
	}

	if err := githubapi.WithRetry(ctx, func() error {
		return githubapi.SetCommitStatus(ctx, token, repoFullName, sha, state, description)
	}); err != nil {
		return fmt.Errorf("github_auth: set commit status: %w", err)
	}

	s.log.Info("commit status set",
		"repo", repoFullName,
		"sha", sha[:minInt(7, len(sha))],
		"state", state,
	)
	return nil
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// buildManifest returns the JSON manifest payload for GitHub App creation.
// This structure is POSTed to GitHub when the user submits the manifest form.
// nameSuffix is appended to the app name to ensure uniqueness across installs.
func buildManifest(baseURL, callbackURL, nameSuffix string) map[string]any {
	return map[string]any{
		"name": version.AppName + "-" + nameSuffix,
		"url":  baseURL,
		"hook_attributes": map[string]string{
			"url": baseURL + "/webhooks/github",
		},
		"callback_urls": []string{callbackURL},
		"redirect_url":  callbackURL,
		// After the user selects repos on GitHub, redirect straight to Add Project.
		"setup_url":      baseURL + "/project/edit",
		"public":         false,
		"default_events": []string{"push", "create"},
		"default_permissions": map[string]string{
			"contents": "read",
			"metadata": "read",
			"statuses": "write",
		},
	}
}

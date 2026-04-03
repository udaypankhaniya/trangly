package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/udaypankhaniya/trangly/internal/domain"
	"github.com/udaypankhaniya/trangly/internal/infra/crypto"
	"github.com/udaypankhaniya/trangly/internal/infra/db"
)

// GitHubWebhookService routes inbound GitHub webhook events to the appropriate action.
// It owns: event routing and DeployJob insertion.
// It MUST NOT handle auth, manifest, or token exchange (that belongs to GitHubAuthService).
type GitHubWebhookService struct {
	db        *db.DB
	deploySvc *DeployService
	masterKey []byte
	log       *slog.Logger
}

// NewGitHubWebhookService creates a GitHubWebhookService.
func NewGitHubWebhookService(database *db.DB, deploySvc *DeployService, masterKey []byte) *GitHubWebhookService {
	return &GitHubWebhookService{
		db:        database,
		deploySvc: deploySvc,
		masterKey: masterKey,
		log:       slog.Default().With("service", "github_webhook"),
	}
}

// RouteEvent parses and routes a validated GitHub webhook event.
// eventType is the X-GitHub-Event header value.
// payload is the raw (already HMAC-verified) request body.
func (s *GitHubWebhookService) RouteEvent(ctx context.Context, eventType string, payload []byte) error {
	switch eventType {
	case "push":
		return s.handlePush(ctx, payload)
	case "create":
		return s.handleCreate(ctx, payload)
	case "installation":
		return s.handleInstallation(ctx, payload)
	case "ping":
		s.log.Info("github webhook ping received")
		return nil
	default:
		s.log.Info("ignoring unrecognised github event", "event", eventType)
		return nil
	}
}

// GetWebhookSecretForRepo returns the decrypted webhook secret for a given repository.
// This is used by the webhook handler to validate the X-Hub-Signature-256 header.
func (s *GitHubWebhookService) GetWebhookSecretForRepo(repoFullName string) (string, error) {
	row, err := s.db.GetGitHubApp()
	if err != nil {
		return "", fmt.Errorf("webhook: get app: %w", err)
	}
	plain, err := crypto.Decrypt(s.masterKey, row.WebhookSecret)
	if err != nil {
		return "", fmt.Errorf("webhook: decrypt webhook secret: %w", err)
	}
	return string(plain), nil
}

// PushEvent contains the fields we extract from a GitHub push webhook payload.
type PushEvent struct {
	Ref        string `json:"ref"`   // e.g. "refs/heads/main"
	After      string `json:"after"` // commit SHA pushed to
	Repository struct {
		FullName      string `json:"full_name"`
		DefaultBranch string `json:"default_branch"`
	} `json:"repository"`
}

// CreateEvent contains the fields we extract from a GitHub create webhook payload.
type CreateEvent struct {
	RefType    string `json:"ref_type"` // "branch" | "tag"
	Ref        string `json:"ref"`
	Repository struct {
		FullName      string `json:"full_name"`
		DefaultBranch string `json:"default_branch"`
	} `json:"repository"`
}

func (s *GitHubWebhookService) handlePush(ctx context.Context, payload []byte) error {
	var event PushEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return fmt.Errorf("webhook: parse push event: %w", err)
	}

	branch := branchFromRef(event.Ref)
	repoFullName := event.Repository.FullName
	commitSHA := event.After

	// Ignore branch deletions (zero SHA).
	if commitSHA == "0000000000000000000000000000000000000000" {
		s.log.Info("ignoring push deletion event", "repo", repoFullName)
		return nil
	}

	// Look up project for this repository.
	row, err := s.db.GetProjectByRepo(repoFullName)
	if err != nil {
		// No project configured for this repo — silently ignore.
		s.log.Debug("no project for repo, skipping", "repo", repoFullName)
		return nil
	}

	// Only trigger on the configured default branch.
	if branch != row.DefaultBranch {
		s.log.Debug("push on non-default branch, skipping",
			"repo", repoFullName, "branch", branch, "default", row.DefaultBranch)
		return nil
	}

	job, err := s.deploySvc.Trigger(row.ID, commitSHA, branch)
	if err != nil {
		return fmt.Errorf("webhook: trigger deploy: %w", err)
	}
	s.log.Info("deploy job created from push webhook",
		"job_id", job.ID, "repo", repoFullName, "sha", job.ShortSHA())
	return nil
}

func (s *GitHubWebhookService) handleCreate(ctx context.Context, payload []byte) error {
	var event CreateEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return fmt.Errorf("webhook: parse create event: %w", err)
	}

	// Only act on tag creation, not branch creation.
	if event.RefType != "tag" {
		return nil
	}

	repoFullName := event.Repository.FullName
	s.log.Info("tag created", "repo", repoFullName, "tag", event.Ref)

	// Tag-based deploys: look up project and trigger with the tag ref as the SHA placeholder.
	// The fetch stage will resolve the actual commit SHA.
	row, err := s.db.GetProjectByRepo(repoFullName)
	if err != nil {
		s.log.Debug("no project for repo, skipping tag deploy", "repo", repoFullName)
		return nil
	}

	job, err := s.deploySvc.Trigger(row.ID, event.Ref, domain.StatusPending)
	if err != nil {
		return fmt.Errorf("webhook: trigger deploy from tag: %w", err)
	}
	s.log.Info("deploy job created from tag webhook",
		"job_id", job.ID, "repo", repoFullName, "tag", event.Ref)
	return nil
}

// InstallationEvent contains the fields we extract from a GitHub installation webhook payload.
type InstallationEvent struct {
	Action       string `json:"action"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
}

func (s *GitHubWebhookService) handleInstallation(ctx context.Context, payload []byte) error {
	var event InstallationEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return fmt.Errorf("webhook: parse installation event: %w", err)
	}

	if event.Action != "created" {
		s.log.Info("ignoring installation event", "action", event.Action)
		return nil
	}

	if err := s.db.SetGitHubInstallationID(event.Installation.ID); err != nil {
		return fmt.Errorf("webhook: store installation id: %w", err)
	}

	s.log.Info("GitHub App installed", "installation_id", event.Installation.ID)
	return nil
}

// branchFromRef extracts the branch name from a full git ref (e.g. "refs/heads/main" → "main").
func branchFromRef(ref string) string {
	const prefix = "refs/heads/"
	if len(ref) > len(prefix) && ref[:len(prefix)] == prefix {
		return ref[len(prefix):]
	}
	return ref
}

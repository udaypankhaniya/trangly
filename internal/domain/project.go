package domain

import "time"

// Project represents a registered GitHub repository that Trangly monitors and deploys.
type Project struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`            // user-friendly display name
	RepoFullName   string    `json:"repo_full_name"`  // e.g. "owner/repo"
	DefaultBranch  string    `json:"default_branch"`  // branch to watch for pushes
	ComposePath    string    `json:"compose_path"`    // path to docker-compose.yml within repo
	MemLimitMB     int64     `json:"mem_limit_mb"`    // optional: container memory limit; 0 = unset
	WebhookSecret  string    `json:"-"`               // HMAC secret for GitHub webhooks (encrypted at rest)
	EnvVars        string    `json:"-"`               // newline-delimited KEY=VALUE pairs (encrypted at rest)
	DeployMode     string    `json:"deploy_mode"`     // "rebuild" (default) or "hot_swap"
	InstallationID int64     `json:"installation_id"` // GitHub App installation ID for this repo
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

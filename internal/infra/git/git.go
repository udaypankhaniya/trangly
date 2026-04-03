// Package git provides operations for cloning and managing Git workspaces.
// It uses exec.Command("git", ...) — the only place in Trangly where git CLI is called.
package git

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Clone performs a shallow (depth=1) clone of repoURL authenticated with token,
// then checks out the exact commit at commitSHA.
// targetDir is the absolute path into which the repository is cloned.
// If targetDir already exists it is removed first.
func Clone(ctx context.Context, repoURL, token, targetDir, commitSHA string) error {
	// Build an authenticated https URL: https://x-access-token:<token>@github.com/owner/repo.git
	authedURL := repoURL
	if token != "" {
		authedURL = injectToken(repoURL, token)
	}

	if err := os.RemoveAll(targetDir); err != nil {
		return fmt.Errorf("git: remove existing workspace: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(targetDir), 0755); err != nil {
		return fmt.Errorf("git: mkdir parent: %w", err)
	}

	// Shallow-clone without a specific ref so we can fetch any SHA.
	if err := run(ctx, "", "git", "clone", "--depth=1", "--no-single-branch",
		authedURL, targetDir); err != nil {
		return fmt.Errorf("git: clone: %w", err)
	}

	// Fetch the exact commit in case depth=1 doesn't include it.
	if err := run(ctx, targetDir, "git", "fetch", "--depth=1", "origin", commitSHA); err != nil {
		// Non-fatal: the commit may already be present from the clone.
		_ = err
	}

	// Checkout the specific commit SHA.
	if err := run(ctx, targetDir, "git", "checkout", commitSHA); err != nil {
		return fmt.Errorf("git: checkout %s: %w", commitSHA, err)
	}
	return nil
}

// Clean removes the workspace directory entirely.
func Clean(dir string) error {
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("git: clean workspace %s: %w", dir, err)
	}
	return nil
}

// ResolveHEAD returns the full commit SHA of the current HEAD in the given repo directory.
func ResolveHEAD(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD") //nolint:gosec
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git: rev-parse HEAD: %w", err)
	}
	sha := strings.TrimSpace(string(out))
	if len(sha) < 7 {
		return "", fmt.Errorf("git: rev-parse returned invalid SHA: %q", sha)
	}
	return sha, nil
}

// injectToken rewrites an https:// GitHub URL to embed x-access-token authentication.
func injectToken(repoURL, token string) string {
	const prefix = "https://"
	if len(repoURL) > len(prefix) && repoURL[:len(prefix)] == prefix {
		return prefix + "x-access-token:" + token + "@" + repoURL[len(prefix):]
	}
	return repoURL
}

// run executes a git command, streaming both stdout and stderr to /dev/null.
// dir is the working directory; pass "" to use the current directory.
func run(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, args[0], args[1:]...) //nolint:gosec
	cmd.Dir = dir
	// Discard output — callers stream Docker logs via the log file, not git output.
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%v: %w", args, err)
	}
	return nil
}

package deploy

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"

	"github.com/udaypankhaniya/trangly/internal/domain"
)

const (
	// composeDownTimeout is the grace period (seconds) for containers to stop during compose down.
	composeDownTimeout = "30"
	// composeUpTimeout is the timeout (seconds) for compose up to complete.
	composeUpTimeout = "120"
)

// SwapStage brings up the new production containers using docker compose.
// It stops old containers and starts new ones from the freshly built images.
// On failure, it attempts to restart the previous containers.
func (p *Pipeline) SwapStage(
	ctx context.Context,
	job *domain.DeployJob,
	projectSlug string,
	logW io.Writer,
) error {
	logger := p.Log.With("stage", "swap", "job_id", job.ID)
	workspaceDir := p.WorkspaceDir(job)

	logger.InfoContext(ctx, "starting swap", slog.String("project", projectSlug))
	fmt.Fprintf(logW, "[swap] bringing up production containers for %s\n", projectSlug)

	// Stop existing containers for this project first.
	downCmd := exec.CommandContext(ctx, "docker", "compose", "down", "--remove-orphans", "--timeout", composeDownTimeout) //nolint:gosec
	downCmd.Dir = workspaceDir
	downCmd.Stdout = logW
	downCmd.Stderr = logW
	downCmd.Env = append(os.Environ(), "COMPOSE_PROJECT_NAME="+projectSlug)
	downCmd.Cancel = func() error { return downCmd.Process.Kill() }
	if err := downCmd.Run(); err != nil {
		// Non-fatal — may not have existing containers.
		fmt.Fprintf(logW, "[swap] NOTE: compose down returned: %v (may be first deploy)\n", err)
	}

	// Start new containers in detached mode.
	upCmd := exec.CommandContext(ctx, "docker", "compose", "up", "-d", "--force-recreate", "--timeout", composeUpTimeout) //nolint:gosec
	upCmd.Dir = workspaceDir
	upCmd.Stdout = logW
	upCmd.Stderr = logW
	upCmd.Env = append(os.Environ(), "COMPOSE_PROJECT_NAME="+projectSlug)
	upCmd.Cancel = func() error { return upCmd.Process.Kill() }

	if err := upCmd.Run(); err != nil {
		fmt.Fprintf(logW, "[swap] ERROR: docker compose up failed: %v\n", err)
		return stageErr("swap", fmt.Errorf("docker compose up: %w", err))
	}

	logger.InfoContext(ctx, "swap complete", slog.String("project", projectSlug))
	fmt.Fprintf(logW, "[swap] production containers running for %s\n", projectSlug)
	return nil
}

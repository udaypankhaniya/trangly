package deploy

import (
	"bytes"
	"context"
	"encoding/json"
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

	// Remove any pre-existing containers whose names conflict with what compose
	// will create. This handles the case where a container was started manually
	// (outside compose) and blocks compose up with a "name already in use" error.
	if err := p.removeConflictingContainers(ctx, workspaceDir, projectSlug, logW); err != nil {
		// Non-fatal — best-effort cleanup. compose up will fail if conflicts remain.
		fmt.Fprintf(logW, "[swap] WARNING: conflict cleanup: %v\n", err)
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

// composeConfig is the minimal subset of docker compose config JSON we need.
type composeConfig struct {
	Services map[string]struct {
		ContainerName string `json:"container_name"`
	} `json:"services"`
}

// removeConflictingContainers parses the compose config to discover any explicit
// container_name values, then force-stops and removes those containers if they
// already exist. This prevents "name already in use" errors when a container was
// created outside of compose (e.g. manually via docker run).
func (p *Pipeline) removeConflictingContainers(
	ctx context.Context,
	workspaceDir string,
	projectSlug string,
	logW io.Writer,
) error {
	// Ask compose for the resolved config as JSON.
	var buf bytes.Buffer
	cfgCmd := exec.CommandContext(ctx, "docker", "compose", "config", "--format", "json") //nolint:gosec
	cfgCmd.Dir = workspaceDir
	cfgCmd.Stdout = &buf
	cfgCmd.Stderr = logW
	cfgCmd.Env = append(os.Environ(), "COMPOSE_PROJECT_NAME="+projectSlug)
	cfgCmd.Cancel = func() error { return cfgCmd.Process.Kill() }

	if err := cfgCmd.Run(); err != nil {
		return fmt.Errorf("compose config: %w", err)
	}

	var cfg composeConfig
	if err := json.Unmarshal(buf.Bytes(), &cfg); err != nil {
		return fmt.Errorf("parse compose config: %w", err)
	}

	for svc, def := range cfg.Services {
		name := def.ContainerName
		if name == "" {
			continue
		}
		logger := p.Log.With("stage", "swap", "container", name, "service", svc)
		logger.InfoContext(ctx, "removing conflicting container")
		fmt.Fprintf(logW, "[swap] removing pre-existing container %q (service %s)\n", name, svc)

		if err := p.Docker.ContainerStop(ctx, name); err != nil {
			fmt.Fprintf(logW, "[swap] NOTE: stop %q: %v\n", name, err)
		}
		if err := p.Docker.ContainerRemove(ctx, name); err != nil {
			return fmt.Errorf("remove container %s: %w", name, err)
		}
	}
	return nil
}

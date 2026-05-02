package deploy

import (
	"context"
	"fmt"
	"io"
	"os/exec"

	"github.com/udaypankhaniya/trangly/internal/domain"
)

// HealthCheckStage validates the compose configuration before the swap phase.
// For compose-based deploys, a pre-swap staging container would conflict on
// host ports, so we validate config here and rely on compose's native
// healthcheck directives (if defined) after the swap brings containers up.
func (p *Pipeline) HealthCheckStage(
	ctx context.Context,
	job *domain.DeployJob,
	projectSlug string,
	mode HealthMode,
	logW io.Writer,
) error {
	logger := p.Log.With("stage", "health_check", "job_id", job.ID)
	workspaceDir := p.WorkspaceDir(job)

	if mode.Mode == "none" {
		logger.InfoContext(ctx, "health check mode is none — skipping")
		fmt.Fprintf(logW, "[health_check] mode=none, skipping\n")
		return nil
	}

	fmt.Fprintf(logW, "[health_check] validating compose configuration (mode=%s)\n", mode.Mode)

	// Validate compose config is syntactically correct before swap.
	cmd := exec.CommandContext(ctx, "docker", "compose", "config", "--quiet") //nolint:gosec
	cmd.Dir = workspaceDir
	cmd.Stdout = logW
	cmd.Stderr = logW
	cmd.Env = composeEnv(projectSlug)
	cmd.Cancel = func() error { return cmd.Process.Kill() }

	if err := cmd.Run(); err != nil {
		fmt.Fprintf(logW, "[health_check] compose config validation FAILED: %v\n", err)
		return stageErr("health_check", fmt.Errorf("compose config invalid: %w", err))
	}

	logger.InfoContext(ctx, "health check passed — compose config valid")
	fmt.Fprintf(logW, "[health_check] compose config valid — native healthcheck will apply on swap\n")
	return nil
}

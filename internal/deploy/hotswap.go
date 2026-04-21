package deploy

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"

	"github.com/udaypankhaniya/trangly/internal/domain"
)

// HotSwapStage copies updated files from the workspace into the running
// container(s) using `docker cp`. This avoids a full rebuild for apps like
// PHP/Laravel that only need the source files replaced.
// If the container is not running, it returns an error.
func (p *Pipeline) HotSwapStage(
	ctx context.Context,
	job *domain.DeployJob,
	projectSlug string,
	logW io.Writer,
) error {
	logger := p.Log.With("stage", "hot_swap", "job_id", job.ID)
	workspaceDir := p.WorkspaceDir(job)

	logger.InfoContext(ctx, "starting hot-swap", slog.String("project", projectSlug))
	fmt.Fprintf(logW, "[hot_swap] copying files into running container(s) for %s\n", projectSlug)

	// Find the running container(s) for this compose project.
	containers, err := listComposeContainers(ctx, workspaceDir, projectSlug)
	if err != nil {
		fmt.Fprintf(logW, "[hot_swap] ERROR: could not list containers: %v\n", err)
		return stageErr("hot_swap", fmt.Errorf("list containers: %w", err))
	}
	if len(containers) == 0 {
		fmt.Fprintf(logW, "[hot_swap] ERROR: no running containers found for project %s\n", projectSlug)
		return stageErr("hot_swap", fmt.Errorf("no running containers for project %q — run a full rebuild first", projectSlug))
	}

	// Copy workspace files into each running container's working directory.
	for _, ctr := range containers {
		ctr = strings.TrimSpace(ctr)
		if ctr == "" {
			continue
		}

		// Determine the container's working directory.
		workDir, err := inspectWorkDir(ctx, ctr)
		if err != nil {
			logger.WarnContext(ctx, "could not inspect workdir, using /app", "container", ctr, "err", err)
			workDir = "/app"
		}

		// docker cp {workspace}/. {container}:{workdir}
		src := workspaceDir + string(os.PathSeparator) + "."
		dst := ctr + ":" + workDir

		fmt.Fprintf(logW, "[hot_swap] copying files to %s (%s)\n", ctr, workDir)
		cpCmd := exec.CommandContext(ctx, "docker", "cp", src, dst) //nolint:gosec
		cpCmd.Stdout = logW
		cpCmd.Stderr = logW
		cpCmd.Cancel = func() error { return cpCmd.Process.Kill() }

		if err := cpCmd.Run(); err != nil {
			fmt.Fprintf(logW, "[hot_swap] ERROR: docker cp failed for %s: %v\n", ctr, err)
			return stageErr("hot_swap", fmt.Errorf("docker cp to %s: %w", ctr, err))
		}

		logger.InfoContext(ctx, "files copied", "container", ctr, "dest", workDir)
		fmt.Fprintf(logW, "[hot_swap] files copied to %s successfully\n", ctr)
	}

	logger.InfoContext(ctx, "hot-swap complete", slog.String("project", projectSlug))
	fmt.Fprintf(logW, "[hot_swap] all containers updated for %s\n", projectSlug)
	return nil
}

// listComposeContainers returns the IDs of running containers for a compose project.
func listComposeContainers(ctx context.Context, workspaceDir, projectSlug string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", "compose", "ps", "-q") //nolint:gosec
	cmd.Dir = workspaceDir
	cmd.Env = composeEnv(projectSlug)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			ids = append(ids, line)
		}
	}
	return ids, nil
}

// inspectWorkDir returns the configured working directory of a container.
func inspectWorkDir(ctx context.Context, containerID string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{.Config.WorkingDir}}", containerID) //nolint:gosec
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	dir := strings.TrimSpace(string(out))
	if dir == "" {
		return "/app", nil
	}
	return dir, nil
}

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

// BuildStage runs `docker compose build` in the workspace directory.
// The resulting images are named by Docker Compose's convention.
func (p *Pipeline) BuildStage(ctx context.Context, job *domain.DeployJob, projectSlug string, logW io.Writer) error {
	workspaceDir := p.WorkspaceDir(job)
	imageTag := fmt.Sprintf("%s:sha-%s", projectSlug, job.ShortSHA())

	logger := p.Log.With("stage", "build", "job_id", job.ID, "sha", job.ShortSHA())
	logger.InfoContext(ctx, "starting build", "workspace", workspaceDir, "image_tag", imageTag)
	fmt.Fprintf(logW, "[build] running docker compose build in %s\n", workspaceDir)

	// Build all services defined in the compose file.
	cmd := exec.CommandContext(ctx, "docker", "compose", "build") //nolint:gosec
	cmd.Dir = workspaceDir
	cmd.Stdout = logW
	cmd.Stderr = logW
	cmd.Env = append(os.Environ(),
		"COMPOSE_PROJECT_NAME="+projectSlug,
		"DOCKER_BUILDKIT=1",
	)
	cmd.Cancel = func() error { return cmd.Process.Kill() }

	if err := cmd.Run(); err != nil {
		fmt.Fprintf(logW, "[build] ERROR: docker compose build failed: %v\n", err)
		return stageErr("build", fmt.Errorf("docker compose build: %w", err))
	}

	// Discover compose-built images and tag the first one with our SHA tag.
	builtImage := discoverComposeImage(ctx, workspaceDir, projectSlug)
	if builtImage != "" {
		if err := p.Docker.ImageTag(ctx, builtImage, imageTag); err != nil {
			logger.WarnContext(ctx, "could not tag compose image", "source", builtImage, "target", imageTag, "err", err)
		} else {
			fmt.Fprintf(logW, "[build] tagged %s → %s\n", builtImage, imageTag)
		}
	} else {
		logger.WarnContext(ctx, "could not discover compose image name — swap will use compose up")
	}

	logger.InfoContext(ctx, "build complete", slog.String("image_tag", imageTag))
	fmt.Fprintf(logW, "[build] build complete\n")
	return nil
}

// discoverComposeImage runs `docker compose config --images` to find the image
// name that compose assigned. Returns the first image found, or empty string.
func discoverComposeImage(ctx context.Context, workspaceDir, projectSlug string) string {
	cmd := exec.CommandContext(ctx, "docker", "compose", "config", "--images") //nolint:gosec
	cmd.Dir = workspaceDir
	cmd.Env = append(os.Environ(), "COMPOSE_PROJECT_NAME="+projectSlug)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

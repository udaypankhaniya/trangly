package deploy

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/udaypankhaniya/trangly/internal/domain"
	"github.com/udaypankhaniya/trangly/internal/infra/git"
)

// CleanupStage removes the git workspace, prunes old images, and performs
// final bookkeeping. It is always called — even on failure — to avoid leaking
// workspace directories. Errors here are logged but do not affect job status.
func (p *Pipeline) CleanupStage(
	ctx context.Context,
	job *domain.DeployJob,
	projectSlug string,
	succeeded bool,
	logW io.Writer,
) {
	logger := p.Log.With("stage", "cleanup", "job_id", job.ID)

	// Remove the git workspace directory.
	workspaceDir := p.WorkspaceDir(job)
	if err := git.Clean(workspaceDir); err != nil {
		logger.WarnContext(ctx, "could not remove workspace", slog.String("dir", workspaceDir), slog.Any("err", err))
		fmt.Fprintf(logW, "[cleanup] WARNING: could not remove workspace %s: %v\n", workspaceDir, err)
	} else {
		fmt.Fprintf(logW, "[cleanup] workspace removed: %s\n", workspaceDir)
	}

	// Prune dangling (<none>) images left over from the build.
	if err := p.Docker.PruneImages(ctx); err != nil {
		logger.WarnContext(ctx, "could not prune dangling images", slog.Any("err", err))
		fmt.Fprintf(logW, "[cleanup] WARNING: could not prune dangling images: %v\n", err)
	} else {
		fmt.Fprintf(logW, "[cleanup] dangling images pruned\n")
	}

	if succeeded {
		logger.InfoContext(ctx, "cleanup complete")
		fmt.Fprintf(logW, "[cleanup] deploy complete\n")
	} else {
		logger.InfoContext(ctx, "cleanup complete (build failed — images retained for inspection)")
		fmt.Fprintf(logW, "[cleanup] build failed — old images retained for inspection\n")
	}
}

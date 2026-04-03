package deploy

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/udaypankhaniya/trangly/internal/domain"
	"github.com/udaypankhaniya/trangly/internal/infra/git"
)

// FetchStage clones the repository at the exact commit SHA into a workspace directory.
// The workspace path is: {WorkspacesDir}/{projectID}/{shortSHA}/
func (p *Pipeline) FetchStage(ctx context.Context, job *domain.DeployJob, repoURL, token string, logW io.Writer) error {
	targetDir := p.WorkspaceDir(job)

	logger := p.Log.With("stage", "fetch", "job_id", job.ID, "sha", job.ShortSHA())
	logger.InfoContext(ctx, "starting fetch", "repo", repoURL, "target", targetDir)
	fmt.Fprintf(logW, "[fetch] cloning %s @ %s\n", repoURL, job.ShortSHA())

	if err := git.Clone(ctx, repoURL, token, targetDir, job.CommitSHA); err != nil {
		fmt.Fprintf(logW, "[fetch] ERROR: %v\n", err)
		return stageErr("fetch", err)
	}

	// Verify the workspace was created.
	if _, err := os.Stat(targetDir); err != nil {
		return stageErr("fetch", fmt.Errorf("workspace not found after clone: %w", err))
	}

	logger.InfoContext(ctx, "fetch complete", slog.String("dir", targetDir))
	fmt.Fprintf(logW, "[fetch] workspace ready: %s\n", targetDir)
	return nil
}

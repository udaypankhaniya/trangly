// Package engine executes deploy jobs end-to-end.
// runner.go calls pipeline stages, supervisor.go manages goroutine lifecycle,
// and context.go handles cancellation and timeout propagation.
package engine

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/udaypankhaniya/trangly/internal/api/sse"
	"github.com/udaypankhaniya/trangly/internal/deploy"
	"github.com/udaypankhaniya/trangly/internal/domain"
	"github.com/udaypankhaniya/trangly/internal/infra/crypto"
	"github.com/udaypankhaniya/trangly/internal/infra/db"
	"github.com/udaypankhaniya/trangly/internal/infra/git"
	"github.com/udaypankhaniya/trangly/internal/infra/github"
	"github.com/udaypankhaniya/trangly/internal/queue"
)

// Runner executes one deploy job end-to-end.
// This is the ONLY file that calls pipeline stage functions.
// It uses queue.Manager to update job status at each phase boundary.
type Runner struct {
	pipeline    *deploy.Pipeline
	queue       *queue.Manager
	db          *db.DB
	masterKey   []byte
	supervisor  *Supervisor
	broadcaster *sse.Broadcaster
	log         *slog.Logger
}

// NewRunner creates a Runner.
func NewRunner(
	pipeline *deploy.Pipeline,
	qm *queue.Manager,
	database *db.DB,
	masterKey []byte,
	supervisor *Supervisor,
	broadcaster *sse.Broadcaster,
) *Runner {
	return &Runner{
		pipeline:    pipeline,
		queue:       qm,
		db:          database,
		masterKey:   masterKey,
		supervisor:  supervisor,
		broadcaster: broadcaster,
		log:         slog.Default().With("component", "runner"),
	}
}

// Run executes a full deployment pipeline for one job.
// Called exclusively by the scheduler (via the JobRunner interface).
// Status is updated in the queue at each phase boundary.
func (r *Runner) Run(ctx context.Context, job *domain.DeployJob) error {
	jobCtx, cancel := WithJobTimeout(ctx, job)
	defer cancel()

	logger := r.log.With("job_id", job.ID, "sha", job.ShortSHA())
	logger.InfoContext(jobCtx, "starting job execution")

	// Open job log file with SSE broadcasting.
	logFile, err := openJobLog(job.LogPath)
	if err != nil {
		logger.ErrorContext(jobCtx, "cannot open job log", "path", job.LogPath, "err", err)
		return r.fail(job, fmt.Errorf("cannot open log file: %w", err))
	}
	defer logFile.Close()
	logW := r.newBroadcastWriter(logFile, job.ID)

	// Load project and credentials.
	project, err := r.db.GetProject(job.ProjectID)
	if err != nil {
		return r.fail(job, fmt.Errorf("load project: %w", err))
	}

	// Get the git clone token.
	installID := project.InstallationID
	if installID == 0 {
		// Fall back to the GitHub App's installation_id for older projects.
		if appRow, err := r.db.GetGitHubApp(); err == nil && appRow.InstallationID > 0 {
			installID = appRow.InstallationID
		}
	}
	if installID == 0 {
		return r.fail(job, fmt.Errorf("no GitHub App installation found — install the app on your repos in Settings"))
	}
	token, err := r.getInstallationToken(jobCtx, installID)
	if err != nil {
		return r.fail(job, fmt.Errorf("get installation token: %w", err))
	}

	repoURL := "https://github.com/" + project.RepoFullName + ".git"
	projectSlug := safeSlug(project.Name)

	// --- FETCH --- (job is already in "running" state; MarkStarted was called
	// by the scheduler synchronously before this goroutine was spawned.)
	if err := r.pipeline.FetchStage(jobCtx, job, repoURL, token, logW); err != nil {
		return r.fail(job, err)
	}

	// Resolve HEAD to actual commit SHA if the caller passed "HEAD".
	if job.CommitSHA == "HEAD" {
		resolvedSHA, err := git.ResolveHEAD(jobCtx, r.pipeline.WorkspaceDir(job))
		if err != nil {
			return r.fail(job, fmt.Errorf("resolve HEAD: %w", err))
		}
		job.CommitSHA = resolvedSHA
		_ = r.db.UpdateJobCommitSHA(job.ID, resolvedSHA)
		logger.InfoContext(jobCtx, "resolved HEAD", "sha", job.ShortSHA())
		fmt.Fprintf(logW, "[fetch] resolved HEAD → %s\n", job.ShortSHA())
	}

	// Branch on deploy mode: hot_swap skips BUILD, HEALTH CHECK, SWAP.
	if project.DeployMode == domain.DeployModeHotSwap {
		return r.runHotSwapPath(jobCtx, job, project, projectSlug, token, logW, logger)
	}
	return r.runRebuildPath(jobCtx, job, project, projectSlug, token, logW, logger)
}

// runRebuildPath executes the standard 5-phase pipeline: BUILD → HEALTH CHECK → SWAP → CLEANUP.
func (r *Runner) runRebuildPath(
	ctx context.Context,
	job *domain.DeployJob,
	project *db.ProjectRow,
	projectSlug, token string,
	logW io.Writer,
	logger *slog.Logger,
) error {
	// --- ENV VARS ---
	// Decrypt stored env vars and write them as a .env file in the workspace
	// root so that docker compose reads them automatically at build & up time.
	// Values are never logged; only a count of variables is emitted.
	if len(project.EnvVars) > 0 {
		plainEnv, err := crypto.Decrypt(r.masterKey, project.EnvVars)
		if err != nil {
			return r.fail(job, fmt.Errorf("decrypt env vars: %w", err))
		}
		envPath := filepath.Join(r.pipeline.WorkspaceDir(job), ".env")
		if err := os.WriteFile(envPath, plainEnv, 0600); err != nil {
			return r.fail(job, fmt.Errorf("write .env file: %w", err))
		}
		count := 0
		for _, line := range strings.Split(string(plainEnv), "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				count++
			}
		}
		logger.InfoContext(ctx, "injected env vars into workspace", "count", count)
		fmt.Fprintf(logW, "[env] wrote %d environment variable(s) to workspace .env\n", count)
	}

	// --- BUILD ---
	if err := r.queue.UpdateStatus(job.ID, domain.StatusBuilding); err != nil {
		cleanup(r, ctx, job, projectSlug, false, logW)
		return r.fail(job, err)
	}
	if err := r.pipeline.BuildStage(ctx, job, projectSlug, logW); err != nil {
		cleanup(r, ctx, job, projectSlug, false, logW)
		return r.fail(job, err)
	}

	// --- HEALTH CHECK ---
	if err := r.queue.UpdateStatus(job.ID, domain.StatusHealthCheck); err != nil {
		cleanup(r, ctx, job, projectSlug, false, logW)
		return r.fail(job, err)
	}
	mode, err := deploy.DetectMode(r.pipeline.WorkspaceDir(job), project.ComposePath)
	if err != nil {
		logger.WarnContext(ctx, "healthcheck detect failed, using mode=none", "err", err)
		mode = deploy.HealthMode{Mode: "none"}
	}
	if err := r.pipeline.HealthCheckStage(ctx, job, projectSlug, mode, logW); err != nil {
		cleanup(r, ctx, job, projectSlug, false, logW)
		return r.fail(job, err)
	}

	// --- SWAP ---
	if err := r.queue.UpdateStatus(job.ID, domain.StatusSwapping); err != nil {
		cleanup(r, ctx, job, projectSlug, false, logW)
		return r.fail(job, err)
	}
	if err := r.pipeline.SwapStage(ctx, job, projectSlug, logW); err != nil {
		cleanup(r, ctx, job, projectSlug, false, logW)
		return r.fail(job, err)
	}

	// --- CLEANUP (success path) ---
	cleanup(r, ctx, job, projectSlug, true, logW)
	if err := r.queue.MarkFinished(job.ID, domain.StatusSuccess, ""); err != nil {
		logger.ErrorContext(ctx, "failed to mark job success", "err", err)
	}

	go r.setCommitStatus(job, project.RepoFullName, token, domain.CommitStatusSuccess)
	logger.InfoContext(ctx, "job completed successfully")
	return nil
}

// runHotSwapPath executes the hot-swap pipeline: HOT_SWAP → CLEANUP.
// Skips BUILD, HEALTH CHECK, and SWAP stages entirely.
func (r *Runner) runHotSwapPath(
	ctx context.Context,
	job *domain.DeployJob,
	project *db.ProjectRow,
	projectSlug, token string,
	logW io.Writer,
	logger *slog.Logger,
) error {
	logger.InfoContext(ctx, "using hot-swap deploy mode")
	fmt.Fprintf(logW, "[pipeline] using hot-swap mode — skipping build/healthcheck/swap\n")

	// --- HOT SWAP ---
	if err := r.queue.UpdateStatus(job.ID, domain.StatusHotSwapping); err != nil {
		cleanup(r, ctx, job, projectSlug, false, logW)
		return r.fail(job, err)
	}
	if err := r.pipeline.HotSwapStage(ctx, job, projectSlug, logW); err != nil {
		cleanup(r, ctx, job, projectSlug, false, logW)
		return r.fail(job, err)
	}

	// --- CLEANUP (success path) ---
	cleanup(r, ctx, job, projectSlug, true, logW)
	if err := r.queue.MarkFinished(job.ID, domain.StatusSuccess, ""); err != nil {
		logger.ErrorContext(ctx, "failed to mark job success", "err", err)
	}

	go r.setCommitStatus(job, project.RepoFullName, token, domain.CommitStatusSuccess)
	logger.InfoContext(ctx, "hot-swap job completed successfully")
	return nil
}

func (r *Runner) fail(job *domain.DeployJob, err error) error {
	if merr := r.queue.MarkFinished(job.ID, domain.StatusFailed, err.Error()); merr != nil {
		r.log.Error("failed to mark job failed", "job_id", job.ID, "err", merr)
	}
	return err
}

func (r *Runner) getInstallationToken(ctx context.Context, installationID int64) (string, error) {
	appRow, err := r.db.GetGitHubApp()
	if err != nil {
		return "", fmt.Errorf("get github app: %w", err)
	}
	pemPlain, err := crypto.Decrypt(r.masterKey, appRow.PEM)
	if err != nil {
		return "", fmt.Errorf("decrypt PEM: %w", err)
	}
	app, err := github.NewApp(appRow.AppID, pemPlain)
	if err != nil {
		return "", fmt.Errorf("parse github app key: %w", err)
	}
	return app.GetInstallationToken(ctx, installationID)
}

func (r *Runner) setCommitStatus(job *domain.DeployJob, repoFullName, token, state string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	desc := "Deploy succeeded"
	if state != domain.CommitStatusSuccess {
		desc = "Deploy failed"
	}
	if err := github.SetCommitStatus(ctx, token, repoFullName, job.CommitSHA, state, desc); err != nil {
		r.log.Warn("failed to set commit status", "job_id", job.ID, "err", err)
	}
}

// cleanup runs CleanupStage on a background context with a 2-minute timeout so
// it always completes even if the job context was cancelled, but never hangs indefinitely.
func cleanup(r *Runner, _ context.Context, job *domain.DeployJob, projectSlug string, succeeded bool, logW io.Writer) {
	cleanCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	r.pipeline.CleanupStage(cleanCtx, job, projectSlug, succeeded, logW)
}

// safeSlug converts a project name into a Docker-safe lowercase slug.
func safeSlug(name string) string {
	slug := make([]byte, 0, len(name))
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z':
			slug = append(slug, c)
		case c >= 'A' && c <= 'Z':
			slug = append(slug, c+32) // toLower
		case c >= '0' && c <= '9':
			slug = append(slug, c)
		default:
			if len(slug) > 0 && slug[len(slug)-1] != '-' {
				slug = append(slug, '-')
			}
		}
	}
	// Trim trailing dash.
	for len(slug) > 0 && slug[len(slug)-1] == '-' {
		slug = slug[:len(slug)-1]
	}
	return string(slug)
}

func openJobLog(path string) (*os.File, error) {
	if path == "" {
		return os.Stderr, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("create log dir: %w", err)
	}
	return os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
}

// broadcastWriter wraps a file writer and also publishes each line to the SSE broadcaster.
type broadcastWriter struct {
	file        io.Writer
	broadcaster *sse.Broadcaster
	jobID       string
	buf         []byte
}

// newBroadcastWriter creates a writer that tees to the log file and SSE subscribers.
func (r *Runner) newBroadcastWriter(file io.Writer, jobID string) *broadcastWriter {
	return &broadcastWriter{
		file:        file,
		broadcaster: r.broadcaster,
		jobID:       jobID,
	}
}

func (w *broadcastWriter) Write(p []byte) (int, error) {
	n, err := w.file.Write(p)
	if err != nil {
		return n, err
	}
	// Buffer and broadcast complete lines.
	w.buf = append(w.buf, p[:n]...)
	for {
		idx := -1
		for i, b := range w.buf {
			if b == '\n' {
				idx = i
				break
			}
		}
		if idx < 0 {
			break
		}
		line := string(w.buf[:idx])
		w.buf = w.buf[idx+1:]
		if w.broadcaster != nil {
			w.broadcaster.Publish(sse.LogTopic(w.jobID), line)
		}
	}
	return n, nil
}

// ensure Runner implements the scheduler.JobRunner interface at compile time.
var _ interface {
	Run(ctx context.Context, job *domain.DeployJob) error
} = (*Runner)(nil)

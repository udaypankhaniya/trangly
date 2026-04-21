package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/udaypankhaniya/trangly/internal/config"
	"github.com/udaypankhaniya/trangly/internal/infra/system"
	"github.com/udaypankhaniya/trangly/pkg/version"
)

// App drives the full startup/shutdown lifecycle.
type App struct {
	cfg     config.Config
	baseURL string
}

// NewApp constructs an App. baseURL is the public URL used by the GitHub App manifest.
// May be empty; user can set it later via the UI.
func NewApp(cfg config.Config, baseURL string) *App {
	return &App{cfg: cfg, baseURL: baseURL}
}

// Run performs the full startup sequence and blocks until SIGTERM/SIGINT.
//
//  1. Preflight checks - log results; block on fatal failures
//  2. Construct container (DB migrate, wire services)
//  3. Warn if no admin account exists (run `Trangly setup` first)
//  4. Start scheduler
//  5. Start HTTP server
//  6. Wait for shutdown signal -> graceful drain
func (a *App) Run() error {
	logger := slog.Default().With("app", version.AppName)

	logger.Info("starting", "version", version.Version, "port", a.cfg.Port)

	// --- 1. Preflight checks ---
	ctx := context.Background()
	results := system.RunChecks(ctx)
	allOK := true
	for _, r := range results {
		switch r.Status {
		case "ok":
			logger.Info("preflight ok", "check", r.Name)
		case "warn":
			logger.Warn("preflight warn", "check", r.Name, "detail", r.Detail, "fix", r.Fix)
		case "fail":
			logger.Error("preflight failed", "check", r.Name, "detail", r.Detail, "fix", r.Fix)
			allOK = false
		}
	}
	if !allOK {
		return fmt.Errorf("preflight checks failed -- fix the errors above and restart %s", version.AppName)
	}

	// --- 2. Build container ---
	c, err := NewContainer(a.cfg, a.baseURL)
	if err != nil {
		return fmt.Errorf("startup: %w", err)
	}

	// --- 3. Warn if no admin exists ---
	if exists, _ := c.DB.UserExists(); !exists {
		logger.Warn("no admin account found -- visit http://localhost:" + fmt.Sprintf("%d", a.cfg.Port) + "/setup or run `" + version.AppBinary + " setup` to create one")
	}

	// --- 4. Graceful signal context ---
	shutdownCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Close ShutdownCh when the signal fires so background goroutines (rate limiters, etc.) stop.
	go func() {
		<-shutdownCtx.Done()
		close(c.ShutdownCh)
	}()

	// --- 5. Scheduler ---
	go func() {
		logger.Info("scheduler started")
		c.Scheduler.Run(shutdownCtx)
		logger.Info("scheduler stopped")
	}()

	// --- 6. HTTP server ---
	errC := make(chan error, 1)
	go func() {
		logger.Info("http server listening", "port", a.cfg.Port)
		errC <- c.HTTPServer.Start(shutdownCtx)
	}()

	select {
	case <-shutdownCtx.Done():
		logger.Info("shutdown signal received")
	case err := <-errC:
		if err != nil {
			return fmt.Errorf("http server: %w", err)
		}
	}

	// --- Graceful shutdown ---
	logger.Info("draining active jobs (up to 30s)...")
	drainCtx, cancel := context.WithTimeout(context.Background(), 30*1_000_000_000)
	defer cancel()
	c.Supervisor.Drain(drainCtx)

	if err := c.Docker.Close(); err != nil {
		logger.Warn("docker client close error", "err", err)
	}

	logger.Info("shutdown complete")
	return nil
}

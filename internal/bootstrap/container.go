// Package bootstrap wires all dependencies and manages the startup sequence.
// container.go — dependency construction
// app.go        — startup sequence (preflight → DB → scheduler → HTTP server)
package bootstrap

import (
	"fmt"
	"os"

	apihttp "github.com/udaypankhaniya/trangly/internal/api/http"
	"github.com/udaypankhaniya/trangly/internal/api/sse"
	"github.com/udaypankhaniya/trangly/internal/app"
	"github.com/udaypankhaniya/trangly/internal/config"
	"github.com/udaypankhaniya/trangly/internal/deploy"
	"github.com/udaypankhaniya/trangly/internal/engine"
	"github.com/udaypankhaniya/trangly/internal/infra/crypto"
	"github.com/udaypankhaniya/trangly/internal/infra/db"
	"github.com/udaypankhaniya/trangly/internal/infra/docker"
	githubapi "github.com/udaypankhaniya/trangly/internal/infra/github"
	"github.com/udaypankhaniya/trangly/internal/queue"
	"github.com/udaypankhaniya/trangly/internal/scheduler"
	"github.com/udaypankhaniya/trangly/internal/webhook"
)

// Container holds all constructed service instances.
// It is the single place where the entire dependency graph is visible.
type Container struct {
	Config         config.Config
	DB             *db.DB
	MasterKey      []byte
	ShutdownCh     chan struct{} // closed on shutdown to stop background goroutines
	AuthSvc        *app.AuthService
	ProjectSvc     *app.ProjectService
	DeploySvc      *app.DeployService
	GHAuthSvc      *app.GitHubAuthService
	GHWebhookSvc   *app.GitHubWebhookService
	QueueMgr       *queue.Manager
	Supervisor     *engine.Supervisor
	Runner         *engine.Runner
	Pipeline       *deploy.Pipeline
	Scheduler      *scheduler.Scheduler
	Broadcaster    *sse.Broadcaster
	WebhookHandler *webhook.Handler
	HTTPServer     *apihttp.Server
	Docker         *docker.Client
}

// NewContainer constructs all services in dependency order.
func NewContainer(cfg config.Config, baseURL string) (*Container, error) {
	c := &Container{Config: cfg, ShutdownCh: make(chan struct{})}

	// 1. Database
	database, err := db.New(cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: open database: %w", err)
	}
	c.DB = database

	// 2. Master key (AES-256)
	masterKey, err := loadOrCreateMasterKey(cfg.MasterKeyPath)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: master key: %w", err)
	}
	c.MasterKey = masterKey

	// 3. Auth service
	authSvc, err := app.NewAuthService(database, masterKey)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: auth service: %w", err)
	}
	c.AuthSvc = authSvc

	// 4. Project & deploy services
	c.ProjectSvc = app.NewProjectService(database, masterKey)
	c.DeploySvc = app.NewDeployService(database, cfg.LogsDir)

	// 5. GitHub services
	tokenCache := githubapi.NewTokenCache()
	c.GHAuthSvc = app.NewGitHubAuthService(database, masterKey, tokenCache)
	c.GHWebhookSvc = app.NewGitHubWebhookService(database, c.DeploySvc, masterKey)

	// 6. SSE broadcaster (created before queue so it can receive status events)
	c.Broadcaster = sse.NewBroadcaster()

	// 7. Queue (receives broadcaster for real-time SSE status events)
	qm, err := queue.NewManager(database, c.Broadcaster)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: queue manager: %w", err)
	}
	c.QueueMgr = qm

	// 8. Docker client
	dc, err := docker.New()
	if err != nil {
		return nil, fmt.Errorf("bootstrap: docker client: %w", err)
	}
	c.Docker = dc

	// 9. Pipeline + engine
	c.Pipeline = deploy.NewPipeline(dc, cfg.WorkspacesDir)
	c.Supervisor = engine.NewSupervisor()
	c.Runner = engine.NewRunner(c.Pipeline, qm, database, masterKey, c.Supervisor, c.Broadcaster)

	// 10. Scheduler
	mon := scheduler.NewMonitor(database)
	sched, err := scheduler.NewScheduler(database, qm, mon, c.Runner, c.Supervisor)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: scheduler: %w", err)
	}
	c.Scheduler = sched

	// 11. Webhook handler
	c.WebhookHandler = webhook.NewHandler(c.GHWebhookSvc)

	// 12. HTTP server
	c.HTTPServer = apihttp.NewServer(apihttp.Config{
		Port:           cfg.Port,
		BaseURL:        baseURL,
		ShutdownCh:     c.ShutdownCh,
		DB:             database,
		Docker:         dc,
		AuthSvc:        authSvc,
		ProjectSvc:     c.ProjectSvc,
		DeploySvc:      c.DeploySvc,
		GHAuthSvc:      c.GHAuthSvc,
		GHWebhookSvc:   c.GHWebhookSvc,
		QueueMgr:       qm,
		Broadcaster:    c.Broadcaster,
		WebhookHandler: c.WebhookHandler,
	})

	return c, nil
}

// loadOrCreateMasterKey reads the master key file, creating a new one on first run.
// The key file is chmod 600 and must never be logged.
func loadOrCreateMasterKey(path string) ([]byte, error) {
	data, err := os.ReadFile(path) //nolint:gosec
	if err == nil && len(data) == 32 {
		return data, nil
	}

	// Generate a new 32-byte random key.
	key, err := crypto.RandomBytes(32)
	if err != nil {
		return nil, fmt.Errorf("generate master key: %w", err)
	}
	if err := os.WriteFile(path, key, 0600); err != nil {
		return nil, fmt.Errorf("write master key: %w", err)
	}
	return key, nil
}

// LoadMasterKey reads an existing master key from disk.
// Returns an error if the key file does not exist or is invalid.
func LoadMasterKey(path string) ([]byte, error) {
	data, err := os.ReadFile(path) //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("read master key: %w", err)
	}
	if len(data) != 32 {
		return nil, fmt.Errorf("master key file has invalid length %d (expected 32)", len(data))
	}
	return data, nil
}

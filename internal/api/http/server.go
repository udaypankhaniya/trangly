// Package http provides the HTTP server for the Trangly API.
// The server uses Fiber v2 as the HTTP framework.
package http

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/compress"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/fiber/v2/middleware/requestid"
	"github.com/gofiber/websocket/v2"

	"github.com/udaypankhaniya/trangly/internal/api/http/handlers"
	"github.com/udaypankhaniya/trangly/internal/api/http/middleware"
	"github.com/udaypankhaniya/trangly/internal/api/sse"
	"github.com/udaypankhaniya/trangly/internal/app"
	"github.com/udaypankhaniya/trangly/internal/infra/db"
	"github.com/udaypankhaniya/trangly/internal/infra/docker"
	"github.com/udaypankhaniya/trangly/internal/queue"
	"github.com/udaypankhaniya/trangly/internal/ui"
	"github.com/udaypankhaniya/trangly/internal/webhook"
)

// Server is the Trangly HTTP server backed by Fiber v2.
type Server struct {
	app  *fiber.App
	port int
	log  *slog.Logger
}

// Config holds dependencies needed to wire the HTTP server.
type Config struct {
	Port    int
	BaseURL string
	// ShutdownCh is closed when the server is shutting down.
	// Used to stop background goroutines such as rate limiter cleanup.
	ShutdownCh <-chan struct{}
	// TrustProxy controls whether X-Forwarded-For is trusted for IP extraction in the
	// rate limiter. Set true only when running behind a known trusted reverse proxy.
	TrustProxy     bool
	DB             *db.DB
	Docker         *docker.Client
	AuthSvc        *app.AuthService
	ProjectSvc     *app.ProjectService
	DeploySvc      *app.DeployService
	GHAuthSvc      *app.GitHubAuthService
	GHWebhookSvc   *app.GitHubWebhookService
	QueueMgr       *queue.Manager
	Broadcaster    *sse.Broadcaster
	WebhookHandler *webhook.Handler
}

// NewServer creates and configures the HTTP server without starting it.
func NewServer(cfg Config) *Server {
	s := &Server{
		port: cfg.Port,
		log:  slog.Default().With("component", "http_server"),
	}

	fiberApp := fiber.New(fiber.Config{
		// ReadTimeout: 0 is required for SSE streams — no per-connection read deadline.
		// Non-SSE route timeouts are enforced at the handler level via context.WithTimeout.
		ReadTimeout:  0,
		WriteTimeout: 0,
		IdleTimeout:  120 * time.Second,
		// Match previous 10 MB limit from io.LimitReader in webhook handler.
		BodyLimit:                10 * 1024 * 1024,
		DisableStartupMessage:    true,
		StreamRequestBody:        true,
		DisableDefaultDate:       true,
		DisableHeaderNormalizing: false,
		// Centralized JSON error handler for unhandled errors.
		// Framework-level fiber.Error messages (e.g. "Not Found") are passed through;
		// all other internal errors are collapsed to avoid leaking Go internals.
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			code := fiber.StatusInternalServerError
			msg := "internal server error"
			if e, ok := err.(*fiber.Error); ok {
				code = e.Code
				msg = e.Message
			}
			return c.Status(code).JSON(fiber.Map{"error": msg})
		},
	})
	s.app = fiberApp

	// --- Global middleware ---
	fiberApp.Use(recover.New())
	fiberApp.Use(requestid.New())
	// Compress all responses except SSE streams. SSE requires chunked streaming;
	// compression would buffer the output and break event delivery.
	fiberApp.Use(compress.New(compress.Config{
		Level: compress.LevelBestSpeed,
		Next: func(c *fiber.Ctx) bool {
			// Skip compression for SSE routes (text/event-stream).
			p := c.Path()
			return p == "/api/events" || strings.HasPrefix(p, "/api/deployments/") || strings.HasPrefix(p, "/api/containers/")
		},
	}))
	fiberApp.Use(middleware.Security())
	fiberApp.Use(cors.New(cors.Config{
		// Restrict cross-origin requests to the same origin as the configured base URL.
		// This prevents third-party sites from calling the API on behalf of a logged-in user.
		AllowOrigins:     cfg.BaseURL,
		AllowMethods:     "GET,POST,PUT,DELETE,OPTIONS",
		AllowHeaders:     "Authorization,Content-Type,X-Request-ID",
		AllowCredentials: false,
	}))
	fiberApp.Use(middleware.Logger())

	// --- Handlers ---
	sysh := handlers.NewSystemHandler(cfg.DB, cfg.AuthSvc, cfg.Docker)
	authh := handlers.NewAuthHandler(cfg.AuthSvc)
	ghh := handlers.NewGitHubHandler(cfg.GHAuthSvc, cfg.BaseURL)
	prjh := handlers.NewProjectHandler(cfg.ProjectSvc, cfg.DeploySvc, cfg.GHAuthSvc)
	deph := handlers.NewDeployHandler(cfg.DeploySvc, cfg.ProjectSvc, cfg.Broadcaster)
	queueh := handlers.NewQueueHandler(cfg.DeploySvc, cfg.QueueMgr)
	eventh := handlers.NewEventsHandler(cfg.DeploySvc, cfg.Broadcaster)
	termh := handlers.NewTerminalHandler(cfg.Docker)

	// --- Public routes (no auth) ---
	fiberApp.Get("/api/version", sysh.Version)
	fiberApp.Get("/api/system/preflight", sysh.Preflight)
	fiberApp.Get("/api/setup/status", sysh.SetupStatus)
	fiberApp.Post("/api/setup", sysh.Setup)
	// Login is rate-limited to 5 attempts/min per IP to block brute-force attacks.
	fiberApp.Post("/api/auth/login", middleware.RateLimit(5, time.Minute, cfg.TrustProxy, cfg.ShutdownCh), authh.Login)
	fiberApp.Get("/api/github/callback", ghh.Callback)

	// --- Webhook (rate-limited, not JWT-authenticated — has own HMAC check) ---
	fiberApp.Post("/webhooks/github", middleware.RateLimit(60, time.Minute, cfg.TrustProxy, cfg.ShutdownCh), cfg.WebhookHandler.FiberHandler())

	// --- Authenticated routes ---
	// Auth middleware is applied inline per-route. Do NOT use Group("") with auth
	// middleware — empty-prefix groups in Fiber v2 leak into subsequent routes
	// (including UI catch-all), causing everything to return 401.
	authMW := middleware.Auth(cfg.AuthSvc)

	fiberApp.Get("/api/system/ram", authMW, sysh.RAM)
	fiberApp.Get("/api/system/stats", authMW, sysh.Stats)

	fiberApp.Get("/api/auth/me", authMW, authh.GetProfile)
	fiberApp.Put("/api/auth/profile", authMW, authh.UpdateProfile)
	fiberApp.Post("/api/auth/change-password", authMW, authh.ChangePassword)

	fiberApp.Get("/api/github/manifest", authMW, ghh.Manifest)
	fiberApp.Get("/api/github/status", authMW, ghh.Status)
	fiberApp.Get("/api/github/repos", authMW, ghh.Repos)
	fiberApp.Get("/api/github/install-url", authMW, ghh.InstallURL)
	fiberApp.Delete("/api/github/app", authMW, ghh.Disconnect)

	fiberApp.Get("/api/github/repos/:owner/:repo/branches", authMW, ghh.Branches)
	fiberApp.Get("/api/github/repos/:owner/:repo/compose-files", authMW, ghh.ComposeFiles)

	fiberApp.Post("/api/repos/:owner/:repo/status", authMW, ghh.SetCommitStatus)

	fiberApp.Get("/api/projects", authMW, prjh.List)
	fiberApp.Get("/api/projects/:id", authMW, prjh.Get)
	fiberApp.Get("/api/projects/:id/env", authMW, prjh.GetEnv)
	fiberApp.Post("/api/projects", authMW, prjh.Create)
	fiberApp.Put("/api/projects/:id", authMW, prjh.Update)
	fiberApp.Delete("/api/projects/:id", authMW, prjh.Delete)

	fiberApp.Post("/api/projects/:id/deploy", authMW, deph.TriggerDeploy)
	fiberApp.Get("/api/deployments", authMW, deph.ListDeployments)
	fiberApp.Get("/api/deployments/:id/logs", authMW, deph.StreamLogs)
	fiberApp.Get("/api/deployments/:id", authMW, deph.GetDeployment)

	fiberApp.Get("/api/queue", authMW, queueh.List)
	fiberApp.Delete("/api/queue/:job_id", authMW, queueh.Cancel)

	// --- SSE dashboard events ---
	fiberApp.Get("/api/events", authMW, eventh.Stream)

	// --- Terminal (container exec via WebSocket) ---
	fiberApp.Get("/api/containers", authMW, termh.ListContainers)
	fiberApp.Get("/api/containers/:id/terminal", authMW, websocket.New(termh.ExecTerminal))

	// --- Embedded UI (catch-all, must be last) ---
	ui.RegisterRoutes(fiberApp)

	return s
}

// Start begins listening. Blocks until the context is cancelled or the server errors.
func (s *Server) Start(ctx context.Context) error {
	addr := fmt.Sprintf(":%d", s.port)
	s.log.Info("HTTP server starting", "addr", addr)

	errC := make(chan error, 1)
	go func() {
		if err := s.app.Listen(addr); err != nil {
			errC <- fmt.Errorf("http server: %w", err)
		}
		close(errC)
	}()

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.app.ShutdownWithContext(shutCtx); err != nil {
			s.log.Warn("HTTP shutdown error", "err", err)
		}
		return nil
	case err := <-errC:
		return err
	}
}

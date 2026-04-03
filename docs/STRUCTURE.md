# Trangly — Project Structure & Architecture

This document defines the **mandatory folder structure and architectural boundaries** for Trangly.

This is not optional. Any deviation will introduce coupling, break concurrency guarantees, or make the deployment engine unreliable.

---

# 1. High-Level Architecture

Trangly is split into two strict layers:

### Control Plane
* HTTP API
* Web UI
* Auth
* Project configuration

### Execution Plane
* Deployment pipeline
* Queue system
* Scheduler
* Workers

**Rule:**
Control Plane MUST NEVER directly execute deployment logic.

All execution flows through:

```
API → Queue → Scheduler → Engine → Pipeline
```

---

# 2. Directory Structure

```
Trangly/
├── cmd/
│   └── Trangly/
│       └── main.go

├── pkg/
│   └── version/
│       └── version.go          ← App constants (name, version, description, build info)

├── internal/

│   # --- DOMAIN (pure business logic, no external deps)
│   ├── domain/
│   │   ├── project.go
│   │   ├── deployment.go
│   │   ├── job.go
│   │   ├── queue.go
│   │   └── enums.go

│   # --- APPLICATION SERVICES (use-case orchestration)
│   ├── app/
│   │   ├── project_service.go
│   │   ├── deploy_service.go
│   │   ├── github_auth_service.go    ← magic link, manifest, token exchange
│   │   ├── github_webhook_service.go ← event routing, job creation
│   │   ├── auth_service.go
│   │   ├── helpers.go
│   │   └── doc.go

│   # --- INFRASTRUCTURE (external systems)
│   ├── infra/
│   │   ├── db/
│   │   ├── docker/
│   │   ├── git/
│   │   ├── github/
│   │   ├── crypto/
│   │   ├── github/
│   │   │   ├── auth.go
│   │   │   ├── client.go
│   │   │   ├── install.go
│   │   │   ├── retry.go          ← HTTP retry with exponential backoff
│   │   │   └── token_cache.go    ← installation token cache (avoids redundant API calls)
│   │   └── system/
│   │       ├── preflight.go      ← OS, Docker, socket, port, reachability checks
│   │       ├── memory.go         ← /proc/meminfo reader (Linux)
│   │       ├── memory_windows.go ← Windows memory reader
│   │       ├── memory_types.go   ← MemInfo struct shared across platforms
│   │       ├── cpu.go            ← /proc/cpuinfo reader (Linux)
│   │       ├── cpu_windows.go    ← Windows CPU info reader
│   │       └── distro.go         ← /etc/os-release parser

│   # --- DEPLOYMENT PIPELINE (core engine)
│   ├── deploy/
│   │   ├── pipeline.go
│   │   ├── fetch.go
│   │   ├── build.go
│   │   ├── healthcheck.go
│   │   ├── healthcheck_detect.go  ← auto-detect TCP vs HTTP vs none from Compose
│   │   ├── swap.go               ← full rebuild swap (compose down + up)
│   │   ├── hotswap.go            ← hot-swap deploy (file copy, no rebuild)
│   │   └── cleanup.go

│   # --- QUEUE SYSTEM (state + execution control)
│   ├── queue/
│   │   ├── manager.go
│   │   ├── worker.go
│   │   └── state_machine.go

│   # --- SCHEDULER (RAM-aware execution control)
│   ├── scheduler/
│   │   ├── scheduler.go        ← 5s loop, job pickup decision
│   │   ├── memory.go           ← docker stats sampler, adaptive estimate calculator
│   │   └── monitor.go          ← hold escalation, 30-min timeout enforcement

│   # --- ENGINE (runtime execution layer)
│   ├── engine/
│   │   ├── runner.go           ← runs one job end-to-end, calls pipeline stages
│   │   ├── supervisor.go       ← goroutine lifecycle: spawn, track, cancel
│   │   └── context.go          ← cancellation + timeout propagation only

│   # --- API (thin layer only)
│   ├── api/
│   │   ├── http/
│   │   │   ├── server.go
│   │   │   ├── middleware/
│   │   │   │   ├── auth.go        ← JWT verification middleware
│   │   │   │   ├── logger.go      ← structured request logging
│   │   │   │   ├── ratelimit.go   ← token bucket rate limiter
│   │   │   │   └── security.go    ← security headers (CSP, X-Frame-Options, etc.)
│   │   │   └── handlers/
│   │   │       ├── auth.go
│   │   │       ├── deploy.go
│   │   │       ├── events.go
│   │   │       ├── github.go
│   │   │       ├── helpers.go     ← shared response helpers (respondJSON, respondError)
│   │   │       ├── project.go
│   │   │       ├── queue.go
│   │   │       └── system.go
│   │   └── sse/
│   │       └── stream.go

│   # --- WEBHOOK HANDLERS
│   ├── webhook/
│   │   ├── github.go
│   │   └── handler.go

│   # --- UI (embedded frontend)
│   ├── ui/
│   │   ├── ui.go             ← route registration + template rendering
│   │   ├── render.go         ← template renderer construction
│   │   └── static/

│   # --- CONFIGURATION
│   ├── config/
│   │   └── config.go

│   # --- BOOTSTRAP / DEPENDENCY WIRING
│   └── bootstrap/
│       ├── app.go              ← startup sequence: preflight → DB → scheduler → HTTP
│       ├── container.go        ← dependency wiring only
│       └── setup.go            ← initial admin setup wizard

├── scripts/
├── Makefile
└── README.md
```

---

# 3. App Constants — `pkg/version`

All app-wide constants live in one place. No other package hardcodes the app name, version, or description. This package is imported by any layer that needs to identify the application — API responses, log output, the UI, the install script, the systemd service description.

```go
// pkg/version/version.go

package version

// Build-time variables — injected via ldflags:
//   go build -ldflags "-X github.com/udaypankhaniya/trangly/pkg/version.Version=1.0.0 \
//                       -X github.com/udaypankhaniya/trangly/pkg/version.Commit=abc1234 \
//                       -X github.com/udaypankhaniya/trangly/pkg/version.BuildDate=2025-01-01"

var (
    Version   = "dev"              // semantic version e.g. "1.0.0"
    Commit    = "unknown"          // short git SHA e.g. "abc1234"
    BuildDate = "unknown"          // ISO date e.g. "2025-01-01"
)

const (
    AppName        = "Trangly"
    AppSlug        = "Trangly"
    AppDescription = "Lightweight self-hosted CI/CD for Docker projects"
    AppURL         = "https://trangly.dev"
    AppPort        = 2880
    AppDataDir     = "~/.trangly"
    AppBinary      = "Trangly"
)

// Info returns a structured map of all version metadata.
// Used by the API /api/version endpoint and startup log line.
func Info() map[string]string {
    return map[string]string{
        "name":        AppName,
        "version":     Version,
        "commit":      Commit,
        "build_date":  BuildDate,
        "description": AppDescription,
        "url":         AppURL,
    }
}

// String returns the single-line version string shown in CLI and logs.
// Example: "Trangly v1.0.0 (commit abc1234, built 2025-01-01)"
func String() string {
    return AppName + " v" + Version +
        " (commit " + Commit + ", built " + BuildDate + ")"
}
```

**Usage across the codebase:**

```go
// cmd/trangly/main.go
import "github.com/udaypankhaniya/trangly/pkg/version"
log.Info(version.String())

// internal/api/http/handlers/system.go
import "github.com/udaypankhaniya/trangly/pkg/version"
c.JSON(200, version.Info())

// internal/bootstrap/app.go
import "github.com/udaypankhaniya/trangly/pkg/version"
fmt.Printf("Starting %s on port %d\n", version.AppName, version.AppPort)

// internal/infra/system/preflight.go
import "github.com/udaypankhaniya/trangly/pkg/version"
checkPort(version.AppPort)
```

**Makefile injection:**

```makefile
VERSION  := $(shell git describe --tags --always --dirty)
COMMIT   := $(shell git rev-parse --short HEAD)
DATE     := $(shell date -u +%Y-%m-%d)
LDFLAGS  := -X github.com/udaypankhaniya/trangly/pkg/version.Version=$(VERSION) \
             -X github.com/udaypankhaniya/trangly/pkg/version.Commit=$(COMMIT) \
             -X github.com/udaypankhaniya/trangly/pkg/version.BuildDate=$(DATE)

build:
    go build -ldflags "$(LDFLAGS)" -o bin/trangly ./cmd/trangly
```

---

# 4. Layer Responsibilities

## 4.1 Domain (`internal/domain`)

* Pure business models
* No imports from infra, API, or external libs

**Rule:**
This layer must compile independently.

---

## 4.2 App Layer (`internal/app`)

* Orchestrates use cases
* Calls domain + infra

Split into focused services — no God objects:

* `project_service.go` — create, update, delete projects
* `deploy_service.go` — trigger deploys, query history
* `github_auth_service.go` — magic link flow, manifest generation, token exchange, credential storage
* `github_webhook_service.go` — signature validation, event parsing, job insertion into queue
* `auth_service.go` — admin login, session management

**Rule:**
Each service file owns exactly one domain concern. If a file exceeds ~200 lines, split it.

---

## 4.3 Infra Layer (`internal/infra`)

* Wraps ALL external dependencies

Includes:

* `docker/` — Docker SDK: build, run, stop, stats, prune
* `db/` — SQLite: all queries via sqlc-generated code
* `git/` — clone, checkout at SHA, clean workspace
* `github/` — GitHub API calls: manifest conversion, installation token, commit status
* `crypto/` — AES-256-GCM encrypt/decrypt, Argon2id key derivation
* `system/` — preflight checks, `/proc/meminfo` reader, `/etc/os-release` parser

**Rule:**
No external library should leak outside this layer.

---

## 4.4 Deploy Pipeline (`internal/deploy`)

Core system logic. Each stage is isolated and independently testable.

Trangly supports **two pipeline modes** — selected per-project at creation time:

### Rebuild Mode (`DeployModeRebuild`)

Full container rebuild — the default for most projects:

```
FETCH → BUILD → HEALTH CHECK → SWAP → CLEANUP
```

### Hot-Swap Mode (`DeployModeHotSwap`)

File-copy-based deployment — skips build/healthcheck, copies updated files into the running container:

```
FETCH → HOT_SWAP → CLEANUP
```

Implemented in `hotswap.go`. Useful for interpreted-language projects where a full rebuild is unnecessary.

### Additional files

* `healthcheck_detect.go` — inspects Docker Compose file at deploy time to determine health check mode:
  1. If `healthcheck` defined in compose → use as-is
  2. If port exposed, no healthcheck → TCP probe (auto)
  3. If no ports → mode: none, emit warning

Each stage must:
* Accept `context.Context` for cancellation
* Return an explicit typed error
* Write structured log lines to the job's log stream
* Be independently testable with mocked infra

---

## 4.5 Queue System (`internal/queue`)

Responsible for:
* Per-project job queues (not a single global queue)
* Job lifecycle and state transitions
* SQLite persistence — queue survives restarts

States (rebuild mode):

```
pending → held → running → building → health_check → swapping → success | failed
```

States (hot-swap mode):

```
pending → held → running → hot_swapping → success | failed
```

**Rule:**
State transitions MUST be validated centrally in `state_machine.go`. No ad-hoc status string updates elsewhere.

---

## 4.6 Scheduler (`internal/scheduler`)

Responsible for:
* RAM-aware job execution decisions (5s loop)
* Concurrency control across per-project queues
* Hold/resume logic based on `/proc/meminfo` MemAvailable
* Hold escalation: warn at 10min, auto-fail at 30min

Files:
* `scheduler.go` — main loop, job pickup decision, RAM math
* `memory.go` — docker stats sampler (2s interval during build), adaptive estimate calculator (avg of last 3 peak RSS × 1.20)
* `monitor.go` — hold escalation enforcement, impossible job detection (estimate > available_ram → fail immediately)

Must NEVER:
* Execute jobs directly — delegates to engine
* Call Docker API directly — reads stats via `infra/docker`

---

## 4.7 Engine (`internal/engine`)

Responsible for:
* Running one job end-to-end
* Managing goroutine lifecycle
* Context cancellation and timeout propagation

Files:
* `runner.go` — executes the pipeline for one job. Has two code paths: `runBuildPath()` (rebuild mode: fetch→build→healthcheck→swap→cleanup) and `runHotSwapPath()` (hot-swap mode: fetch→hotswap→cleanup). Updates job status at each phase boundary.
* `supervisor.go` — goroutine lifecycle only: spawn worker goroutines, track active goroutines, cancel on shutdown signal. No pipeline logic here.
* `context.go` — cancellation and timeout propagation only. Wraps `context.WithTimeout` with job-specific timeout from project config. No business logic.

**Critical boundary:**
`supervisor.go` owns goroutine lifecycle. `context.go` owns deadline/cancellation. These must not bleed into each other. `runner.go` is the only file that calls pipeline stages.

---

## 4.8 API Layer (`internal/api`)

Responsibilities:
* HTTP endpoints — thin, no business logic
* Input validation
* Auth middleware (JWT verification)
* SSE streaming (`api/sse/stream.go`)

Must NOT:
* Execute business logic directly
* Interact with Docker
* Modify queue state directly
* Know anything about the pipeline

---

## 4.9 Webhooks (`internal/webhook`)

Responsibilities:
* Validate `X-Hub-Signature-256` on every request
* Parse GitHub event payloads
* Convert events → internal job structs
* Delegate to `app/github_webhook_service.go` for job insertion

---

## 4.10 Bootstrap (`internal/bootstrap`)

Single place for:
* Dependency wiring — construct all services with their dependencies
* Service initialization order
* Starting scheduler loop
* Starting HTTP server
* Starting SSE broadcaster

`container.go` — dependency injection: constructs all service instances
`app.go` — startup sequence: preflight → migrate DB → start scheduler → start HTTP server

---

# 5. Concurrency Rules (Critical)

* Every goroutine MUST be cancellable via `context.Context`
* No unbounded goroutines — supervisor tracks all goroutines
* Queue controls execution — not API, not webhook handler
* Scheduler is the ONLY component allowed to start jobs
* `docker stats` sampling runs in its own goroutine, cancelled when job ends
* SSE log streaming runs in its own goroutine per connected client, cancelled on disconnect

---

# 6. Dependency Rules

Allowed direction:

```
api       → app → domain
                → infra

webhook   → app

deploy    → domain + infra
queue     → domain
scheduler → queue + infra/system + infra/docker (stats only)
engine    → deploy + queue

bootstrap → everything (wiring only)
pkg/version → nothing (no imports from internal/)
```

Forbidden:

```
api       → deploy        (never bypass queue)
api       → docker        (never direct Docker access)
api       → queue         (only via app layer)
queue     → api
infra     → domain
scheduler → deploy        (delegates to engine only)
engine    → scheduler     (no circular)
domain    → anything      (domain is the root)
```

---

# 7. Logging & Observability

* All logs MUST be structured (key=value pairs, JSON in production)
* Use `pkg/version.AppName` as the logger name — never hardcode "Trangly" in log output
* Deployment logs streamed via SSE — one line per log entry, newline-delimited
* Each job has its own log file at `~/.trangly/logs/{job_id}.log`
* Startup log line: `version.String()` — printed exactly once at process start

---

# 8. Testing Strategy

Must support:

* Unit tests for domain — pure functions, no mocks needed
* Unit tests for each pipeline stage with mocked infra interfaces
* Unit tests for state machine — all valid and invalid transitions
* Unit tests for scheduler RAM math — deterministic with fake `/proc/meminfo` values
* Integration tests for deploy pipeline — real Docker, isolated test containers
* Integration tests for magic link flow — mocked GitHub API responses

Test file placement:

```
internal/deploy/fetch_test.go
internal/queue/state_machine_test.go
internal/scheduler/scheduler_test.go
pkg/version/version_test.go
```

---

# 9. Non-Negotiable Rules

* No global state
* No circular dependencies
* No business logic inside HTTP handlers
* No direct Docker/GitHub calls outside `internal/infra`
* No skipping queue/scheduler for execution
* No hardcoded app name, version, or port — always use `pkg/version`
* No state string literals outside `internal/domain/enums.go`
* No ad-hoc `context.Background()` in business logic — always propagate the parent context

---

# 10. Entry Point

`cmd/trangly/main.go`

Responsibilities:
* Parse flags / env
* Load config
* Print version string
* Initialize bootstrap

```go
package main

import (
    "log"
    "github.com/udaypankhaniya/trangly/internal/bootstrap"
    "github.com/udaypankhaniya/trangly/pkg/version"
)

func main() {
    log.Println(version.String())
    app := bootstrap.NewApp()
    app.Run()
}
```

---

# 11. Philosophy

Trangly is:
* **Deterministic** — given the same input, the same pipeline runs every time
* **Event-driven** — webhook arrives → job created → scheduler picks up → engine runs
* **Resource-aware** — no job starts without RAM validation

Every design decision must preserve:
* Isolation between projects
* Reliability of deployments
* Predictable resource usage
* A codebase a new contributor can understand in one sitting

---

**If a change violates these principles, it should not be merged.**

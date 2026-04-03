---
applyTo: "internal/deploy/**,internal/queue/**,internal/scheduler/**,internal/engine/**"
---

# Deploy Pipeline, Queue, Scheduler & Engine Rules

## Pipeline Modes

Execution path: `API → Queue → Scheduler → Engine(runner.go) → Pipeline stages`

Trangly supports **two pipeline modes**, selected per-project via `DeployModeRebuild` or `DeployModeHotSwap` (constants in `domain/enums.go`).

### Rebuild Mode (default — 5 phases)

Old prod container **stays running and untouched** through phases 1–3. Only SWAP (phase 4) touches production.

| Phase | Status value | File | What happens |
|---|---|---|---|
| 1. FETCH | `running` | `deploy/fetch.go` | `git clone --depth=1` into `version.AppDataDir/workspaces/{project_id}/{job_id}/` |
| 2. BUILD | `building` | `deploy/build.go` | `docker compose build` via `exec.CommandContext` + `cmd.Cancel` |
| 3. HEALTH CHECK | `health_check` | `deploy/healthcheck.go` | Validate compose config; auto-detect mode; native HC post-swap |
| 4. SWAP | `swapping` | `deploy/swap.go` | `compose down --timeout 30` + `compose up -d --timeout 120`; `cmd.Cancel = kill` |
| 5. CLEANUP | → `success` | `deploy/cleanup.go` | Remove workspace dir; prune dangling images; update DB; emit SSE |

### Hot-Swap Mode (3 phases)

Skips build and health check — copies updated files into the running container:

| Phase | Status value | File | What happens |
|---|---|---|---|
| 1. FETCH | `running` | `deploy/fetch.go` | `git clone --depth=1`; resolve HEAD if needed |
| 2. HOT_SWAP | `hot_swapping` | `deploy/hotswap.go` | File-copy deploy into running container |
| 3. CLEANUP | → `success` | `deploy/cleanup.go` | Remove workspace; update job SUCCESS; emit SSE |

**Failure rule per phase:**
- FETCH / BUILD / HEALTH CHECK failure → mark `failed`, stop. Old prod untouched.
- SWAP failure → mark `failed`, trigger emergency rollback to previous image tag, show UI warning.
- HOT_SWAP failure → mark `failed`, stop.
- Never silently swallow errors — return them up the call stack.

**Stuck job monitor (in `scheduler/monitor.go`):** Jobs stuck in running/building/health_check/swapping/hot_swapping > 10 min → auto-fail.

**Each stage function must:**
- Accept `context.Context` as first argument (for cancellation)
- Return an explicit typed error
- Write structured log lines to the job’s log stream
- Be independently testable with mocked infra interfaces

## Job State Machine

All state transitions **must** go through `internal/queue/state_machine.go`. No direct status writes anywhere else.

Valid transitions (rebuild mode):
```
pending → held
pending → running
held    → running
running → building
building → health_check
health_check → swapping
swapping → success
(any state) → failed
```

Valid transitions (hot-swap mode):
```
pending → held
pending → running
held    → running
running → hot_swapping
hot_swapping → success
(any state) → failed
```

State constants live **only** in `internal/domain/enums.go` — never raw string literals:
```go
// internal/domain/enums.go
const (
    StatusPending      = "pending"
    StatusHeld         = "held"
    StatusRunning      = "running"
    StatusBuilding     = "building"
    StatusHealthCheck  = "health_check"
    StatusSwapping     = "swapping"
    StatusHotSwapping  = "hot_swapping"
    StatusSuccess      = "success"
    StatusFailed       = "failed"
)
```

## Docker — SDK + Compose CLI (via `internal/infra/docker/`)

- **Image/container operations** (prune, inspect, stats, create, remove) use the Docker SDK via `internal/infra/docker/`.
- **`docker compose` operations** (build, up, down, config) use `exec.CommandContext` with `cmd.Cancel = process.Kill` for reliable timeout/cancellation. The Docker SDK has no `docker compose` equivalent.
- `deploy/` and `scheduler/` call `infra/docker/` for SDK operations — neither touches the SDK directly.
- Pass `context.Context` to every Docker operation so builds can be cancelled.
- `docker stats` sampling runs in `scheduler/memory.go` only — not in deploy stages.
- All compose commands set `COMPOSE_PROJECT_NAME={project_slug}` via env.

## RAM-Aware Scheduler (`internal/scheduler/`)

### Constants (never magic numbers — spread across scheduler files)

`scheduler.go`:
```go
const (
    schedulerInterval = 5 * time.Second
    tranglyResvMB     = 50
    safetyBufferPct   = 0.2
)
```

`memory.go`:
```go
const statsSampleInterval = 2 * time.Second
```

`monitor.go`:
```go
const (
    holdWarnDuration     = 10 * time.Minute
    holdAutoFailDuration = 30 * time.Minute
    stuckJobDuration     = 10 * time.Minute
)
```

### File responsibilities within `internal/scheduler/`

- `scheduler.go` — main 5s loop; job pickup decision; RAM budget math; **only** component allowed to start jobs (via engine, never directly)
- `memory.go` — `docker stats` sampler (runs in its own goroutine per job)
- `monitor.go` — hold escalation enforcement; impossible job detection (`estimate > available_ram` → fail immediately); stuck job timeout enforcement

### Budget (computed once at startup in `scheduler.go`)

```
safety_buffer   = total_ram_mb × SafetyBufferPct
available_ram   = total_ram_mb - safety_buffer - TranglyReservedMB
```

RAM info read via `internal/infra/system/memory.go` — scheduler never reads `/proc/meminfo` directly.

### Per-job estimate (`scheduler/memory.go`)

```
// Bootstrap (first deploy — no history)
estimate = project.MemLimitMB + BootstrapOverheadMB
if project.MemLimitMB == 0 → estimate = DefaultEstimateMB
estimate_source = "bootstrap"

// Adaptive (history available — uses runtime RSS, NOT build peak RSS)
estimate = avg(last N runtime_rss_mb values) × AdaptiveHeadroom
estimate_source = "adaptive"
```

### Scheduler decision (every SchedulerInterval — `scheduler.go`)

```
effective = min(current_free_ram_mb, available_ram - sum(running_job.ram_estimate_mb))
if job.ram_estimate_mb > available_ram → FAIL immediately (never hold)
if effective >= job.ram_estimate_mb   → delegate to engine.runner (never execute directly)
else                                  → set status=held, hold_reason='insufficient_memory'
```

### Hold escalation (`scheduler/monitor.go`)

```
held_duration > holdWarnDuration     → emit slog.Warn, set WARNING banner via SSE
held_duration > holdAutoFailDuration → mark FAILED, error = "Job cancelled after 30min memory hold."
```

## Queue Architecture (`internal/queue/`)

- One FIFO queue **per project** — Project A's slow build never blocks Project B.
- `manager.go` — per-project queue management, SQLite persistence (queue survives restarts)
- `state_machine.go` — centralizes ALL state transitions; no ad-hoc status string updates elsewhere
- `worker.go` — job worker abstraction
- Queue processor is a single background goroutine; it never blocks the HTTP server.
- stdout + stderr captured line by line, written to `version.AppDataDir/logs/{deploy_id}.log` and broadcast via SSE simultaneously.
- Build timeout is configurable per project (default: 10 minutes). On timeout → mark `failed`, free goroutine.

## Engine Layer (`internal/engine/`) — Critical Boundaries

- `runner.go` — executes one job end-to-end; has two code paths: `runBuildPath()` (rebuild: fetch→build→healthcheck→swap→cleanup) and `runHotSwapPath()` (hot-swap: fetch→hotswap→cleanup); updates job status at each phase boundary. **Only** file that calls pipeline stages.
- `supervisor.go` — goroutine lifecycle **only**: spawn worker goroutines, track active goroutines, cancel on shutdown signal. No pipeline logic permitted here.
- `context.go` — deadline/cancellation propagation **only**: wraps `context.WithTimeout` with per-project build timeout from config. No business logic permitted here.

These three files must never have their responsibilities bleed into each other.

## Log Streaming (`internal/api/sse/stream.go`)

- Use Server-Sent Events (SSE) only. No WebSockets.
- Topic-based broadcaster with two topic types:
  - `dashboard` — job status change events (all clients subscribe via `GET /api/events`)
  - `log:{jobID}` — per-job log line streaming (client subscribes via `GET /api/deployments/:id/logs`)
- `PublishJSON(topic, data)` for structured events; `Publish(topic, line)` for log lines.
- Dashboard SSE sends `event: init` with snapshot of active jobs on connect, then stream updates.
- Historical logs served by replaying the `.log` file line by line over SSE.
- SSE streaming runs in its own goroutine per connected client, cancelled on client disconnect.
- Queue manager publishes `JobStatusEvent` on every status transition via `publishStatusChange()`.

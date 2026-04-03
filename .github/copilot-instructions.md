# Trangly — Copilot Workspace Instructions

> **Canonical structure reference:** `docs/STRUCTURE.md` is mandatory reading.
> All architectural decisions, layer boundaries, and dependency rules in this file derive from it.

## What Trangly Is

A **single-binary, self-hosted CI/CD tool** written in Go that watches a GitHub repo and auto-redeploys a Docker-based application on every push. Target users are solo developers and freelancers who want push-to-deploy with zero YAML, zero cloud accounts, and zero DevOps knowledge.

**The one-liner:** Trangly does exactly one thing — watches a repo, rebuilds the Docker app, and keeps old prod running until the new build is confirmed healthy.

## What Trangly Is NOT

- Not a PaaS (no routing, no DB provisioning, no environment management)
- Not a general-purpose CI runner (no multi-step pipelines, no matrix builds)
- Not a Kubernetes tool
- Not multi-user in v1.0 (single admin only)
- Not a GitLab/Bitbucket tool (GitHub only in v1.0)

## Tech Stack — Hard Constraints

| Layer | Technology | Must NOT use |
|---|---|---|
| Backend | Go 1.22+ | No `google/go-github` SDK (too heavy — only 4 API calls needed) |
| Frontend | Alpine.js 3.14.1 + Tailwind CSS (CDN) + Font Awesome 6.5.1 + Tom Select 2.3.1 (embedded) | No React, Vue, Next.js, or any Node.js build step |
| Database | SQLite via `modernc.org/sqlite` | No Postgres, no MySQL, no external DB |
| Docker control | Docker SDK for Go (image/container ops) + `exec.CommandContext` for `docker compose` CLI | No `exec.Command("docker build/run/stop ...")` — only `docker compose` via CLI (no SDK equivalent) |
| Log streaming | Server-Sent Events (SSE) with topic-based broadcaster | No WebSockets |
| GitHub integration | Direct HTTP calls + minimal JWT signer | No `google/go-github`, no `bradleyfalzon/ghinstallation` |
| App constants | `pkg/version` package | Never hardcode `"Trangly"`, `2880`, or `~/.trangly` anywhere else |

**Docker compose note:** `docker compose` (V2 CLI plugin) has no Docker SDK equivalent. Trangly uses `exec.CommandContext` with `cmd.Cancel = process.Kill` for reliable timeout/cancellation. All other Docker ops (prune images, inspect, stats) use the Docker SDK via `internal/infra/docker/`.

## Go Package Structure (canonical — matches docs/STRUCTURE.md exactly)

```
Trangly/
├── cmd/trangly/main.go              ← parse flags, print version.String(), call bootstrap
├── pkg/
│   └── version/version.go           ← AppName, AppSlug, AppPort, AppDataDir, Version — NO internal imports
└── internal/
    ├── domain/                       ← pure business models; compiles with zero external deps
    │   ├── project.go
    │   ├── deployment.go
    │   ├── job.go
    │   ├── queue.go
    │   └── enums.go                 ← ALL status string constants live here only
    ├── app/                          ← use-case orchestration
    │   ├── project_service.go
    │   ├── deploy_service.go
    │   ├── github_auth_service.go   ← magic link, manifest, token exchange ONLY
    │   ├── github_webhook_service.go ← event routing, job insertion ONLY
    │   ├── auth_service.go
    │   ├── helpers.go
    │   └── doc.go
    ├── infra/                        ← wraps ALL external dependencies
    │   ├── db/                      ← sqlc-generated; never hand-write SQL access
    │   ├── docker/                  ← Docker SDK wrapper
    │   ├── git/                     ← clone, checkout, clean workspace
    │   ├── github/                  ← raw GitHub API calls only
    │   │   ├── auth.go
    │   │   ├── client.go
    │   │   ├── install.go
    │   │   ├── retry.go             ← HTTP retry with exponential backoff
    │   │   └── token_cache.go       ← installation token cache
    │   ├── crypto/                  ← AES-256-GCM, Argon2id key derivation
    │   └── system/
    │       ├── preflight.go         ← all 7 startup checks (incl. public reachability)
    │       ├── memory.go            ← /proc/meminfo reader (Linux)
    │       ├── memory_windows.go    ← Windows memory reader
    │       ├── memory_types.go      ← MemInfo struct shared across platforms
    │       ├── cpu.go               ← /proc/cpuinfo reader (Linux)
    │       ├── cpu_windows.go       ← Windows CPU info reader
    │       └── distro.go            ← /etc/os-release parser
    ├── deploy/
    │   ├── pipeline.go              ← orchestrates pipeline phases
    │   ├── fetch.go
    │   ├── build.go
    │   ├── healthcheck.go
    │   ├── healthcheck_detect.go    ← auto-detect HTTP/TCP/None from Compose file
    │   ├── swap.go                  ← rebuild mode swap (compose down + up)
    │   ├── hotswap.go               ← hot-swap mode (file copy, no rebuild)
    │   └── cleanup.go
    ├── queue/
    │   ├── manager.go               ← per-project FIFO queues, SQLite persistence
    │   ├── worker.go
    │   └── state_machine.go         ← ONLY place state transitions are validated
    ├── scheduler/
    │   ├── scheduler.go             ← 5s loop, RAM-aware job pickup
    │   ├── memory.go                ← docker stats sampler, adaptive estimate calculator
    │   └── monitor.go               ← hold escalation, 30-min auto-fail
    ├── engine/
    │   ├── runner.go                ← calls pipeline stages; two code paths (rebuild vs hot-swap)
    │   ├── supervisor.go            ← goroutine lifecycle ONLY (spawn, track, cancel)
    │   └── context.go               ← cancellation + timeout propagation ONLY
    ├── api/
    │   ├── http/
    │   │   ├── server.go
    │   │   ├── middleware/
    │   │   │   ├── auth.go          ← JWT verification
    │   │   │   ├── logger.go        ← structured request logging
    │   │   │   ├── ratelimit.go     ← token bucket rate limiter
    │   │   │   └── security.go      ← security headers
    │   │   └── handlers/
    │   │       ├── auth.go
    │   │       ├── deploy.go
    │   │       ├── events.go
    │   │       ├── github.go
    │   │       ├── helpers.go       ← shared response helpers
    │   │       ├── project.go
    │   │       ├── queue.go
    │   │       └── system.go
    │   └── sse/
    │       └── stream.go
    ├── webhook/
    │   ├── github.go                ← signature validation, event parsing
    │   └── handler.go
    ├── ui/
    │   ├── ui.go                    ← route registration + template rendering
    │   ├── render.go                ← template renderer construction
    │   └── static/
    ├── config/config.go
    └── bootstrap/
        ├── app.go                   ← startup sequence: preflight → DB → scheduler → HTTP
        ├── container.go             ← dependency wiring only
        └── setup.go                 ← initial admin setup wizard
```

**Hard rules:**
- Every database read/write goes through `internal/infra/db/`. No raw SQL elsewhere.
- No state string literals outside `internal/domain/enums.go`.
- `supervisor.go` owns goroutine lifecycle only. `context.go` owns deadline/cancellation only. `runner.go` is the only file that calls pipeline stages.
- `github_auth_service.go` and `github_webhook_service.go` must NEVER be merged.

## Storage Layout (VPS)

Path is `pkg/version.AppDataDir` — never hardcode `~/.trangly` anywhere else.

```
~/.trangly/          ← version.AppDataDir
  config.db           ← SQLite: projects, deploy_jobs, users, queue, github_app
  master.key          ← AES-256 key (generated at first run, chmod 600, never logged)
  github-app.pem      ← GitHub App private key (AES-256-GCM encrypted at rest)
  workspaces/
    {project-id}/
      {short-sha}/    ← git clone per commit, deleted after successful swap
  logs/
    {deploy-id}.log   ← raw stdout+stderr, one file per job
```

## Security Non-Negotiables

1. **Passwords** — bcrypt (cost ≥ 12). Never store plaintext.
2. **JWTs** — HS256, signed with a random 32-byte secret stored in `config.db`. Validate on every protected route.
3. **Encryption at rest** — AES-256-GCM for GitHub App PEM and project env vars. Master key derived with Argon2id (time=1, memory=64MB, threads=4) from admin password — never stored directly.
4. **Webhook validation** — every `/webhooks/github` request must pass `X-Hub-Signature-256` HMAC-SHA256 check before any processing. Reject and 401 immediately on failure.
5. **State tokens** (GitHub OAuth) — cryptographically random 32 bytes, single-use, expire in 10 minutes, deleted on first use.
6. **Rate limiting** — webhook endpoint: 60 req/min per IP.
7. **Docker socket** — document the risk clearly. Never expose the socket to untrusted code.
8. **Secrets never logged** — env vars, PEM keys, JWT secrets must never appear in any log output or API response after initial save.

## Deployment Pipeline

Execution path: `API → Queue → Scheduler → Engine(runner.go) → Pipeline`

Trangly supports **two pipeline modes**, selected per-project via `DeployModeRebuild` or `DeployModeHotSwap` (defined in `domain/enums.go`).

### Rebuild Mode (default — 5 phases)

Old prod container runs untouched through phases 1–3. Only SWAP (phase 4) affects production.

```
FETCH       → fetch.go        git clone --depth=1; resolve HEAD if needed
BUILD       → build.go        docker compose build (exec.CommandContext + cmd.Cancel)
HEALTH CHECK→ healthcheck.go  validate compose config; auto-detect mode from Compose
SWAP        → swap.go         docker compose down --timeout 30; docker compose up -d --timeout 120
CLEANUP     → cleanup.go      remove workspace; prune dangling images; update job SUCCESS; emit SSE
```

### Hot-Swap Mode (3 phases)

Skips build and health check — copies updated files into the running container:

```
FETCH       → fetch.go        git clone --depth=1; resolve HEAD if needed
HOT_SWAP    → hotswap.go      file-copy deploy into running container
CLEANUP     → cleanup.go      remove workspace; update job SUCCESS; emit SSE
```

**Failure rule:** Phase failure → mark FAILED, stop. Old prod untouched for FETCH/BUILD/HEALTHCHECK failures. SWAP failure → error returned up stack.

Each stage function must: accept `context.Context`, return a typed error, write structured log lines to the job log stream.

**Stuck job monitor (in `scheduler/monitor.go`):** Jobs stuck in running/building/health_check/swapping > 10 min → auto-fail.

## RAM-Aware Scheduler Rules

**Budget (calculated once at startup — in `scheduler/scheduler.go`):**
```
safety_buffer   = total_ram × 20%
trangly_rsv    = 50 MB
available_ram   = total_ram - safety_buffer - trangly_rsv
```

**Estimate logic (in `scheduler/memory.go`):**
- First deploy (no history): `estimate = mem_limit + 150 MB`. If no mem_limit: `300 MB`. Mark `estimate_source = 'bootstrap'`
- Subsequent deploys: `estimate = avg(runtime steady-state RSS of last 3 builds) × 1.20`. Mark `estimate_source = 'adaptive'`
- Two sampling phases: build-phase peak RSS (for overhead model) and runtime steady-state RSS during health check (for adaptive estimate)
- `docker stats` sampled every 2 seconds via `infra/docker` — scheduler never calls Docker API directly

**Scheduler loop (every 5s — in `scheduler/scheduler.go`):**
```
effective = min(current_free_ram, available_ram - sum(running job estimates))
if effective >= job.ram_estimate → delegate to engine (never execute directly)
else → hold with hold_reason = 'insufficient_memory'
```

**Escalation (in `scheduler/monitor.go`):**
- Held > 10 min → WARNING banner in UI
- Held > 30 min → AUTO-FAIL with clear message
- `job.ram_estimate > available_ram` → FAIL immediately (never held)

## Job State Machine

Valid `status` values defined as constants in `internal/domain/enums.go` — **nowhere else**.

Rebuild mode: `pending` → `held` → `running` → `building` → `health_check` → `swapping` → `success`
Hot-swap mode: `pending` → `held` → `running` → `hot_swapping` → `success`
Any state → `failed`

All state transitions validated centrally in `internal/queue/state_machine.go`. No ad-hoc status string updates anywhere else.

## Health Check Auto-Detection (`internal/deploy/healthcheck_detect.go`)

1. Parse `docker-compose.yml` for exposed ports
2. If `healthcheck` defined in Compose → use it as-is
3. If port exposed, no healthcheck: TCP probe first; then probe `/health`, `/healthz`, `/ping` for HTTP
4. If no ports → warn user, default to None (swap immediately after build)

Detection runs at deploy time. Result shown in UI as read-only indicator at project creation time.

## API Endpoints (Complete List)

| Method | Path | Auth | Handler location |
|---|---|---|---|
| GET | `/api/version` | Public | `handlers/system.go` — returns `pkg/version.Info()` |
| GET | `/api/system/preflight` | Public | `handlers/system.go` — structured `CheckResult` list |
| GET | `/api/system/ram` | Authenticated | `handlers/system.go` |
| GET | `/api/system/stats` | Authenticated | `handlers/system.go` |
| GET | `/api/setup/status` | Public | `handlers/system.go` |
| POST | `/api/setup` | Public | `handlers/system.go` |
| POST | `/api/auth/login` | Public | `handlers/auth.go` |
| GET | `/api/auth/me` | Authenticated | `handlers/auth.go` |
| PUT | `/api/auth/profile` | Authenticated | `handlers/auth.go` |
| POST | `/api/auth/change-password` | Authenticated | `handlers/auth.go` |
| GET | `/api/github/manifest` | Authenticated | `handlers/github.go` |
| GET | `/api/github/callback` | Public (state-validated) | `handlers/github.go` |
| GET | `/api/github/status` | Authenticated | `handlers/github.go` |
| GET | `/api/github/repos` | Authenticated | `handlers/github.go` |
| GET | `/api/github/repos/:owner/:repo/branches` | Authenticated | `handlers/github.go` |
| GET | `/api/github/repos/:owner/:repo/compose-files` | Authenticated | `handlers/github.go` |
| GET | `/api/github/install-url` | Authenticated | `handlers/github.go` |
| DELETE | `/api/github/app` | Authenticated | `handlers/github.go` |
| POST | `/api/repos/:owner/:repo/status` | Authenticated | `handlers/github.go` |
| GET | `/api/projects` | Authenticated | `handlers/project.go` — includes last deploy per project |
| GET | `/api/projects/:id` | Authenticated | `handlers/project.go` |
| GET | `/api/projects/:id/env` | Authenticated | `handlers/project.go` |
| POST | `/api/projects` | Authenticated | `handlers/project.go` — includes RAM viability check |
| PUT | `/api/projects/:id` | Authenticated | `handlers/project.go` |
| DELETE | `/api/projects/:id` | Authenticated | `handlers/project.go` |
| POST | `/api/projects/:id/deploy` | Authenticated | `handlers/deploy.go` — supports specific SHA for rollback |
| GET | `/api/queue` | Authenticated | `handlers/queue.go` |
| DELETE | `/api/queue/:job_id` | Authenticated | `handlers/queue.go` |
| GET | `/api/deployments` | Authenticated | `handlers/deploy.go` — paginated (?limit=20&offset=0) |
| GET | `/api/deployments/:id/logs` | Authenticated (SSE) | `api/sse/stream.go` |
| GET | `/api/events` | Authenticated (SSE) | `handlers/events.go` — real-time dashboard events |
| POST | `/webhooks/github` | Public (HMAC-validated) | `internal/webhook/handler.go` |

## GitHub Magic Link Flow (App Manifest)

Trangly uses GitHub's App Manifest flow. The user clicks ONE button in Trangly, ONE button on GitHub. No copy-pasting.

Implementation split:
- `internal/app/github_auth_service.go` — manifest generation, state token, credential storage (business logic)
- `internal/infra/github/` — raw HTTP calls to GitHub API only
- `internal/webhook/` — signature validation + event parsing
- `internal/app/github_webhook_service.go` — event routing + job insertion

These must NEVER be merged into a single file.

Flow steps:
1. Trangly builds manifest JSON server-side with all permissions pre-filled
2. Stores one-time state token (32 random bytes) in SQLite with 10-min expiry
3. Redirects browser to `https://github.com/settings/apps/new?state={token}`
4. GitHub redirects back to `/api/github/callback?code={code}&state={token}`
5. Trangly validates state (single-use, not expired), then POSTs to `https://api.github.com/app-manifests/{code}/conversions`
6. Receives `app_id`, `webhook_secret`, `pem` — encrypts all with AES-256-GCM immediately
7. Never displays credentials. Marks GitHub connected.

**GitHub App permissions:** `contents: read`, `metadata: read`, `statuses: write`
**GitHub App events:** `push`, `create`

## SQLite Schema — Key Tables

```sql
-- Deploy jobs (core queue/history table)
CREATE TABLE deploy_jobs (
  id               TEXT PRIMARY KEY,
  project_id       TEXT NOT NULL,
  commit_sha       TEXT NOT NULL,
  branch           TEXT NOT NULL,
  status           TEXT NOT NULL,
  worker_id        INTEGER,
  ram_estimate_mb  INTEGER,
  peak_rss_mb      INTEGER,
  estimate_source  TEXT,       -- 'bootstrap' | 'adaptive'
  hold_reason      TEXT,       -- 'insufficient_memory' | null
  log_path         TEXT,
  queued_at        DATETIME NOT NULL,
  held_at          DATETIME,
  started_at       DATETIME,
  finished_at      DATETIME,
  error            TEXT
);
```

## Dependency Direction Rules (non-negotiable)

```
Allowed:
  api        → app → domain
                   → infra
  webhook    → app
  deploy     → domain + infra
  queue      → domain
  scheduler  → queue + infra/system + infra/docker (stats only)
  engine     → deploy + queue
  bootstrap  → everything (wiring only)
  pkg/version → nothing (zero imports from internal/)

Forbidden:
  api       → deploy     (never bypass queue)
  api       → docker     (never direct Docker access)
  queue     → api
  infra     → domain
  scheduler → deploy     (delegates to engine only)
  engine    → scheduler  (no circular)
  domain    → anything   (domain is the root)
```

## App Constants — `pkg/version` (first package written, everything imports it)

```go
const (
    AppName    = "Trangly"
    AppSlug    = "Trangly"
    AppPort    = 2880
    AppDataDir = "~/.trangly"
    AppBinary  = "Trangly"
)
// Version, Commit, BuildDate injected via ldflags at build time
```

`version.Info()` returns the structured map served by `GET /api/version`.
`version.String()` is printed exactly once at process start.

## Performance & Binary Targets

| Metric | Target |
|---|---|
| Binary size | < 25 MB |
| Idle RAM | < 35 MB |
| Active deploy RAM | < 60 MB |
| Peak (2 concurrent + SSE) | < 100 MB |

**Do NOT** use heavy dependencies. Audit `go.sum` regularly. Prefer stdlib over third-party where reasonable.

## Preflight Checks (run every startup — `internal/infra/system/preflight.go`)

All checks return `CheckResult{Name, Status, Detail, Fix}` — the API returns this as JSON, the UI renders it as the live checklist. No string parsing in the frontend.

Checks run in order; all must pass before wizard proceeds:
1. OS supported — parsed by `internal/infra/system/distro.go` from `/etc/os-release`
2. Docker installed (minimum v20.x)
3. Docker Compose V2 CLI plugin present (minimum v2.x)
4. Docker socket accessible (`/var/run/docker.sock`)
5. Port `version.AppPort` available — **never hardcode 2880**
6. **Public reachability** — hard gate; GitHub must reach this VPS before Connect GitHub is shown
7. Sudo access (only required if Docker install needed)

If Docker is missing → prompt user (never auto-install silently).
If Compose plugin is missing → auto-install silently (no prompt needed).
If socket permission denied → offer one-click `usermod -aG docker $USER` fix.
If not publicly reachable → block wizard; show firewall fix options and Cloudflare Tunnel guide.

## Out of Scope — v1.0

Do NOT implement or suggest:
- Multi-user / role-based access
- GitLab, Bitbucket, or Gitea integration
- Non-Docker deployments
- Build caching
- PR preview environments
- Slack/email/Telegram notifications
- Horizontal scaling or clustering
- Auto-TLS / Let's Encrypt (v1.1)
- Auto-rollback (v1.1)
- Kubernetes

## Code Conventions

- All errors returned up the call stack; no silent swallowing
- Structured logging via `log/slog` (Go 1.21+); use `pkg/version.AppName` as logger name — never hardcode `"Trangly"`
- `context.Context` threaded through all I/O operations for cancellation; no ad-hoc `context.Background()` in business logic
- No global state — dependency injection via `internal/bootstrap/container.go`
- Database migrations run at startup via embedded SQL files in `internal/infra/db/migrations/`
- All durations, sizes, and string constants in constants — no magic numbers or string literals in scheduler/deploy logic
- No state string literals outside `internal/domain/enums.go`
- Test files alongside source (`_test.go`), table-driven where applicable
- Each app service file owns exactly one domain concern; if a file exceeds ~200 lines, split it

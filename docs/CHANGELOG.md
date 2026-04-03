# Changelog

All notable changes to Trangly are documented here.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Versioning follows [Semantic Versioning](https://semver.org/).

---

## [Unreleased]

---

## [1.0.0] — 2026-04-03

### Added

**Core pipeline**
- Five-phase deploy pipeline: FETCH → BUILD → HEALTH CHECK → SWAP → CLEANUP
- Old production container stays live through FETCH, BUILD, and HEALTH CHECK phases
- SWAP is the only phase that affects production; failure at any earlier phase leaves production untouched
- Per-deploy log files streamed live via Server-Sent Events

**GitHub integration**
- One-click GitHub App setup via manifest flow (no copy-pasting tokens or webhook URLs)
- HMAC-SHA256 webhook signature validation on every incoming push event
- `push` and `create` event routing; branch-filtered job insertion

**RAM-aware scheduler**
- Startup budget: `available_ram = total_ram × 80% − 50 MB (Trangly reserve)`
- Bootstrap estimate (first deploy): `mem_limit + 150 MB`; fallback to 300 MB if no limit set
- Adaptive estimate (subsequent deploys): `avg(last 3 steady-state RSS) × 1.20`
- Jobs held with `hold_reason = insufficient_memory` when budget is tight
- Held > 10 min → WARNING banner; held > 30 min → auto-fail

**Health check auto-detection**
- Parses `docker-compose.yml` for exposed ports and `healthcheck` directives
- If `healthcheck` defined: uses it as-is
- If port exposed, no healthcheck: TCP probe → HTTP probes (`/health`, `/healthz`, `/ping`)
- If no ports: warns user, defaults to None (swap immediately after build)

**Authentication & security**
- bcrypt admin password (cost 12)
- HS256 JWT with a random 32-byte secret per installation
- AES-256-GCM encryption of GitHub App PEM and project env vars at rest
- Master key derived with Argon2id (t=1, m=64 MB, p=4) from admin password
- Single-use 32-byte state tokens with 10-minute expiry for GitHub OAuth
- Rate limiting: 60 req/min per IP on the webhook endpoint

**Queue system**
- Per-project FIFO queue with SQLite persistence (survives restarts)
- Central state machine in `internal/queue/state_machine.go`
- Valid states: `pending → held → running → building → health_check → swapping → success`; any → `failed`
- Stuck-job monitor: jobs in terminal-phase states > 10 min are auto-failed

**Web dashboard**
- Setup wizard with live preflight checklist (OS, Docker, Compose, socket, port, public reachability)
- Project management: add, edit, delete; supports custom Compose file path and branch
- Deployment history with live log viewer and SSE-powered status updates
- RAM indicator: shows VPS budget and per-project estimate
- Queue view with cancel support

**CLI**
- `trangly setup` — interactive first-run wizard
- `trangly start` — start server (`--port`, `--data-dir`, `--base-url` flags)
- `trangly check` — run preflight checks and exit
- `trangly install` — install as systemd service (Linux) or print NSSM guide (Windows)
- `trangly uninstall` — remove systemd service
- `trangly reset` — factory reset (deletes DB, keys, logs, workspaces)
- `trangly version` — print version info

**Packaging**
- `.deb` for Debian/Ubuntu (amd64, arm64)
- `.rpm` for RHEL/Fedora/CentOS (amd64, arm64)
- `postinstall.sh` runs `trangly check` after package install
- Windows `.exe` (amd64) for development use

---

[Unreleased]: https://github.com/udaypankhaniya/trangly/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/udaypankhaniya/trangly/releases/tag/v1.0.0

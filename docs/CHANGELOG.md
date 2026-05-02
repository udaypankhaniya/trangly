# Changelog

All notable changes to Trangly are documented here.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Versioning follows [Semantic Versioning](https://semver.org/).

---

## [0.1.2] — 2026-05-02

### Added

- **Web terminal** — SSH-like terminal access directly from the Trangly dashboard; no separate SSH client needed
- README screenshots (Dashboard, Deployments, Project, Live Logs, Terminal, Settings)

### Changed

- Replaced placeholder demo section in README with actual UI screenshots

---

## [0.1.1] — 2026-04-11

### Changed

- Replace all CDN deps with self-hosted/compiled alternatives (Tailwind, Font Awesome, Geist fonts, Tom Select); only Alpine.js remains on CDN
- Compile Tailwind CSS v4 to embedded `dist/style.min.css` (58 KB vs 400 KB CDN)
- Replace Font Awesome with inline Lucide SVG sprite (56 icons, `scripts/build-icons.mjs`)
- Switch to system font stack, remove Geist CDN requests
- Self-host Tom Select 2.3.1 in `static/vendor/`
- Full design system rewrite in `style.css` (theme tokens, component classes, animations)
- Tighten CSP: remove `cdn.tailwindcss.com`, `cdn.jsdelivr.net`, `cdnjs.cloudflare.com`

### Added

- Fiber gzip middleware (`LevelBestSpeed`), skip SSE routes
- `Cache-Control: public, max-age=3600` for static assets; `no-store` for HTML
- Adaptive dark/light SVG favicon (terminal icon)

### Performance

- Binary size: 18.3 MB
- Idle RAM: < 35 MB

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

[Unreleased]: https://github.com/udaypankhaniya/trangly/compare/v0.1.2...HEAD
[0.1.2]: https://github.com/udaypankhaniya/trangly/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/udaypankhaniya/trangly/compare/v1.0.0...v0.1.1
[1.0.0]: https://github.com/udaypankhaniya/trangly/releases/tag/v1.0.0

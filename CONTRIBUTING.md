# Contributing to Trangly

Thank you for your interest in contributing. Trangly is intentionally focused — it does one thing (push-to-deploy for Docker apps) and the contribution bar reflects that philosophy: no feature creep, no over-engineering.

---

## Before You Start

- Check [open issues](https://github.com/udaypankhaniya/trangly/issues) to avoid duplicate work
- For sizeable features or architectural changes, open an issue first to discuss the approach
- Trangly is a focused tool — contributions that add scope beyond push-to-deploy for Docker apps will not be accepted

---

## Out of Scope (do not submit PRs for these)

- Multi-user / RBAC
- GitLab, Bitbucket, or Gitea integration
- Non-Docker deployments
- Build caching layers
- PR preview environments
- Kubernetes / multi-node
- Notification integrations (planned for v1.2 — coordinate first)
- Auto-rollback (planned for v1.1 — coordinate first)

---

## Development Setup

### Prerequisites

- **Go 1.22+**
- **Docker** with Compose V2 plugin
- **sqlc** (only if changing SQL schema): `go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest`
- **golangci-lint**: [install guide](https://golangci-lint.run/usage/install/)
- **nfpm** (only for package builds): `go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest`

### Clone & run

```bash
git clone https://github.com/udaypankhaniya/trangly.git
cd trangly
go mod tidy
make setup   # first-run: creates admin in ./dev-data
make run     # starts server on :2880
```

### Tests

```bash
make test       # all tests, race detector
make cover      # coverage report → coverage.out
```

All new code must include table-driven tests in `_test.go` files alongside the source.

### Lint

```bash
make lint
```

No warnings, no excluded rules.

---

## Architecture Rules (non-negotiable)

Read [docs/STRUCTURE.md](docs/STRUCTURE.md) before touching any package boundary.

| Rule | Why |
|---|---|
| All deploys flow through `API → Queue → Scheduler → Engine → Pipeline` | Prevents race conditions and ensures RAM-aware scheduling |
| Status string constants live only in `internal/domain/enums.go` | Single source of truth for the state machine |
| State transitions validated only in `internal/queue/state_machine.go` | Prevents invalid state jumps |
| `internal/infra/` wraps ALL external dependencies | Enables testability and keeps domain pure |
| `pkg/version` has zero imports from `internal/` | Anything can import it safely |
| No raw SQL outside `internal/infra/db/` | sqlc generates the query layer |
| `github_auth_service.go` and `github_webhook_service.go` must stay separate | Different concerns, different change velocity |

---

## Code Conventions

- **Structured logging**: `log/slog`, use `pkg/version.AppName` as the logger name — never hardcode `"Trangly"`
- **Context propagation**: thread `context.Context` through all I/O, no ad-hoc `context.Background()` in business logic
- **No global state**: all dependencies injected via `internal/bootstrap/container.go`
- **No magic numbers or string literals**: durations, sizes, constants in `const` blocks
- **Error handling**: return errors up the stack — no silent swallowing, no `log.Fatal` outside of `main`
- **File size**: if a service file exceeds ~200 lines, split it; each file owns exactly one domain concern
- **No `exec.Command("docker build/run/stop")`**: Docker SDK for image/container ops; `exec.CommandContext` only for `docker compose` (V2 CLI plugin has no SDK equivalent)

---

## Submitting a PR

1. Fork the repo and create a branch: `git checkout -b fix/short-description`
2. Make your changes, add or update tests
3. Run `make test` and `make lint` — both must pass
4. Open a PR against `main` using the PR template
5. Link any related issue in the PR description

PR titles follow [Conventional Commits](https://www.conventionalcommits.org/):
```
fix: handle webhook delivery failure when installation_id is missing
feat: add TCP health check fallback for exposed ports
docs: clarify RAM estimate sources in scheduler README
```

---

## Reporting Bugs

Use the [bug report template](https://github.com/udaypankhaniya/trangly/issues/new?template=bug_report.yml).

Include:
- Trangly version (`trangly version`)
- OS and Docker version
- Steps to reproduce
- Actual vs. expected behaviour
- Relevant log lines from `~/.trangly/logs/`

---

## Security Issues

**Do not open a public issue for security vulnerabilities.** See [SECURITY.md](SECURITY.md) for the responsible disclosure process.

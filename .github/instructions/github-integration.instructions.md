---
applyTo: "internal/app/github_auth_service.go,internal/app/github_webhook_service.go,internal/infra/github/**,internal/webhook/**"
---

# GitHub Integration Rules

## Service Split — Never Merge These

| File | Owns |
|---|---|
| `internal/app/github_auth_service.go` | Magic link flow, manifest generation, state token lifecycle, credential storage |
| `internal/app/github_webhook_service.go` | Webhook event routing, branch matching, job insertion into queue |
| `internal/infra/github/` | Raw GitHub HTTP API calls only — no business logic |
| `internal/webhook/github.go` | `X-Hub-Signature-256` HMAC validation only |
| `internal/webhook/handler.go` | HTTP handler: validate → parse → delegate to `github_webhook_service.go` |

These must NEVER be merged. They have different lifecycles, different failure modes, and different test requirements.

## Zero External SDK

- **Never** import `google/go-github` or `bradleyfalzon/ghinstallation`.
- Trangly makes exactly **4 GitHub API calls** — all in `internal/infra/github/`, done with `net/http` + minimal JWT signing:
  1. `POST https://api.github.com/app-manifests/{code}/conversions` — exchange manifest code for credentials (called from `github_auth_service.go`)
  2. `POST https://api.github.com/app/installations/{id}/access_tokens` — get installation token (JWT auth)
  3. `GET  https://api.github.com/installation/repositories` — list repos
  4. `POST https://api.github.com/repos/{owner}/{repo}/statuses/{sha}` — write commit status

## App Manifest Flow — Exact Responsibility Split

**`internal/app/github_auth_service.go` owns all business logic:**
```
Step 1 — GenerateManifestRedirect(ctx):
  - Build manifest JSON
  - Generate state token: 32 crypto/rand bytes, hex-encoded
  - Store in SQLite: (token, expires_at = now+10min, used=false)
  - Return { "redirect_url": ... }

Step 2 — HandleCallback(ctx, code, state):
  - Look up state token in SQLite via infra/db
  - Reject if: not found / expired / already used
  - Mark token used=true immediately (BEFORE any API call)
  - Call infra/github.ConvertManifest(code)
  - Encrypt app_id, webhook_secret, pem immediately on receipt via infra/crypto
  - Store encrypted blobs in SQLite
  - Never log or return raw credentials
```

**`internal/infra/github/` owns only the raw HTTP calls** — no state token logic, no encryption, no SQLite.

## Manifest JSON Structure

```json
{
  "name": "Trangly on {hostname}",
  "url": "http://{vps_ip}:2880",
  "hook_attributes": {
    "url": "http://{vps_ip}:2880/webhooks/github",
    "active": true
  },
  "redirect_url": "http://{vps_ip}:2880/api/github/callback",
  "public": false,
  "default_permissions": {
    "contents": "read",
    "metadata": "read",
    "statuses": "write"
  },
  "default_events": ["push", "create"]
}
```

## JWT Signing for GitHub App Auth

GitHub App API calls require a short-lived JWT signed with the App's RSA private key (RS256). Trangly implements this without any SDK:

```
header  = base64url({ "alg": "RS256", "typ": "JWT" })
payload = base64url({ "iat": now-60, "exp": now+540, "iss": app_id })
signature = RS256(header.payload, pem_private_key)
jwt = header.payload.signature
```

- Decrypt PEM from SQLite (AES-256-GCM) at call time — never cache decrypted key in memory long-term.
- JWT lifespan: 10 minutes max (GitHub enforces this).
- `iat` is set 60 seconds in the past to account for clock skew.

## Webhook Handler — `/webhooks/github` (`internal/webhook/`)

```
// internal/webhook/github.go
1. ValidateSignature: HMAC-SHA256(webhook_secret, raw_body) vs X-Hub-Signature-256
   → If mismatch: return 401. Stop. Log warning. No body parsing.

// internal/webhook/handler.go → delegates to app/github_webhook_service.go
2. Parse X-GitHub-Event header
3. Handle only "push" events — ignore all others with 200 OK
4. Extract: repository.full_name, head_commit.id (SHA), ref (branch)

// internal/app/github_webhook_service.go
5. Match ref+repo against projects watching that repo+branch
6. Insert deploy_job into queue for each matching project
7. Return 200 immediately — never block webhook response on job processing
```

**Rate limiting:** 60 req/min per IP applied in `api/http/middleware/` before signature check. Use an in-memory token bucket (no external dep).

**Never** process a webhook payload before validating the signature. The signature check is the trust boundary.

## State Token Security Properties

- 32 bytes from `crypto/rand` — never `math/rand`
- Single-use: marked `used=true` the moment it is validated, before any downstream API call
- 10-minute TTL: `expires_at` checked on every use
- No token reuse on retry — user must re-click the Connect GitHub button to generate a fresh token
- Tokens are cleaned up (DELETE WHERE expires_at < now) on every startup and on each callback request

## Installation Token Caching

Installation tokens from GitHub expire in 1 hour. Cache them in memory (not SQLite):

```go
type tokenCache struct {
    token     string
    expiresAt time.Time
}
// Refresh when expiresAt - now < 5 minutes
```

Never persist an unencrypted installation token to disk.

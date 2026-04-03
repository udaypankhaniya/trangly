---
applyTo: "internal/app/auth_service.go,internal/infra/crypto/**,internal/api/http/**,internal/webhook/**"
---

# Security Rules

## Passwords

- Hash with `bcrypt`, cost **≥ 12**. Use `golang.org/x/crypto/bcrypt`.
- Auth logic lives in `internal/app/auth_service.go`. Crypto primitives in `internal/infra/crypto/`.
- Never store, log, or return a plaintext password anywhere.
- Compare passwords with `bcrypt.CompareHashAndPassword` — never plain equality.

## JWTs (Session Tokens)

- Algorithm: **HS256** only. Never HS512, RS256, or "none".
- Signing secret: random 32 bytes generated at first run, stored in `config.db`. Loaded into memory at startup. Never written to a log.
- Validate the signature and expiry on **every** protected route — no exceptions.
- Token lifespan: 24 hours. No refresh tokens in v1.0.
- On validation failure: return `401 Unauthorized`, no body detail (prevents oracle attacks).
- Implement JWT sign/verify from scratch using `crypto/hmac` + `crypto/sha256` — no external JWT library needed given the simplicity.

## Encryption at Rest (AES-256-GCM) — `internal/infra/crypto/`

All sensitive data persisted to SQLite must be encrypted before storage.

**What must be encrypted:**
- GitHub App PEM private key
- GitHub App webhook secret  
- GitHub App ID (encrypt as belt-and-suspenders)
- Per-project environment variables

**How:**
```
key derivation: Argon2id(password, salt, time=1, memory=65536KB, threads=4) → 32-byte key
encryption:     AES-256-GCM(key, plaintext) → nonce(12B) || ciphertext || tag(16B)
storage:        base64(nonce || ciphertext || tag) in SQLite TEXT column
```

- Nonce: 12 bytes from `crypto/rand`, unique per encryption call. Never reuse a nonce with the same key.
- The master key is **derived** from the admin password via Argon2id — it is never stored directly.
- A random 16-byte salt is generated at first run, stored in `~/.trangly/config.db`, used consistently for the lifetime of the installation.
- If the admin password changes, all encrypted blobs must be re-encrypted. Document this clearly; do not implement silently.

## Webhook Signature Validation (`internal/webhook/github.go`)

Every request to `POST /webhooks/github` must be validated before any body parsing:

```go
// Pseudocode — must be implemented in the actual handler
mac := hmac.New(sha256.New, webhookSecret)
mac.Write(rawBody)
expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
if !hmac.Equal([]byte(expected), []byte(headerValue)) {
    w.WriteHeader(http.StatusUnauthorized)
    return
}
```

- Read the **full raw body** before validating. Do not stream or parse JSON first.
- Use `hmac.Equal` for constant-time comparison — never `==` or `bytes.Equal`.
- On failure: `401`, log `slog.Warn("webhook signature mismatch", "ip", r.RemoteAddr)`. Nothing else.

## Rate Limiting

- Webhook endpoint (`/webhooks/github`): **60 req/min per IP**.
- Use a token bucket implemented in-memory — no external dependency.
- Apply the rate limit check **before** the HMAC validation (cheaper to reject early).
- `429 Too Many Requests` response with `Retry-After` header.

## Secrets Must Never Be Logged

Audit every `slog` call. The following must **never** appear in any log output:
- Admin password (plaintext or hash)
- JWT signing secret
- GitHub App PEM key (decrypted)
- GitHub webhook secret (decrypted)
- Installation access tokens
- Project environment variable values

If you are building a log line that touches one of these, log only metadata (e.g., key name, length) — never the value.

## API Response Sanitization

After initial save, these fields must never be returned in any API response:
- `env_vars` values (return key names only, with `"set": true`)
- GitHub PEM / webhook secret
- JWT signing secret

The response for `GET /api/projects/:id` must redact env var values.

## Input Validation

- Validate all user-supplied input at the HTTP handler boundary.
- Project `branch` and `repo` fields: alphanumeric, `/`, `-`, `_`, `.` only.
- Reject oversized bodies: set `http.MaxBytesReader` on all handlers (max 1MB for API, 50KB for webhooks).
- SQL queries go through sqlc-generated code only — no string interpolation near SQL.

# Security Policy

## Supported Versions

| Version | Supported |
|---|---|
| Latest release | Yes |
| Previous minor | Security fixes only |
| Older | No |

---

## Reporting a Vulnerability

**Please do not open a public GitHub issue for security vulnerabilities.**

Send a report to: **security@trangly.dev**

Include:
- Description of the vulnerability
- Steps to reproduce (proof of concept if possible)
- Affected version(s)
- Your assessment of severity and impact

You will receive an acknowledgement within **48 hours** and a resolution timeline within **7 days**.

---

## Scope

In-scope vulnerabilities:

- Authentication bypass (JWT, bcrypt, session handling)
- Webhook signature bypass (`X-Hub-Signature-256` validation)
- AES-256-GCM encryption weaknesses (master key, PEM at rest)
- Remote code execution via the deploy pipeline
- Path traversal in workspace, log file handling, or hot-swap file copy
- Arbitrary file write or container escape via hot-swap deploy mode
- Secret leakage (env vars, PEM keys, JWT secrets in responses or logs)
- SQL injection via user-controlled input
- SSRF via GitHub App manifest or webhook endpoints
- CORS bypass or cross-origin request forgery
- CSP bypass leading to script injection

Out of scope:

- Issues requiring physical access to the VPS
- Self-XSS in the admin dashboard (single admin, no untrusted users in v1.0)
- Denial of service via extremely large payloads (rate limiting is implemented)
- Issues already reported or in a published CVE for a dependency

---

## Security Design Summary

| Concern | Implementation |
|---|---|
| Passwords | bcrypt, cost 12 |
| Session tokens | HS256 JWT, 32-byte random secret per installation, 24-hour expiry |
| Secrets at rest | AES-256-GCM; master key derived with Argon2id (t=1, m=64MB, p=4) |
| Webhook validation | HMAC-SHA256 on every `/webhooks/github` request; constant-time comparison via `hmac.Equal`; reject on mismatch |
| State tokens | 32 random bytes, single-use, 10-minute expiry |
| Rate limiting | 60 req/min per IP on webhook endpoint; 5 req/min per IP on login endpoint |
| Security headers | CSP, X-Frame-Options: DENY, X-Content-Type-Options: nosniff, Referrer-Policy, Permissions-Policy on all responses |
| CORS | Locked to the configured base URL origin; credentials disallowed |
| Body size limits | 10 MB maximum request body enforced globally by the HTTP framework |
| Docker socket | Documented risk; never expose to untrusted code |
| Secrets in logs | env vars, PEM keys, JWT secrets are never logged or returned in API responses |

---

## Dependency Security

Dependencies are kept minimal to reduce attack surface. Run `go list -m -json all` to audit the dependency tree. The `go.sum` file is committed and verified.

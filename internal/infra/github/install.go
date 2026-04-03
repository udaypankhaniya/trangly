package github

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"time"
)

// GetInstallationToken exchanges a GitHub App JWT for a short-lived installation access token.
// It validates the JWT via GET /app before requesting the token — if the JWT is rejected,
// the call fails fast with a clear error instead of a confusing 401 on the token endpoint.
func (a *App) GetInstallationToken(ctx context.Context, installationID int64) (string, error) {
	appJWT, err := signAppJWT(a.AppID, a.Key)
	if err != nil {
		return "", fmt.Errorf("github: sign JWT: %w", err)
	}

	// Self-verify: prove the JWT is valid before sending to GitHub.
	if err := selfVerifyJWT(appJWT, &a.Key.PublicKey); err != nil {
		return "", fmt.Errorf("github: JWT self-verification FAILED (code bug): %w", err)
	}

	slog.Info("github: requesting installation token",
		"app_id", a.AppID,
		"installation_id", installationID,
		"jwt_self_verify", "pass",
		"pubkey_fp", pubKeyFingerprint(a.Key),
	)

	// Validate the JWT via GitHub before requesting the token.
	if err := validateJWT(ctx, appJWT); err != nil {
		return "", fmt.Errorf("github: JWT is locally valid but rejected by GitHub for app_id=%d — the app's key may not have propagated yet, or the stored PEM doesn't match GitHub's copy; try waiting 30s then retry, or disconnect and reconnect: %w", a.AppID, err)
	}

	url := fmt.Sprintf("https://api.github.com/app/installations/%d/access_tokens", installationID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return "", fmt.Errorf("github: GetInstallationToken request: %w", err)
	}
	setHeaders(req, appJWT)

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("github: GetInstallationToken: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return "", newAPIError(resp, body)
	}

	var result struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("github: GetInstallationToken decode: %w", err)
	}
	return result.Token, nil
}

// DeleteInstallation removes a GitHub App installation via the API (best-effort).
// Uses JWT auth (app-level, not installation token).
func (a *App) DeleteInstallation(ctx context.Context, installationID int64) error {
	appJWT, err := signAppJWT(a.AppID, a.Key)
	if err != nil {
		return fmt.Errorf("github: sign JWT: %w", err)
	}

	url := fmt.Sprintf("https://api.github.com/app/installations/%d", installationID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("github: DeleteInstallation request: %w", err)
	}
	setHeaders(req, appJWT)

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("github: DeleteInstallation: %w", err)
	}
	defer resp.Body.Close()

	// 204 = success, 404 = already gone — both are fine.
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return newAPIError(resp, body)
	}
	return nil
}

// validateJWT calls GET /app to verify the JWT is accepted by GitHub.
// Detects clock skew by comparing local time with GitHub's Date header.
// Logs only the status code and skew — never the JWT or response body.
func validateJWT(ctx context.Context, jwt string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/app", nil)
	if err != nil {
		return fmt.Errorf("build validation request: %w", err)
	}
	setHeaders(req, jwt)

	localNow := time.Now()
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("JWT validation request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))

	// Detect clock skew between local machine and GitHub.
	var skewSec float64
	if dateStr := resp.Header.Get("Date"); dateStr != "" {
		if ghTime, parseErr := http.ParseTime(dateStr); parseErr == nil {
			skewSec = localNow.Sub(ghTime).Seconds()
		}
	}

	slog.Info("github: JWT validation",
		"status", resp.StatusCode,
		"x_github_request_id", resp.Header.Get("X-GitHub-Request-Id"),
		"github_date", resp.Header.Get("Date"),
		"local_time", localNow.UTC().Format(time.RFC1123),
		"clock_skew_sec", int(skewSec),
	)

	if resp.StatusCode != http.StatusOK {
		if math.Abs(skewSec) > 30 {
			return fmt.Errorf("clock skew detected: local time is %.0fs %s GitHub (local=%s, github=%s) — fix your system clock; JWT iat/exp are invalid from GitHub's perspective",
				math.Abs(skewSec),
				skewDirection(skewSec),
				localNow.UTC().Format(time.RFC3339),
				resp.Header.Get("Date"),
			)
		}
		return newAPIError(resp, body)
	}
	return nil
}

func skewDirection(skew float64) string {
	if skew > 0 {
		return "ahead of"
	}
	return "behind"
}

// selfVerifyJWT decodes the JWT, logs the claims (non-sensitive: iss, iat, exp),
// and verifies the RS256 signature locally using the public key.
// If this fails, there is a bug in signAppJWT. If this passes but GitHub rejects
// the JWT, the issue is environmental (key mismatch, propagation delay, etc.).
func selfVerifyJWT(jwt string, pub *rsa.PublicKey) error {
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		return fmt.Errorf("expected 3 JWT parts, got %d", len(parts))
	}

	// Decode and log claims for diagnostics (iss/iat/exp are non-sensitive).
	claimsJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return fmt.Errorf("decode claims: %w", err)
	}
	slog.Info("github: JWT claims", "claims", string(claimsJSON))

	// Verify the signature locally.
	signingInput := parts[0] + "." + parts[1]
	sigBytes, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}
	digest := sha256.Sum256([]byte(signingInput))
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, digest[:], sigBytes); err != nil {
		return fmt.Errorf("RS256 signature invalid: %w", err)
	}
	return nil
}

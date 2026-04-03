// Package webhook validates and parses inbound GitHub webhook requests.
// Signature validation must pass before any payload is processed.
package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// ValidateSignature verifies the X-Hub-Signature-256 header from GitHub.
// secret is the plain-text webhook secret stored per-app.
// payload is the raw request body.
// sigHeader is the value of the X-Hub-Signature-256 header (e.g. "sha256=abc123...").
func ValidateSignature(secret string, payload []byte, sigHeader string) error {
	const prefix = "sha256="
	if !strings.HasPrefix(sigHeader, prefix) {
		return errors.New("webhook: missing sha256= prefix in signature header")
	}
	received, err := hex.DecodeString(sigHeader[len(prefix):])
	if err != nil {
		return fmt.Errorf("webhook: malformed signature header: %w", err)
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expected := mac.Sum(nil)

	if !hmac.Equal(expected, received) {
		return errors.New("webhook: signature mismatch")
	}
	return nil
}

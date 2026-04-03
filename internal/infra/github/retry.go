package github

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"net"
	"time"
)

// Retry configuration constants.
const (
	retryMaxAttempts = 3
	retryBaseDelay   = 1 * time.Second
	retryJitterPct   = 0.25 // ±25%
)

// WithRetry executes fn up to retryMaxAttempts times with exponential backoff and jitter.
// It retries on:
//   - Network errors (net.Error)
//   - HTTP 5xx responses (APIError with Status >= 500)
//   - HTTP 429 (Too Many Requests) — respects Retry-After if present
//
// It does NOT retry:
//   - Context cancellation/deadline
//   - HTTP 4xx errors (except 429)
//   - nil errors (success)
func WithRetry(ctx context.Context, fn func() error) error {
	var lastErr error

	for attempt := range retryMaxAttempts {
		lastErr = fn()
		if lastErr == nil {
			return nil
		}

		// Never retry if the context is done.
		if ctx.Err() != nil {
			return lastErr
		}

		if !isRetryable(lastErr) {
			return lastErr
		}

		// Last attempt — don't sleep, just return the error.
		if attempt == retryMaxAttempts-1 {
			break
		}

		delay := retryDelay(attempt, lastErr)
		slog.Warn("github: retrying after transient error",
			"attempt", attempt+1,
			"max_attempts", retryMaxAttempts,
			"delay", delay,
			"err", lastErr,
		)

		select {
		case <-ctx.Done():
			return lastErr
		case <-time.After(delay):
		}
	}

	return lastErr
}

// isRetryable returns true if the error is a transient failure worth retrying.
func isRetryable(err error) bool {
	// Check for GitHub API errors first.
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		if apiErr.Status == 429 {
			return true
		}
		return apiErr.Status >= 500
	}

	// Network-level transient errors.
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}

	return false
}

// retryDelay calculates the backoff delay for the given attempt (0-indexed).
// Base delay doubles each attempt: 1s, 2s, 4s.
// Jitter of ±25% prevents thundering herd.
// For 429 responses with RetryAfter, uses that value instead.
func retryDelay(attempt int, err error) time.Duration {
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.RetryAfter > 0 {
		return apiErr.RetryAfter
	}

	base := retryBaseDelay << uint(attempt) // 1s, 2s, 4s
	jitter := float64(base) * retryJitterPct * (2*rand.Float64() - 1)
	return base + time.Duration(jitter)
}

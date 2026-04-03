package github

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// tokenSafetyMargin is the buffer subtracted from GitHub's 60-minute token TTL
// to avoid using a token that's about to expire during an in-flight request.
const tokenSafetyMargin = 5 * time.Minute

// TokenCache stores a single GitHub installation access token with expiry.
// Designed for single-installation v1.0 but keyed by installation ID for future multi-install support.
// Thread-safe via sync.Mutex.
type TokenCache struct {
	mu     sync.Mutex
	token  string
	expiry time.Time
	instID int64
}

// NewTokenCache creates an empty TokenCache.
func NewTokenCache() *TokenCache {
	return &TokenCache{}
}

// Get returns the cached token if it exists, belongs to the given installation,
// and has not expired. Returns ("", false) on cache miss.
func (tc *TokenCache) Get(installationID int64) (string, bool) {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	if tc.instID != installationID || tc.token == "" {
		return "", false
	}
	if time.Now().After(tc.expiry) {
		// Token expired — clear it.
		tc.token = ""
		tc.instID = 0
		return "", false
	}
	return tc.token, true
}

// Set stores a token with its expiry time for the given installation.
func (tc *TokenCache) Set(installationID int64, token string, expiresAt time.Time) {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	tc.instID = installationID
	tc.token = token
	tc.expiry = expiresAt
}

// Invalidate clears the cached token. Called on Disconnect or PEM update.
func (tc *TokenCache) Invalidate() {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	tc.token = ""
	tc.instID = 0
	tc.expiry = time.Time{}
}

// CachedApp wraps an *App with transparent installation token caching.
// The service layer calls GetInstallationToken exactly as before — caching is invisible.
type CachedApp struct {
	*App
	cache *TokenCache
}

// NewCachedApp creates a CachedApp that caches tokens using the provided TokenCache.
func NewCachedApp(app *App, cache *TokenCache) *CachedApp {
	return &CachedApp{App: app, cache: cache}
}

// GetInstallationToken returns a cached token if available, otherwise fetches
// a new one from GitHub and caches it with a 55-minute TTL.
func (ca *CachedApp) GetInstallationToken(ctx context.Context, installationID int64) (string, error) {
	if tok, ok := ca.cache.Get(installationID); ok {
		slog.Debug("github: installation token cache hit",
			"installation_id", installationID,
		)
		return tok, nil
	}

	slog.Debug("github: installation token cache miss, fetching from GitHub",
		"installation_id", installationID,
	)

	tok, err := ca.App.GetInstallationToken(ctx, installationID)
	if err != nil {
		return "", err
	}

	ca.cache.Set(installationID, tok, time.Now().Add(60*time.Minute-tokenSafetyMargin))
	return tok, nil
}

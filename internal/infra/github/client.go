// Package github provides the raw GitHub API calls Trangly needs.
// No SDK is used — only direct HTTP calls.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// httpClient is shared across all GitHub API calls. Never use http.DefaultClient.
var httpClient = &http.Client{
	Timeout: 10 * time.Second,
}

// maxErrorBody is the maximum bytes read from error response bodies.
const maxErrorBody = 1024

// newAPIError creates an APIError from an HTTP response, parsing Retry-After for 429 responses.
func newAPIError(resp *http.Response, body []byte) *APIError {
	e := &APIError{Status: resp.StatusCode, Body: string(body)}
	if resp.StatusCode == http.StatusTooManyRequests {
		e.RetryAfter = parseRetryAfter(resp.Header.Get("Retry-After"))
	}
	return e
}

// parseRetryAfter parses the Retry-After header value as seconds.
// Returns 0 on empty or unparseable values.
func parseRetryAfter(val string) time.Duration {
	if val == "" {
		return 0
	}
	secs, err := strconv.Atoi(val)
	if err != nil || secs <= 0 {
		return 0
	}
	return time.Duration(secs) * time.Second
}

// AppCredentials holds the result of the App Manifest conversion exchange.
type AppCredentials struct {
	AppID         int64  `json:"id"`
	AppSlug       string `json:"slug"`
	WebhookSecret string `json:"webhook_secret"`
	PEM           string `json:"pem"`
}

// Repo is a minimal GitHub repository view.
type Repo struct {
	FullName      string `json:"full_name"`
	DefaultBranch string `json:"default_branch"`
	Private       bool   `json:"private"`
}

// setHeaders sets the common headers required by all GitHub API calls.
func setHeaders(req *http.Request, authToken string) {
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Trangly")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
	}
}

// ExchangeManifest converts a GitHub App manifest setup code into full app credentials.
// Called once during the GitHub magic-link callback flow.
func ExchangeManifest(ctx context.Context, code string) (*AppCredentials, error) {
	url := fmt.Sprintf("https://api.github.com/app-manifests/%s/conversions", code)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return nil, fmt.Errorf("github: ExchangeManifest request: %w", err)
	}
	setHeaders(req, "")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github: ExchangeManifest: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return nil, newAPIError(resp, body)
	}

	creds := &AppCredentials{}
	if err := json.NewDecoder(resp.Body).Decode(creds); err != nil {
		return nil, fmt.Errorf("github: ExchangeManifest decode: %w", err)
	}
	return creds, nil
}

// ListRepos returns the repositories accessible to an installation access token.
func ListRepos(ctx context.Context, token string) ([]Repo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.github.com/installation/repositories?per_page=100", nil)
	if err != nil {
		return nil, fmt.Errorf("github: ListRepos request: %w", err)
	}
	setHeaders(req, token)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github: ListRepos: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return nil, newAPIError(resp, body)
	}

	var result struct {
		Repositories []Repo `json:"repositories"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("github: ListRepos decode: %w", err)
	}
	return result.Repositories, nil
}

// Branch is a minimal GitHub branch view.
type Branch struct {
	Name      string `json:"name"`
	Protected bool   `json:"protected"`
}

// ContentEntry is a single item from the GitHub repository contents API.
type ContentEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Type string `json:"type"` // "file" or "dir"
}

// ListBranches returns the branches of a repository.
func ListBranches(ctx context.Context, token, repoFullName string) ([]Branch, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/branches?per_page=100", repoFullName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("github: ListBranches request: %w", err)
	}
	setHeaders(req, token)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github: ListBranches: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return nil, newAPIError(resp, body)
	}

	var branches []Branch
	if err := json.NewDecoder(resp.Body).Decode(&branches); err != nil {
		return nil, fmt.Errorf("github: ListBranches decode: %w", err)
	}
	return branches, nil
}

// ListContents returns the contents of a directory in a repository at a given ref.
// path is relative to the repo root (e.g. "" or "." for root).
func ListContents(ctx context.Context, token, repoFullName, path, ref string) ([]ContentEntry, error) {
	if path == "" || path == "." {
		path = ""
	}
	url := fmt.Sprintf("https://api.github.com/repos/%s/contents/%s", repoFullName, path)
	if ref != "" {
		url += "?ref=" + ref
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("github: ListContents request: %w", err)
	}
	setHeaders(req, token)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github: ListContents: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return nil, newAPIError(resp, body)
	}

	var entries []ContentEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, fmt.Errorf("github: ListContents decode: %w", err)
	}
	return entries, nil
}

// SetCommitStatus sets the commit status on a specific SHA via the GitHub Statuses API.
// state must be one of: "pending", "success", "failure", "error".
func SetCommitStatus(ctx context.Context, token, repoFullName, sha, state, description string) error {
	payload := map[string]string{
		"state":       state,
		"description": description,
		"context":     "Trangly/deploy",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("github: SetCommitStatus marshal: %w", err)
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/statuses/%s", repoFullName, sha)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("github: SetCommitStatus request: %w", err)
	}
	setHeaders(req, token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("github: SetCommitStatus: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return newAPIError(resp, b)
	}
	return nil
}

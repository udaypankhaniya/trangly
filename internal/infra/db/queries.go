package db

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrNotFound is returned when a query finds no matching row.
var ErrNotFound = errors.New("db: not found")

// ---- Users ----

func (db *DB) InsertUser(id, username, email, passwordHash string) error {
	_, err := db.Exec(
		`INSERT INTO users (id, username, email, password_hash, created_at) VALUES (?, ?, ?, ?, ?)`,
		id, username, email, passwordHash, time.Now().UTC(),
	)
	return err
}

func (db *DB) GetUserByUsername(username string) (*UserRow, error) {
	row := db.QueryRow(`SELECT id, username, email, password_hash, created_at FROM users WHERE username = ?`, username)
	u := &UserRow{}
	err := row.Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return u, err
}

func (db *DB) UserExists() (bool, error) {
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count)
	return count > 0, err
}

// GetUserByID fetches a user row by primary key.
// Used by profile endpoints where the caller is identified by JWT (UserID claim).
func (db *DB) GetUserByID(id string) (*UserRow, error) {
	row := db.QueryRow(`SELECT id, username, email, password_hash, created_at FROM users WHERE id = ?`, id)
	u := &UserRow{}
	err := row.Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return u, err
}

// UpdateUserProfile updates the username and email for the given user ID.
// The username column has a UNIQUE constraint; callers must handle the resulting error.
func (db *DB) UpdateUserProfile(id, username, email string) error {
	_, err := db.Exec(
		`UPDATE users SET username = ?, email = ? WHERE id = ?`,
		username, email, id,
	)
	return err
}

// UpdateUserPassword replaces the bcrypt hash for the given user ID.
func (db *DB) UpdateUserPassword(id, passwordHash string) error {
	_, err := db.Exec(
		`UPDATE users SET password_hash = ? WHERE id = ?`,
		passwordHash, id,
	)
	return err
}

// ---- JWT Secret ----

func (db *DB) UpsertJWTSecret(secretHex string) error {
	_, err := db.Exec(
		`INSERT INTO jwt_secrets (id, secret_hex, created_at) VALUES (1, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET secret_hex = excluded.secret_hex`,
		secretHex, time.Now().UTC(),
	)
	return err
}

func (db *DB) GetJWTSecret() (*JWTSecretRow, error) {
	row := db.QueryRow(`SELECT secret_hex, created_at FROM jwt_secrets WHERE id = 1`)
	s := &JWTSecretRow{}
	err := row.Scan(&s.SecretHex, &s.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return s, err
}

// ---- GitHub App ----

func (db *DB) UpsertGitHubApp(appID int64, appSlug string, webhookSecret, pem []byte) error {
	now := time.Now().UTC()
	_, err := db.Exec(
		`INSERT INTO github_app (id, app_id, app_slug, webhook_secret, pem, created_at, updated_at)
		 VALUES (1, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   app_id = excluded.app_id,
		   app_slug = excluded.app_slug,
		   webhook_secret = excluded.webhook_secret,
		   pem = excluded.pem,
		   updated_at = excluded.updated_at`,
		appID, appSlug, webhookSecret, pem, now, now,
	)
	return err
}

func (db *DB) SetGitHubInstallationID(installationID int64) error {
	_, err := db.Exec(
		`UPDATE github_app SET installation_id = ?, updated_at = ? WHERE id = 1`,
		installationID, time.Now().UTC(),
	)
	return err
}

func (db *DB) DeleteGitHubApp() error {
	_, err := db.Exec(`DELETE FROM github_app WHERE id = 1`)
	return err
}

func (db *DB) GetGitHubApp() (*GitHubAppRow, error) {
	row := db.QueryRow(
		`SELECT app_id, app_slug, webhook_secret, pem, installation_id, created_at, updated_at
		 FROM github_app WHERE id = 1`,
	)
	a := &GitHubAppRow{}
	var installID sql.NullInt64
	err := row.Scan(&a.AppID, &a.AppSlug, &a.WebhookSecret, &a.PEM,
		&installID, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if installID.Valid {
		a.InstallationID = installID.Int64
	}
	return a, err
}

// ---- State Tokens ----

func (db *DB) InsertStateToken(token string, expiresAt time.Time) error {
	_, err := db.Exec(
		`INSERT INTO state_tokens (token, expires_at, used) VALUES (?, ?, 0)`,
		token, expiresAt.UTC(),
	)
	return err
}

// ConsumeStateToken validates that the token exists, is not used, and has not expired.
// On success it marks the token as used (single-use semantics) and returns nil.
func (db *DB) ConsumeStateToken(token string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	var used bool
	var expiresAt time.Time
	err = tx.QueryRow(
		`SELECT used, expires_at FROM state_tokens WHERE token = ?`, token,
	).Scan(&used, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("state token not found")
	}
	if err != nil {
		return err
	}
	if used {
		return fmt.Errorf("state token already used")
	}
	if time.Now().UTC().After(expiresAt) {
		return fmt.Errorf("state token expired")
	}

	_, err = tx.Exec(`UPDATE state_tokens SET used = 1 WHERE token = ?`, token)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// PurgeExpiredStateTokens deletes all expired or used state tokens.
func (db *DB) PurgeExpiredStateTokens() error {
	_, err := db.Exec(
		`DELETE FROM state_tokens WHERE used = 1 OR expires_at < ?`,
		time.Now().UTC(),
	)
	return err
}

// ---- Projects ----

func (db *DB) InsertProject(p *ProjectRow) error {
	now := time.Now().UTC()
	_, err := db.Exec(
		`INSERT INTO projects
		   (id, name, repo_full_name, default_branch, compose_path,
		    mem_limit_mb, deploy_mode, webhook_secret, env_vars, installation_id, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, p.RepoFullName, p.DefaultBranch, p.ComposePath,
		p.MemLimitMB, p.DeployMode, p.WebhookSecret, p.EnvVars, p.InstallationID, now, now,
	)
	return err
}

func (db *DB) UpdateProject(p *ProjectRow) error {
	_, err := db.Exec(
		`UPDATE projects SET
		   name = ?, default_branch = ?, compose_path = ?,
		   mem_limit_mb = ?, deploy_mode = ?, env_vars = ?, updated_at = ?
		 WHERE id = ?`,
		p.Name, p.DefaultBranch, p.ComposePath,
		p.MemLimitMB, p.DeployMode, p.EnvVars, time.Now().UTC(), p.ID,
	)
	return err
}

func (db *DB) DeleteProject(id string) error {
	_, err := db.Exec(`DELETE FROM projects WHERE id = ?`, id)
	return err
}

func (db *DB) GetProject(id string) (*ProjectRow, error) {
	row := db.QueryRow(
		`SELECT id, name, repo_full_name, default_branch, compose_path,
		        mem_limit_mb, deploy_mode, webhook_secret, env_vars, installation_id, created_at, updated_at
		 FROM projects WHERE id = ?`, id,
	)
	return scanProject(row)
}

func (db *DB) GetProjectByRepo(repoFullName string) (*ProjectRow, error) {
	row := db.QueryRow(
		`SELECT id, name, repo_full_name, default_branch, compose_path,
		        mem_limit_mb, deploy_mode, webhook_secret, env_vars, installation_id, created_at, updated_at
		 FROM projects WHERE repo_full_name = ?`, repoFullName,
	)
	return scanProject(row)
}

func (db *DB) ListProjects() ([]*ProjectRow, error) {
	rows, err := db.Query(
		`SELECT id, name, repo_full_name, default_branch, compose_path,
		        mem_limit_mb, deploy_mode, webhook_secret, env_vars, installation_id, created_at, updated_at
		 FROM projects ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*ProjectRow
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, p)
	}
	return list, rows.Err()
}

type projectScanner interface {
	Scan(dest ...any) error
}

func scanProject(r projectScanner) (*ProjectRow, error) {
	p := &ProjectRow{}
	err := r.Scan(
		&p.ID, &p.Name, &p.RepoFullName, &p.DefaultBranch, &p.ComposePath,
		&p.MemLimitMB, &p.DeployMode, &p.WebhookSecret, &p.EnvVars, &p.InstallationID,
		&p.CreatedAt, &p.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return p, err
}

// ---- Deploy Jobs ----

func (db *DB) InsertJob(j *DeployJobRow) error {
	_, err := db.Exec(
		`INSERT INTO deploy_jobs
		   (id, project_id, commit_sha, branch, status, worker_id,
		    ram_estimate_mb, peak_rss_mb, estimate_source, hold_reason,
		    log_path, queued_at, held_at, started_at, finished_at, error)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		j.ID, j.ProjectID, j.CommitSHA, j.Branch, j.Status, j.WorkerID,
		j.RAMEstimateMB, j.PeakRSSMB, j.EstimateSource, j.HoldReason,
		j.LogPath, j.QueuedAt, j.HeldAt, j.StartedAt, j.FinishedAt, j.Error,
	)
	return err
}

func (db *DB) UpdateJobStatus(id, status string) error {
	_, err := db.Exec(
		`UPDATE deploy_jobs SET status = ? WHERE id = ?`, status, id,
	)
	return err
}

func (db *DB) UpdateJobStarted(id string, workerID int, startedAt time.Time) error {
	_, err := db.Exec(
		`UPDATE deploy_jobs SET status = 'running', worker_id = ?, started_at = ? WHERE id = ?`,
		workerID, startedAt.UTC(), id,
	)
	return err
}

func (db *DB) UpdateJobFinished(id, status, errMsg string, finishedAt time.Time) error {
	var errPtr *string
	if errMsg != "" {
		errPtr = &errMsg
	}
	_, err := db.Exec(
		`UPDATE deploy_jobs SET status = ?, finished_at = ?, error = ? WHERE id = ?`,
		status, finishedAt.UTC(), errPtr, id,
	)
	return err
}

func (db *DB) UpdateJobHeld(id, holdReason string, heldAt time.Time) error {
	_, err := db.Exec(
		`UPDATE deploy_jobs SET status = 'held', hold_reason = ?, held_at = ? WHERE id = ?`,
		holdReason, heldAt.UTC(), id,
	)
	return err
}

func (db *DB) SetJobLogPath(id, logPath string) error {
	_, err := db.Exec(`UPDATE deploy_jobs SET log_path = ? WHERE id = ?`, logPath, id)
	return err
}

func (db *DB) UpdateJobCommitSHA(id, sha string) error {
	_, err := db.Exec(`UPDATE deploy_jobs SET commit_sha = ? WHERE id = ?`, sha, id)
	return err
}

func (db *DB) GetJob(id string) (*DeployJobRow, error) {
	row := db.QueryRow(
		`SELECT id, project_id, commit_sha, branch, status, worker_id,
		        ram_estimate_mb, peak_rss_mb, estimate_source, hold_reason,
		        log_path, queued_at, held_at, started_at, finished_at, error
		 FROM deploy_jobs WHERE id = ?`, id,
	)
	return scanJob(row)
}

func (db *DB) ListJobsByProject(projectID string, limit int) ([]*DeployJobRow, error) {
	rows, err := db.Query(
		`SELECT id, project_id, commit_sha, branch, status, worker_id,
		        ram_estimate_mb, peak_rss_mb, estimate_source, hold_reason,
		        log_path, queued_at, held_at, started_at, finished_at, error
		 FROM deploy_jobs WHERE project_id = ?
		 ORDER BY queued_at DESC LIMIT ?`, projectID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*DeployJobRow
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, j)
	}
	return list, rows.Err()
}

// ListActiveJobs returns all jobs that are not in a terminal state, across all projects.
func (db *DB) ListActiveJobs() ([]*DeployJobRow, error) {
	rows, err := db.Query(
		`SELECT id, project_id, commit_sha, branch, status, worker_id,
		        ram_estimate_mb, peak_rss_mb, estimate_source, hold_reason,
		        log_path, queued_at, held_at, started_at, finished_at, error
		 FROM deploy_jobs
		 WHERE status NOT IN ('success', 'failed')
		 ORDER BY queued_at ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*DeployJobRow
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, j)
	}
	return list, rows.Err()
}

// LastFinishedJobs returns the last N successfully finished jobs for a project.
// Used by the scheduler to compute the adaptive RAM estimate.
func (db *DB) LastFinishedJobs(projectID string, n int) ([]*DeployJobRow, error) {
	rows, err := db.Query(
		`SELECT id, project_id, commit_sha, branch, status, worker_id,
		        ram_estimate_mb, peak_rss_mb, estimate_source, hold_reason,
		        log_path, queued_at, held_at, started_at, finished_at, error
		 FROM deploy_jobs
		 WHERE project_id = ? AND status = 'success' AND peak_rss_mb > 0
		 ORDER BY finished_at DESC LIMIT ?`, projectID, n,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*DeployJobRow
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, j)
	}
	return list, rows.Err()
}

type jobScanner interface {
	Scan(dest ...any) error
}

func scanJob(r jobScanner) (*DeployJobRow, error) {
	j := &DeployJobRow{}
	err := r.Scan(
		&j.ID, &j.ProjectID, &j.CommitSHA, &j.Branch, &j.Status, &j.WorkerID,
		&j.RAMEstimateMB, &j.PeakRSSMB, &j.EstimateSource, &j.HoldReason,
		&j.LogPath, &j.QueuedAt, &j.HeldAt, &j.StartedAt, &j.FinishedAt, &j.Error,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return j, err
}

// GetLastDeployForProject returns the most recent terminal job (success or failed) for a project.
// Returns ErrNotFound if the project has never been deployed.
func (db *DB) GetLastDeployForProject(projectID string) (*DeployJobRow, error) {
	row := db.QueryRow(
		`SELECT id, project_id, commit_sha, branch, status, worker_id,
		        ram_estimate_mb, peak_rss_mb, estimate_source, hold_reason,
		        log_path, queued_at, held_at, started_at, finished_at, error
		 FROM deploy_jobs
		 WHERE project_id = ? AND status IN ('success', 'failed')
		 ORDER BY finished_at DESC LIMIT 1`, projectID,
	)
	return scanJob(row)
}

// ListJobsByProjectPaged returns deploy jobs for a project with offset-based pagination.
// Results are ordered newest-first. Returns (rows, hasMore, error).
func (db *DB) ListJobsByProjectPaged(projectID string, limit, offset int) ([]*DeployJobRow, bool, error) {
	// Fetch one extra row to determine if there are more results.
	rows, err := db.Query(
		`SELECT id, project_id, commit_sha, branch, status, worker_id,
		        ram_estimate_mb, peak_rss_mb, estimate_source, hold_reason,
		        log_path, queued_at, held_at, started_at, finished_at, error
		 FROM deploy_jobs WHERE project_id = ?
		 ORDER BY queued_at DESC LIMIT ? OFFSET ?`, projectID, limit+1, offset,
	)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	var list []*DeployJobRow
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, false, err
		}
		list = append(list, j)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	hasMore := len(list) > limit
	if hasMore {
		list = list[:limit]
	}
	return list, hasMore, nil
}

// ListAllJobsPaged returns deploy jobs across all projects with optional status filter and pagination.
// Results are ordered newest-first. Returns (rows, hasMore, error).
func (db *DB) ListAllJobsPaged(limit, offset int, status string) ([]*DeployJobRow, bool, error) {
	var rows *sql.Rows
	var err error
	if status != "" {
		rows, err = db.Query(
			`SELECT id, project_id, commit_sha, branch, status, worker_id,
			        ram_estimate_mb, peak_rss_mb, estimate_source, hold_reason,
			        log_path, queued_at, held_at, started_at, finished_at, error
			 FROM deploy_jobs WHERE status = ?
			 ORDER BY queued_at DESC LIMIT ? OFFSET ?`, status, limit+1, offset,
		)
	} else {
		rows, err = db.Query(
			`SELECT id, project_id, commit_sha, branch, status, worker_id,
			        ram_estimate_mb, peak_rss_mb, estimate_source, hold_reason,
			        log_path, queued_at, held_at, started_at, finished_at, error
			 FROM deploy_jobs
			 ORDER BY queued_at DESC LIMIT ? OFFSET ?`, limit+1, offset,
		)
	}
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	var list []*DeployJobRow
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, false, err
		}
		list = append(list, j)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	hasMore := len(list) > limit
	if hasMore {
		list = list[:limit]
	}
	return list, hasMore, nil
}

// allowedSortColumns is the whitelist of columns valid for ORDER BY in ListDeploymentsAdvanced.
// Keys are the public API names; values are the SQL expressions used in the query.
var allowedSortColumns = map[string]string{
	"queued_at":   "d.queued_at",
	"status":      "d.status",
	"branch":      "d.branch",
	"commit_sha":  "d.commit_sha",
	"finished_at": "d.finished_at",
}

// ListDeploymentsAdvanced returns deploy jobs with server-side sorting, searching,
// filtering, and pagination. It JOINs the projects table to resolve project names
// for both search and result enrichment. Returns (rows, totalCount, error).
func (db *DB) ListDeploymentsAdvanced(p DeploymentQueryParams) ([]*DeployJobWithProjectRow, int, error) {
	// Validate sort column against whitelist — never interpolate user input.
	sortCol, ok := allowedSortColumns[p.SortBy]
	if !ok {
		sortCol = "d.queued_at"
	}
	sortDir := "DESC"
	if p.SortDir == "ASC" {
		sortDir = "ASC"
	}

	// Build WHERE clause parts.
	var whereParts []string
	var args []any

	if p.ProjectID != "" {
		whereParts = append(whereParts, "d.project_id = ?")
		args = append(args, p.ProjectID)
	}
	if p.Status != "" {
		whereParts = append(whereParts, "d.status = ?")
		args = append(args, p.Status)
	}
	if p.Search != "" {
		like := "%" + p.Search + "%"
		whereParts = append(whereParts, "(d.commit_sha LIKE ? OR d.branch LIKE ? OR d.error LIKE ? OR p.name LIKE ? OR p.repo_full_name LIKE ?)")
		args = append(args, like, like, like, like, like)
	}

	where := ""
	if len(whereParts) > 0 {
		where = " WHERE " + whereParts[0]
		for _, wp := range whereParts[1:] {
			where += " AND " + wp
		}
	}

	fromClause := " FROM deploy_jobs d JOIN projects p ON d.project_id = p.id"

	// Count total matching rows.
	var total int
	countQuery := "SELECT COUNT(*)" + fromClause + where
	if err := db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count deployments: %w", err)
	}

	// Fetch the page of results.
	selectCols := `d.id, d.project_id, d.commit_sha, d.branch, d.status, d.worker_id,
		d.ram_estimate_mb, d.peak_rss_mb, d.estimate_source, d.hold_reason,
		d.log_path, d.queued_at, d.held_at, d.started_at, d.finished_at, d.error,
		p.name`
	dataQuery := "SELECT " + selectCols + fromClause + where +
		" ORDER BY " + sortCol + " " + sortDir +
		" LIMIT ? OFFSET ?"
	dataArgs := append(args, p.Limit, p.Offset) //nolint:gocritic

	rows, err := db.Query(dataQuery, dataArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("list deployments advanced: %w", err)
	}
	defer rows.Close()

	var list []*DeployJobWithProjectRow
	for rows.Next() {
		j := &DeployJobWithProjectRow{}
		err := rows.Scan(
			&j.ID, &j.ProjectID, &j.CommitSHA, &j.Branch, &j.Status, &j.WorkerID,
			&j.RAMEstimateMB, &j.PeakRSSMB, &j.EstimateSource, &j.HoldReason,
			&j.LogPath, &j.QueuedAt, &j.HeldAt, &j.StartedAt, &j.FinishedAt, &j.Error,
			&j.ProjectName,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("scan deployment row: %w", err)
		}
		list = append(list, j)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

// JobCounts holds aggregate deploy job counts for the stats API.
type JobCounts struct {
	Total   int64
	Success int64
}

// DeployJobCounts returns total and successful deploy job counts.
func (db *DB) DeployJobCounts() (JobCounts, error) {
	var c JobCounts
	err := db.QueryRow(
		`SELECT COUNT(*), COALESCE(SUM(CASE WHEN status = 'success' THEN 1 ELSE 0 END), 0) FROM deploy_jobs`,
	).Scan(&c.Total, &c.Success)
	return c, err
}

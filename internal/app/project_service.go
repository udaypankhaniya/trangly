package app

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/udaypankhaniya/trangly/internal/domain"
	"github.com/udaypankhaniya/trangly/internal/infra/crypto"
	"github.com/udaypankhaniya/trangly/internal/infra/db"
)

// ProjectService manages CRUD operations for projects.
// It encrypts all secrets before writing to the database.
type ProjectService struct {
	db        *db.DB
	masterKey []byte
	log       *slog.Logger
}

// NewProjectService creates a ProjectService.
func NewProjectService(database *db.DB, masterKey []byte) *ProjectService {
	return &ProjectService{
		db:        database,
		masterKey: masterKey,
		log:       slog.Default().With("service", "project"),
	}
}

// Create registers a new project. webhookSecret is the plain-text HMAC secret
// used by GitHub webhooks; it is encrypted before storage.
// envVars is a newline-delimited KEY=VALUE string; encrypted before storage.
func (s *ProjectService) Create(
	name, repoFullName, defaultBranch, composePath string,
	memLimitMB int64,
	deployMode string,
	webhookSecret string,
	installationID int64,
	envVars string,
) (*domain.Project, error) {
	encWebhook, err := s.encrypt([]byte(webhookSecret))
	if err != nil {
		return nil, fmt.Errorf("project: encrypt webhook secret: %w", err)
	}

	var encEnv []byte
	if envVars != "" {
		encEnv, err = s.encrypt([]byte(envVars))
		if err != nil {
			return nil, fmt.Errorf("project: encrypt env vars: %w", err)
		}
	}

	if deployMode == "" {
		deployMode = domain.DeployModeRebuild
	}

	id, err := newID()
	if err != nil {
		return nil, err
	}

	row := &db.ProjectRow{
		ID:             id,
		Name:           name,
		RepoFullName:   repoFullName,
		DefaultBranch:  defaultBranch,
		ComposePath:    composePath,
		MemLimitMB:     memLimitMB,
		DeployMode:     deployMode,
		WebhookSecret:  encWebhook,
		EnvVars:        encEnv,
		InstallationID: installationID,
	}
	if err := s.db.InsertProject(row); err != nil {
		return nil, fmt.Errorf("project: insert: %w", err)
	}
	s.log.Info("project created", "id", id, "repo", repoFullName, "deploy_mode", deployMode)
	return s.rowToDomain(row, webhookSecret)
}

// Update modifies mutable fields of a project. WebhookSecret is not changed here;
// env vars may be provided (plain-text) and will be encrypted.
func (s *ProjectService) Update(id, name, defaultBranch, composePath string, memLimitMB int64, deployMode string, envVars string) (*domain.Project, error) {
	existing, err := s.db.GetProject(id)
	if errors.Is(err, db.ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("project: get: %w", err)
	}

	var encEnv []byte
	if envVars != "" {
		encEnv, err = s.encrypt([]byte(envVars))
		if err != nil {
			return nil, fmt.Errorf("project: encrypt env vars: %w", err)
		}
	} else {
		encEnv = existing.EnvVars
	}

	if deployMode == "" {
		deployMode = existing.DeployMode
	}

	existing.Name = name
	existing.DefaultBranch = defaultBranch
	existing.ComposePath = composePath
	existing.MemLimitMB = memLimitMB
	existing.DeployMode = deployMode
	existing.EnvVars = encEnv
	existing.UpdatedAt = time.Now()

	if err := s.db.UpdateProject(existing); err != nil {
		return nil, fmt.Errorf("project: update: %w", err)
	}
	return s.rowToDomain(existing, "")
}

// Delete removes a project and all its associated deploy jobs (via CASCADE).
func (s *ProjectService) Delete(id string) error {
	if err := s.db.DeleteProject(id); err != nil {
		return fmt.Errorf("project: delete: %w", err)
	}
	s.log.Info("project deleted", "id", id)
	return nil
}

// GetByID returns a project, decrypting credentials.
func (s *ProjectService) GetByID(id string) (*domain.Project, error) {
	row, err := s.db.GetProject(id)
	if errors.Is(err, db.ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("project: get: %w", err)
	}
	var webhookSecret string
	if len(row.WebhookSecret) > 0 {
		plain, err := s.decrypt(row.WebhookSecret)
		if err != nil {
			return nil, fmt.Errorf("project: decrypt webhook secret: %w", err)
		}
		webhookSecret = string(plain)
	}
	return s.rowToDomain(row, webhookSecret)
}

// GetByRepo looks up a project by its GitHub full repo name.
func (s *ProjectService) GetByRepo(repoFullName string) (*domain.Project, error) {
	row, err := s.db.GetProjectByRepo(repoFullName)
	if errors.Is(err, db.ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("project: get by repo: %w", err)
	}
	var webhookSecret string
	if len(row.WebhookSecret) > 0 {
		plain, err := s.decrypt(row.WebhookSecret)
		if err != nil {
			return nil, fmt.Errorf("project: decrypt webhook secret: %w", err)
		}
		webhookSecret = string(plain)
	}
	return s.rowToDomain(row, webhookSecret)
}

// List returns all projects. Credentials are NOT decrypted in list results.
func (s *ProjectService) List() ([]*domain.Project, error) {
	rows, err := s.db.ListProjects()
	if err != nil {
		return nil, fmt.Errorf("project: list: %w", err)
	}
	projects := make([]*domain.Project, 0, len(rows))
	for _, r := range rows {
		p, err := s.rowToDomain(r, "")
		if err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	return projects, nil
}

// ErrNotFound is returned when a requested resource does not exist.
var ErrNotFound = errors.New("app: not found")

// EnvVar is a single decrypted environment variable key/value pair.
type EnvVar struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// GetEnvVars decrypts and returns the project's stored env vars as individual
// key/value pairs. Returns an empty slice when no vars are stored.
func (s *ProjectService) GetEnvVars(id string) ([]EnvVar, error) {
	row, err := s.db.GetProject(id)
	if errors.Is(err, db.ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("project: get: %w", err)
	}
	if len(row.EnvVars) == 0 {
		return []EnvVar{}, nil
	}
	plain, err := s.decrypt(row.EnvVars)
	if err != nil {
		return nil, fmt.Errorf("project: decrypt env vars: %w", err)
	}
	var vars []EnvVar
	for _, line := range strings.Split(string(plain), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			vars = append(vars, EnvVar{Key: parts[0], Value: parts[1]})
		}
	}
	if vars == nil {
		return []EnvVar{}, nil
	}
	return vars, nil
}

func (s *ProjectService) encrypt(plain []byte) ([]byte, error) {
	return crypto.Encrypt(s.masterKey, plain)
}

func (s *ProjectService) decrypt(enc []byte) ([]byte, error) {
	return crypto.Decrypt(s.masterKey, enc)
}

func (s *ProjectService) rowToDomain(r *db.ProjectRow, webhookSecret string) (*domain.Project, error) {
	deployMode := r.DeployMode
	if deployMode == "" {
		deployMode = domain.DeployModeRebuild
	}
	return &domain.Project{
		ID:             r.ID,
		Name:           r.Name,
		RepoFullName:   r.RepoFullName,
		DefaultBranch:  r.DefaultBranch,
		ComposePath:    r.ComposePath,
		MemLimitMB:     r.MemLimitMB,
		DeployMode:     deployMode,
		WebhookSecret:  webhookSecret,
		InstallationID: r.InstallationID,
		CreatedAt:      r.CreatedAt,
		UpdatedAt:      r.UpdatedAt,
	}, nil
}

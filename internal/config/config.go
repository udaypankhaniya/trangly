// Package config provides the runtime configuration for Trangly.
// All paths are derived from AppDataDir (defined in pkg/version) — never hardcoded.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/udaypankhaniya/trangly/pkg/version"
)

// Config holds all runtime configuration values resolved at startup.
type Config struct {
	// DataDir is the root directory for all Trangly data (~/.Trangly by default).
	DataDir string
	// Port is the TCP port the HTTP server listens on.
	Port int
	// DBPath is the absolute path to the SQLite database file.
	DBPath string
	// MasterKeyPath is the absolute path to the AES-256 master key file.
	MasterKeyPath string
	// LogsDir is the directory where per-job log files are stored.
	LogsDir string
	// WorkspacesDir is the directory where git workspaces are cloned.
	WorkspacesDir string
}

// Load resolves configuration from the given data directory and port.
// If dataDir is empty, it defaults to version.AppDataDir (~/.Trangly) with
// the home directory expanded.
func Load(dataDir string, port int) (Config, error) {
	if dataDir == "" {
		dataDir = version.AppDataDir
	}
	dataDir = expandHome(dataDir)

	if port == 0 {
		port = version.AppPort
	}

	// Create all required directories up front so later code can assume they exist.
	dirs := []string{
		dataDir,
		filepath.Join(dataDir, "logs"),
		filepath.Join(dataDir, "workspaces"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0700); err != nil {
			return Config{}, fmt.Errorf("config: create dir %s: %w", d, err)
		}
	}

	return Config{
		DataDir:       dataDir,
		Port:          port,
		DBPath:        filepath.Join(dataDir, "config.db"),
		MasterKeyPath: filepath.Join(dataDir, "master.key"),
		LogsDir:       filepath.Join(dataDir, "logs"),
		WorkspacesDir: filepath.Join(dataDir, "workspaces"),
	}, nil
}

// expandHome replaces a leading "~" with the user's home directory.
func expandHome(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[1:])
}

package deploy

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// HealthMode describes the auto-detected health check strategy for a project.
type HealthMode struct {
	Mode       string // one of domain.HealthMode* constants
	Port       int    // exposed port (TCP/HTTP modes)
	HTTPPaths  []string
	UseCompose bool // true if the compose file has a native healthcheck directive
}

// DetectMode parses the docker-compose.yml at composePath and determines the
// appropriate health check mode: HTTP, TCP, or None.
//
// Detection logic (applied to the first service with an exposed port):
//  1. If the service has a `healthcheck` directive → UseCompose = true, Mode = HTTP/TCP
//  2. If port is exposed, no healthcheck key → TCP probe + HTTP path probe
//  3. No ports → Mode = None, emit warning
func DetectMode(workspaceDir, composePath string) (HealthMode, error) {
	fullPath := filepath.Join(workspaceDir, composePath)
	data, err := os.ReadFile(fullPath) //nolint:gosec
	if err != nil {
		return HealthMode{Mode: "none"}, fmt.Errorf("healthcheck_detect: read compose file: %w", err)
	}

	var compose composeFile
	if err := yaml.Unmarshal(data, &compose); err != nil {
		return HealthMode{Mode: "none"}, fmt.Errorf("healthcheck_detect: parse compose file: %w", err)
	}

	for _, svc := range compose.Services {
		port := firstExposedPort(svc.Ports)

		// Native healthcheck defined in compose.
		if svc.Healthcheck != nil && !svc.Healthcheck.Disable {
			mode := "tcp"
			if port > 0 {
				mode = "http"
			}
			return HealthMode{
				Mode:       mode,
				Port:       port,
				HTTPPaths:  []string{"/health", "/healthz", "/ping"},
				UseCompose: true,
			}, nil
		}

		if port > 0 {
			return HealthMode{
				Mode:      "http",
				Port:      port,
				HTTPPaths: []string{"/health", "/healthz", "/ping"},
			}, nil
		}
	}

	return HealthMode{Mode: "none"}, nil
}

// --- YAML parsing types ---

type composeFile struct {
	Services map[string]composeService `yaml:"services"`
}

type composeService struct {
	Ports       []any               `yaml:"ports"`
	Healthcheck *composeHealthcheck `yaml:"healthcheck"`
}

type composeHealthcheck struct {
	Test     any    `yaml:"test"`
	Interval string `yaml:"interval"`
	Timeout  string `yaml:"timeout"`
	Retries  int    `yaml:"retries"`
	Disable  bool   `yaml:"disable"`
}

// firstExposedPort extracts the first host-mapped port number from a compose ports list.
// Ports can be strings ("8080:80", "80") or maps (long syntax).
func firstExposedPort(ports []any) int {
	for _, p := range ports {
		switch v := p.(type) {
		case string:
			port := parseFirstPort(v)
			if port > 0 {
				return port
			}
		case map[string]any:
			if target, ok := v["target"]; ok {
				switch n := target.(type) {
				case int:
					return n
				case float64:
					return int(n)
				}
			}
		}
	}
	return 0
}

// parseFirstPort parses the host port from strings like "8080:80" or "80".
func parseFirstPort(s string) int {
	// Try "host:container" format.
	for i, c := range s {
		if c == ':' {
			port := atoi(s[:i])
			if port > 0 {
				return port
			}
			return atoi(s[i+1:])
		}
	}
	return atoi(s)
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

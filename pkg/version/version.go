// Package version is the single source of truth for all app-wide constants.
// It must have ZERO imports from internal/ — any package can import it safely.
package version

// Build-time variables injected via ldflags:
//
//	go build -ldflags "-X github.com/udaypankhaniya/trangly/pkg/version.Version=1.0.0 \
//	                    -X github.com/udaypankhaniya/trangly/pkg/version.Commit=abc1234 \
//	                    -X github.com/udaypankhaniya/trangly/pkg/version.BuildDate=2025-01-01"
var (
	Version   = "dev"            // semantic version e.g. "1.0.0"
	Commit    = "udaypankhaniya" // short git SHA e.g. "abc1234"
	BuildDate = "2026-04-02"     // ISO date e.g. "2025-01-01"
)

const (
	AppName        = "Trangly"
	AppSlug        = "trangly"
	AppDescription = "Lightweight self-hosted CI/CD for Docker projects"
	AppURL         = "https://trangly.dev"
	AppPort        = 2880
	AppDataDir     = "~/.trangly"
	AppBinary      = "trangly"
)

// Info returns a structured map of all version metadata.
// Served by GET /api/version.
func Info() map[string]string {
	return map[string]string{
		"name":        AppName,
		"version":     Version,
		"commit":      Commit,
		"build_date":  BuildDate,
		"description": AppDescription,
		"url":         AppURL,
	}
}

// String returns the single-line version string shown in CLI output and startup logs.
// Example: "Trangly v1.0.0 (commit abc1234, built 2025-01-01)"
func String() string {
	return AppName + " v" + Version +
		" (commit " + Commit + ", built " + BuildDate + ")"
}

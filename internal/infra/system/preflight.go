package system

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/udaypankhaniya/trangly/pkg/version"
)

// CheckResult is the structured output of a single preflight check.
// The API returns []CheckResult as JSON; the UI renders the live checklist from it.
type CheckResult struct {
	Name   string `json:"name"`
	Status string `json:"status"` // "ok" | "fail" | "warn" | "skip"
	Detail string `json:"detail"`
	Fix    string `json:"fix,omitempty"`
}

// RunChecks executes all 7 preflight checks in order and returns the results.
// The checks are run sequentially because later checks can depend on earlier ones.
func RunChecks(ctx context.Context) []CheckResult {
	results := make([]CheckResult, 0, 7)
	results = append(results, checkOS())
	results = append(results, checkDocker())
	results = append(results, checkDockerCompose())
	results = append(results, checkDockerSocket())
	results = append(results, checkPort(version.AppPort))
	results = append(results, checkPublicReachability(ctx, version.AppPort))
	results = append(results, checkSudo())
	return results
}

// AllPassed returns true if every check in the list is "ok" or "skip".
func AllPassed(results []CheckResult) bool {
	for _, r := range results {
		if r.Status == "fail" {
			return false
		}
	}
	return true
}

// --- individual checks ---

func checkOS() CheckResult {
	if runtime.GOOS == "windows" {
		return CheckResult{
			Name:   "OS supported",
			Status: "warn",
			Detail: "Running on Windows. Production deployments require Linux (Ubuntu, Debian, CentOS, Fedora, or RHEL).",
			Fix:    "Use this machine for development only. Deploy to a Linux VPS for production.",
		}
	}
	d, err := ParseOSRelease()
	if err != nil {
		return CheckResult{
			Name:   "OS supported",
			Status: "fail",
			Detail: "Cannot read /etc/os-release — is this Linux?",
			Fix:    "Trangly requires Linux (Ubuntu, Debian, CentOS, Fedora, or RHEL).",
		}
	}
	if !d.IsSupported() {
		return CheckResult{
			Name:   "OS supported",
			Status: "warn",
			Detail: fmt.Sprintf("%s is not officially supported. Automatic Docker install will be unavailable.", d.Name),
			Fix:    "Install Docker manually using your package manager, then re-check.",
		}
	}
	return CheckResult{Name: "OS supported", Status: "ok", Detail: d.Name}
}

func checkDocker() CheckResult {
	out, err := exec.Command("docker", "version", "--format", "{{.Server.Version}}").Output()
	if err != nil {
		return CheckResult{
			Name:   "Docker installed",
			Status: "fail",
			Detail: "Docker is not installed or not in PATH.",
			Fix:    "Use the install wizard or install Docker manually from https://docs.docker.com/engine/install/",
		}
	}
	v := strings.TrimSpace(string(out))
	return CheckResult{Name: "Docker installed", Status: "ok", Detail: "v" + v}
}

func checkDockerCompose() CheckResult {
	out, err := exec.Command("docker", "compose", "version", "--short").Output()
	if err != nil {
		return CheckResult{
			Name:   "Docker Compose",
			Status: "fail",
			Detail: "Docker Compose V2 CLI plugin is not installed.",
			Fix:    "Trangly can install it automatically — click 'Fix' to proceed.",
		}
	}
	v := strings.TrimSpace(string(out))
	return CheckResult{Name: "Docker Compose", Status: "ok", Detail: "v" + v}
}

func checkDockerSocket() CheckResult {
	// Windows Docker Desktop uses a named pipe instead of a Unix socket.
	if runtime.GOOS == "windows" {
		const pipe = `\\.\pipe\docker_engine`
		f, err := os.OpenFile(pipe, os.O_RDWR, 0)
		if err != nil {
			return CheckResult{
				Name:   "Docker socket",
				Status: "fail",
				Detail: "Cannot access Docker named pipe. Is Docker Desktop running?",
				Fix:    "Start Docker Desktop and ensure it is fully initialised before retrying.",
			}
		}
		f.Close()
		return CheckResult{Name: "Docker socket", Status: "ok", Detail: pipe}
	}

	const sock = "/var/run/docker.sock"
	// Use os.Stat first to distinguish "not found" from "permission denied".
	if _, err := os.Stat(sock); err != nil {
		if os.IsNotExist(err) {
			return CheckResult{
				Name:   "Docker socket",
				Status: "fail",
				Detail: fmt.Sprintf("%s not found. Is Docker daemon running?", sock),
				Fix:    "Ensure Docker daemon is running: sudo systemctl start docker",
			}
		}
		if os.IsPermission(err) {
			return CheckResult{
				Name:   "Docker socket",
				Status: "fail",
				Detail: "Permission denied — current user is not in the docker group.",
				Fix:    "Run: sudo usermod -aG docker $USER  then log out and back in.",
			}
		}
	}
	// Dial the socket — the only correct way to verify it is alive on Linux.
	// os.OpenFile on a Unix socket returns ENXIO ("no such device or address").
	conn, err := net.DialTimeout("unix", sock, 2*time.Second)
	if err != nil {
		if os.IsPermission(err) {
			return CheckResult{
				Name:   "Docker socket",
				Status: "fail",
				Detail: "Permission denied — current user is not in the docker group.",
				Fix:    "Run: sudo usermod -aG docker $USER  then log out and back in.",
			}
		}
		return CheckResult{
			Name:   "Docker socket",
			Status: "fail",
			Detail: fmt.Sprintf("Cannot connect to %s: %v", sock, err),
			Fix:    "Ensure Docker daemon is running: sudo systemctl start docker",
		}
	}
	conn.Close()
	return CheckResult{Name: "Docker socket", Status: "ok", Detail: sock}
}

func checkPort(port int) CheckResult {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return CheckResult{
			Name:   fmt.Sprintf("Port %d available", port),
			Status: "fail",
			Detail: fmt.Sprintf("Port %d is already in use.", port),
			Fix:    fmt.Sprintf("Stop the process using port %d or configure a different port with --port.", port),
		}
	}
	ln.Close()
	return CheckResult{
		Name:   fmt.Sprintf("Port %d available", port),
		Status: "ok",
		Detail: fmt.Sprintf("Port %d is free.", port),
	}
}

func checkPublicReachability(ctx context.Context, port int) CheckResult {
	// On Windows (development mode), skip the reachability check — developers
	// typically use tunnels (ngrok, Cloudflare Tunnel) that cannot be probed
	// from the server side.
	if runtime.GOOS == "windows" {
		return CheckResult{
			Name:   "Public reachability",
			Status: "warn",
			Detail: "Skipped on Windows. Ensure your tunnel (ngrok, Cloudflare Tunnel) is running for GitHub webhook delivery.",
			Fix:    "Use a tunnel like ngrok to expose port " + fmt.Sprintf("%d", port) + " publicly.",
		}
	}

	// Verify outbound internet access by reaching api.github.com (required for
	// GitHub integration regardless). We cannot probe inbound reachability without
	// an external relay service, so we warn the user to open the firewall port
	// themselves rather than blocking startup.
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodHead, "https://api.github.com", nil)
	if err != nil {
		return checkReachabilityFail(port, "Could not construct outbound request.")
	}
	req.Header.Set("User-Agent", version.AppName+"/"+version.Version)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return checkReachabilityFail(port, "No outbound internet access — cannot reach api.github.com.")
	}
	defer resp.Body.Close()

	// Outbound works. We cannot verify inbound without a relay, so warn.
	return CheckResult{
		Name:   "Public reachability",
		Status: "warn",
		Detail: fmt.Sprintf("Outbound internet OK. Ensure port %d is open inbound for GitHub webhooks.", port),
		Fix: fmt.Sprintf(
			"Open port %d in your firewall:\n"+
				"  ufw allow %d\n"+
				"Or use a Cloudflare Tunnel if this VPS has no public IP.",
			port, port,
		),
	}
}

func checkReachabilityFail(port int, detail string) CheckResult {
	return CheckResult{
		Name:   "Public reachability",
		Status: "fail",
		Detail: detail,
		Fix: fmt.Sprintf(
			"Options:\n"+
				"1. Open port %d in your firewall (ufw allow %d or iptables -A INPUT -p tcp --dport %d -j ACCEPT)\n"+
				"2. Set up a Cloudflare Tunnel — works without a public IP\n"+
				"3. Check your VPS provider's security group rules",
			port, port, port,
		),
	}
}

func checkSudo() CheckResult {
	// sudo does not exist on Windows — skip this check entirely.
	if runtime.GOOS == "windows" {
		return CheckResult{
			Name:   "Sudo access",
			Status: "skip",
			Detail: "Not applicable on Windows.",
		}
	}
	err := exec.Command("sudo", "-n", "true").Run()
	if err != nil {
		return CheckResult{
			Name:   "Sudo access",
			Status: "warn",
			Detail: "Passwordless sudo is not available.",
			Fix:    "Sudo is only required if Docker needs to be installed. If Docker is already running, this warning can be ignored.",
		}
	}
	return CheckResult{Name: "Sudo access", Status: "ok", Detail: "Passwordless sudo available."}
}

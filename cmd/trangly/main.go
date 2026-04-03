package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/udaypankhaniya/trangly/internal/app"
	"github.com/udaypankhaniya/trangly/internal/bootstrap"
	"github.com/udaypankhaniya/trangly/internal/config"
	"github.com/udaypankhaniya/trangly/internal/infra/db"
	githubapi "github.com/udaypankhaniya/trangly/internal/infra/github"
	"github.com/udaypankhaniya/trangly/internal/infra/system"
	"github.com/udaypankhaniya/trangly/pkg/version"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	var (
		dataDir string
		port    int
		baseURL string
	)

	root := &cobra.Command{
		Use:   version.AppBinary,
		Short: version.AppDescription,
		// Default action when invoked with no subcommand is to run the server.
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServe(dataDir, port, baseURL)
		},
	}

	root.PersistentFlags().StringVar(&dataDir, "data-dir", version.AppDataDir, "data directory for DB, keys, and logs")
	root.PersistentFlags().IntVar(&port, "port", version.AppPort, "TCP port to listen on")
	root.PersistentFlags().StringVar(&baseURL, "base-url", "", "public base URL (e.g. https://yourserver.example.com) used by the GitHub App manifest")

	root.AddCommand(startCmd(&dataDir, &port, &baseURL))
	root.AddCommand(serveCmd(&dataDir, &port, &baseURL))
	root.AddCommand(versionCmd())
	root.AddCommand(checkCmd())
	root.AddCommand(setupCmd(&dataDir))
	root.AddCommand(updatePEMCmd(&dataDir))
	root.AddCommand(resetCmd(&dataDir))
	root.AddCommand(installCmd(&dataDir, &port, &baseURL))
	root.AddCommand(stopCmd())
	root.AddCommand(uninstallCmd())
	root.AddCommand(statusCmd())
	root.AddCommand(serviceLogsCmd())

	return root
}

func startCmd(dataDir *string, port *int, baseURL *string) *cobra.Command {
	return &cobra.Command{
		Use:          "start",
		Short:        "Start the " + version.AppName + " server",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServe(*dataDir, *port, *baseURL)
		},
	}
}

func serveCmd(dataDir *string, port *int, baseURL *string) *cobra.Command {
	return &cobra.Command{
		Use:          "serve",
		Short:        "Start the " + version.AppName + " server (alias for start)",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServe(*dataDir, *port, *baseURL)
		},
	}
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(version.String())
		},
	}
}

func runServe(dataDir string, port int, baseURL string) error {
	// version.String() is printed exactly once at process start.
	fmt.Println(version.String())

	cfg, err := config.Load(dataDir, port)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	return bootstrap.NewApp(cfg, baseURL).Run()
}

// setupCmd interactively collects name, email, and password then creates the
// admin account. Must be run before the first `serve`.
func setupCmd(dataDir *string) *cobra.Command {
	return &cobra.Command{
		Use:          "setup",
		Short:        "Create the admin account (run once before first start)",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSetup(*dataDir)
		},
	}
}

func runSetup(dataDir string) error {
	fmt.Println(version.String())
	fmt.Println()
	fmt.Println("=== " + version.AppName + " First-Run Setup ===")
	fmt.Println("This creates your admin login. Run once before starting the server.")
	fmt.Println()

	reader := bufio.NewReader(os.Stdin)

	// --- username (retry until non-empty) ---
	var username string
	for {
		v, err := prompt(reader, "Admin username")
		if err != nil {
			return err
		}
		if strings.TrimSpace(v) == "" {
			fmt.Println("  Username cannot be empty. Please try again.")
			continue
		}
		username = strings.TrimSpace(v)
		break
	}

	// --- email (retry until non-empty) ---
	var email string
	for {
		v, err := prompt(reader, "Admin email")
		if err != nil {
			return err
		}
		if strings.TrimSpace(v) == "" {
			fmt.Println("  Email cannot be empty. Please try again.")
			continue
		}
		email = strings.TrimSpace(v)
		break
	}

	// --- password + confirm (retry until valid and matching) ---
	var password string
	for {
		pwd, err := promptPassword("Password (min 8 chars)")
		if err != nil {
			return err
		}
		if len(pwd) < 8 {
			fmt.Println("  Password must be at least 8 characters. Please try again.")
			continue
		}
		confirm, err := promptPassword("Confirm password")
		if err != nil {
			return err
		}
		if pwd != confirm {
			fmt.Println("  Passwords do not match. Please try again.")
			continue
		}
		password = pwd
		break
	}

	cfg, err := config.Load(dataDir, version.AppPort)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if err := bootstrap.SetupAdmin(cfg, username, email, password); err != nil {
		return err
	}

	fmt.Println()
	cprintf(colorGreen, "  ✓ Admin account created.\n")
	fmt.Printf("    Username : %s\n", username)
	fmt.Printf("    Email    : %s\n", email)
	fmt.Println()
	fmt.Println("  Port and public URL are configured via the web UI after you start the server.")
	fmt.Println()
	cprintf(colorGreen, "  Next step: sudo %s install\n", version.AppBinary)
	fmt.Println()
	return nil
}

func updatePEMCmd(dataDir *string) *cobra.Command {
	var pemFile string
	cmd := &cobra.Command{
		Use:          "update-pem",
		Short:        "Replace the stored GitHub App private key with a new PEM file",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if pemFile == "" {
				return fmt.Errorf("--file is required")
			}
			cfg, err := config.Load(*dataDir, 0)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			database, err := db.New(cfg.DBPath)
			if err != nil {
				return fmt.Errorf("open database: %w", err)
			}
			defer database.Close()
			masterKey, err := bootstrap.LoadMasterKey(cfg.MasterKeyPath)
			if err != nil {
				return err
			}
			svc := app.NewGitHubAuthService(database, masterKey, githubapi.NewTokenCache())
			if err := svc.UpdatePEM(context.Background(), pemFile); err != nil {
				return err
			}
			fmt.Println("✓ GitHub App PEM updated successfully.")
			return nil
		},
	}
	cmd.Flags().StringVar(&pemFile, "file", "", "path to the new PEM private key file")
	return cmd
}

// --- Reset command ---

func resetCmd(dataDir *string) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:          "reset",
		Short:        "Delete all data (database, master key, logs, workspaces) — factory reset",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReset(*dataDir, force)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "skip interactive confirmation")
	return cmd
}

func runReset(dataDir string, force bool) error {
	cfg, err := config.Load(dataDir, 0)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	fmt.Println("WARNING: This will permanently delete ALL " + version.AppName + " data:")
	fmt.Printf("  Database:    %s\n", cfg.DBPath)
	fmt.Printf("  Master key:  %s\n", cfg.MasterKeyPath)
	fmt.Printf("  Logs:        %s\n", cfg.LogsDir)
	fmt.Printf("  Workspaces:  %s\n", cfg.WorkspacesDir)
	fmt.Println()

	if !force {
		reader := bufio.NewReader(os.Stdin)
		fmt.Print("Type 'yes' to confirm: ")
		line, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("read confirmation: %w", err)
		}
		if strings.TrimSpace(line) != "yes" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	// Remove database file
	if err := os.Remove(cfg.DBPath); err != nil && !os.IsNotExist(err) {
		fmt.Printf("  warning: could not remove %s: %v\n", cfg.DBPath, err)
	} else {
		fmt.Printf("  ✓ Removed %s\n", cfg.DBPath)
	}

	// Remove master key
	if err := os.Remove(cfg.MasterKeyPath); err != nil && !os.IsNotExist(err) {
		fmt.Printf("  warning: could not remove %s: %v\n", cfg.MasterKeyPath, err)
	} else {
		fmt.Printf("  ✓ Removed %s\n", cfg.MasterKeyPath)
	}

	// Remove GitHub App PEM if it exists
	pemPath := filepath.Join(cfg.DataDir, "github-app.pem")
	if err := os.Remove(pemPath); err != nil && !os.IsNotExist(err) {
		fmt.Printf("  warning: could not remove %s: %v\n", pemPath, err)
	}

	// Clear logs directory
	if err := removeContents(cfg.LogsDir); err != nil {
		fmt.Printf("  warning: could not clear logs: %v\n", err)
	} else {
		fmt.Printf("  ✓ Cleared %s\n", cfg.LogsDir)
	}

	// Clear workspaces directory
	if err := removeContents(cfg.WorkspacesDir); err != nil {
		fmt.Printf("  warning: could not clear workspaces: %v\n", err)
	} else {
		fmt.Printf("  ✓ Cleared %s\n", cfg.WorkspacesDir)
	}

	fmt.Println()
	fmt.Println("✓ Factory reset complete.")
	fmt.Printf("  Start fresh with:  %s start\n", version.AppBinary)
	return nil
}

// removeContents deletes all files and directories inside dir, but not dir itself.
func removeContents(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

// --- Service management commands ---

const systemdUnitPath = "/etc/systemd/system/" + version.AppSlug + ".service"

func installCmd(dataDir *string, port *int, baseURL *string) *cobra.Command {
	return &cobra.Command{
		Use:          "install",
		Short:        "Install " + version.AppName + " as a background service (systemd on Linux, NSSM guide on Windows)",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInstall(*dataDir, *port, *baseURL)
		},
	}
}

func runInstall(dataDir string, port int, baseURL string) error {
	binPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}
	binPath, err = filepath.Abs(binPath)
	if err != nil {
		return fmt.Errorf("resolve absolute path: %w", err)
	}

	// Resolve data dir to absolute path so systemd service works regardless of cwd.
	if dataDir == "" || dataDir == version.AppDataDir {
		dataDir = version.AppDataDir
	}
	absDataDir := expandHome(dataDir)
	absDataDir, _ = filepath.Abs(absDataDir)

	// Load setup.conf written by `trangly setup` as fallback for any flags not explicitly set.
	confPort, confURL := loadSetupConf(absDataDir)
	if port == version.AppPort && confPort > 0 {
		port = confPort
	}
	if baseURL == "" && confURL != "" {
		baseURL = confURL
	}

	if port == 0 {
		port = version.AppPort
	}

	switch runtime.GOOS {
	case "linux":
		return installLinuxService(binPath, absDataDir, port, baseURL)
	case "windows":
		return installWindowsGuide(binPath, absDataDir, port, baseURL)
	default:
		return fmt.Errorf("unsupported OS %q — install is supported on linux and windows", runtime.GOOS)
	}
}

func installLinuxService(binPath, dataDir string, port int, baseURL string) error {
	// Check if running as root
	if os.Getuid() != 0 {
		return fmt.Errorf("the install command must be run as root (try: sudo %s install)", version.AppBinary)
	}

	execStart := fmt.Sprintf("%s start --data-dir %s --port %d", binPath, dataDir, port)
	if baseURL != "" {
		execStart += " --base-url " + baseURL
	}

	unit := fmt.Sprintf(`[Unit]
Description=%s — %s
After=network.target docker.service
Requires=docker.service

[Service]
Type=simple
ExecStart=%s
Restart=on-failure
RestartSec=5
StandardOutput=journal
StandardError=journal
SyslogIdentifier=%s

[Install]
WantedBy=multi-user.target
`, version.AppName, version.AppDescription, execStart, version.AppSlug)

	if err := os.WriteFile(systemdUnitPath, []byte(unit), 0644); err != nil {
		return fmt.Errorf("write systemd unit: %w", err)
	}
	fmt.Printf("✓ Created %s\n", systemdUnitPath)

	cmds := []struct {
		label string
		args  []string
	}{
		{"Reloading systemd", []string{"systemctl", "daemon-reload"}},
		{"Enabling service", []string{"systemctl", "enable", version.AppSlug}},
		{"Starting service", []string{"systemctl", "start", version.AppSlug}},
	}
	for _, c := range cmds {
		fmt.Printf("  %s...\n", c.label)
		out, err := exec.Command(c.args[0], c.args[1:]...).CombinedOutput() //nolint:gosec
		if err != nil {
			return fmt.Errorf("%s failed: %s — %w", c.label, string(out), err)
		}
	}

	fmt.Println()
	fmt.Println("✓ " + version.AppName + " installed as a systemd service.")
	fmt.Println("  Service:  " + version.AppSlug)
	fmt.Println("  Status:   sudo systemctl status " + version.AppSlug)
	fmt.Println("  Logs:     sudo journalctl -u " + version.AppSlug + " -f")
	fmt.Println("  Stop:     sudo systemctl stop " + version.AppSlug)
	fmt.Println("  Remove:   sudo " + version.AppBinary + " uninstall")
	return nil
}

func installWindowsGuide(binPath, dataDir string, port int, baseURL string) error {
	serveArgs := fmt.Sprintf("start --data-dir %s --port %d", dataDir, port)
	if baseURL != "" {
		serveArgs += " --base-url " + baseURL
	}

	fmt.Println("To run " + version.AppName + " as a Windows service, use NSSM (https://nssm.cc):")
	fmt.Println()
	fmt.Println("  1. Download NSSM from https://nssm.cc/download")
	fmt.Println("  2. Run the following commands in an elevated (Administrator) terminal:")
	fmt.Println()
	fmt.Printf("     nssm install %s \"%s\" %s\n", version.AppSlug, binPath, serveArgs)
	fmt.Printf("     nssm set %s Description \"%s\"\n", version.AppSlug, version.AppDescription)
	fmt.Printf("     nssm start %s\n", version.AppSlug)
	fmt.Println()
	fmt.Println("  Management commands:")
	fmt.Printf("     nssm status %s\n", version.AppSlug)
	fmt.Printf("     nssm stop %s\n", version.AppSlug)
	fmt.Printf("     nssm remove %s confirm\n", version.AppSlug)
	return nil
}

func uninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "uninstall",
		Short:        "Remove the " + version.AppName + " background service",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			switch runtime.GOOS {
			case "linux":
				return uninstallLinuxService()
			case "windows":
				return uninstallWindowsGuide()
			default:
				return fmt.Errorf("unsupported OS %q", runtime.GOOS)
			}
		},
	}
}

func uninstallLinuxService() error {
	if os.Getuid() != 0 {
		return fmt.Errorf("the uninstall command must be run as root (try: sudo %s uninstall)", version.AppBinary)
	}

	cmds := []struct {
		label string
		args  []string
	}{
		{"Stopping service", []string{"systemctl", "stop", version.AppSlug}},
		{"Disabling service", []string{"systemctl", "disable", version.AppSlug}},
	}
	for _, c := range cmds {
		fmt.Printf("  %s...\n", c.label)
		out, err := exec.Command(c.args[0], c.args[1:]...).CombinedOutput() //nolint:gosec
		if err != nil {
			// Don't fail hard — service may not be running
			fmt.Printf("  warning: %s: %s\n", c.label, strings.TrimSpace(string(out)))
		}
	}

	if err := os.Remove(systemdUnitPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove unit file: %w", err)
	}
	fmt.Printf("  ✓ Removed %s\n", systemdUnitPath)

	out, err := exec.Command("systemctl", "daemon-reload").CombinedOutput()
	if err != nil {
		return fmt.Errorf("daemon-reload failed: %s — %w", string(out), err)
	}

	// Remove the binary from /usr/local/bin.
	const binPath = "/usr/local/bin/" + version.AppBinary
	if err := os.Remove(binPath); err != nil && !os.IsNotExist(err) {
		fmt.Printf("  warning: could not remove %s: %v\n", binPath, err)
	} else if err == nil {
		fmt.Printf("  ✓ Removed %s\n", binPath)
	}

	fmt.Println()
	cprintf(colorGreen, "  ✓ %s uninstalled completely.\n", version.AppName)
	fmt.Println()
	fmt.Println("  Your data is kept at ~/.trangly — remove it manually if you want a clean slate:")
	fmt.Printf("  rm -rf ~/.trangly\n")
	fmt.Println()
	return nil
}

func uninstallWindowsGuide() error {
	fmt.Println("To remove the " + version.AppName + " Windows service, run in an elevated terminal:")
	fmt.Println()
	fmt.Printf("  nssm stop %s\n", version.AppSlug)
	fmt.Printf("  nssm remove %s confirm\n", version.AppSlug)
	return nil
}

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "status",
		Short:        "Show the " + version.AppName + " service status",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			switch runtime.GOOS {
			case "linux":
				c := exec.Command("systemctl", "status", version.AppSlug)
				c.Stdout = os.Stdout
				c.Stderr = os.Stderr
				_ = c.Run() // exit code 3 = inactive, not an error
				return nil
			case "windows":
				c := exec.Command("sc", "query", version.AppSlug)
				c.Stdout = os.Stdout
				c.Stderr = os.Stderr
				return c.Run()
			default:
				return fmt.Errorf("unsupported OS %q", runtime.GOOS)
			}
		},
	}
}

func serviceLogsCmd() *cobra.Command {
	var lines int
	cmd := &cobra.Command{
		Use:          "logs",
		Short:        "Tail the " + version.AppName + " service logs",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			switch runtime.GOOS {
			case "linux":
				c := exec.Command("journalctl", "-u", version.AppSlug, "-f", "--no-pager", "-n", fmt.Sprintf("%d", lines))
				c.Stdout = os.Stdout
				c.Stderr = os.Stderr
				return c.Run()
			case "windows":
				fmt.Println("On Windows, check the Event Viewer or the NSSM-configured log file:")
				fmt.Printf("  nssm get %s AppStdout\n", version.AppSlug)
				fmt.Printf("  nssm get %s AppStderr\n", version.AppSlug)
				return nil
			default:
				return fmt.Errorf("unsupported OS %q", runtime.GOOS)
			}
		},
	}
	cmd.Flags().IntVarP(&lines, "lines", "n", 100, "number of lines to show")
	return cmd
}

// ── ANSI color helpers ─────────────────────────────────────────────────────────

const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorGray   = "\033[90m"
	colorBold   = "\033[1m"
)

// cprintf prints with ANSI color only when stdout is a real terminal.
func cprintf(color, format string, args ...any) {
	if term.IsTerminal(int(os.Stdout.Fd())) {
		fmt.Printf(color+format+colorReset, args...)
	} else {
		fmt.Printf(format, args...)
	}
}

// printBanner prints a bold section header with separator lines.
func printBanner(title string) {
	fmt.Println()
	cprintf(colorBold, "  %s\n", strings.Repeat("─", 54))
	cprintf(colorBold, "  %s  —  %s\n", version.AppName, title)
	cprintf(colorBold, "  %s\n", strings.Repeat("─", 54))
	fmt.Println()
}

// printCheckRow renders a single preflight check result line.
func printCheckRow(r system.CheckResult) {
	var icon, color string
	switch r.Status {
	case "ok":
		icon, color = "✓", colorGreen
	case "fail":
		icon, color = "✗", colorRed
	case "warn":
		icon, color = "⚠", colorYellow
	default:
		icon, color = "–", colorGray
	}
	name := fmt.Sprintf("%-26s", r.Name)
	cprintf(color, "  %s  %s", icon, name)
	fmt.Printf("%s\n", r.Detail)
}

// ── check command ──────────────────────────────────────────────────────────────

func checkCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "check",
		Aliases:      []string{"doctor", "preflight"},
		Short:        "Check system requirements before installing",
		Long:         "Run all preflight checks and show what is ready, missing, or needs attention.\nFix any failures shown, then re-run: trangly check",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCheck()
		},
	}
}

func runCheck() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	printBanner("System Requirements Check")
	fmt.Println("  Checking requirements...")
	fmt.Println()

	results := system.RunChecks(ctx)

	var failures, warnings []system.CheckResult
	for _, r := range results {
		printCheckRow(r)
		switch r.Status {
		case "fail":
			failures = append(failures, r)
		case "warn":
			warnings = append(warnings, r)
		}
	}

	fmt.Println()
	fmt.Println("  " + strings.Repeat("─", 54))
	fmt.Println()

	if len(failures) > 0 {
		cprintf(colorRed, "  ✗  %d requirement(s) not met — fix these before installing:\n\n", len(failures))
		for i, f := range failures {
			cprintf(colorRed, "  %d) %s\n", i+1, f.Name)
			if f.Fix != "" {
				for _, line := range strings.Split(f.Fix, "\n") {
					fmt.Printf("     %s\n", line)
				}
			}
			fmt.Println()
		}
		fmt.Println("  Once fixed, re-run:  trangly check")
		fmt.Println()
		return fmt.Errorf("requirements not met")
	}

	if len(warnings) > 0 {
		cprintf(colorYellow, "  ⚠  %d warning(s) — review before going to production:\n\n", len(warnings))
		for _, w := range warnings {
			cprintf(colorYellow, "  • %s\n", w.Name)
			if w.Fix != "" {
				for _, line := range strings.Split(w.Fix, "\n") {
					fmt.Printf("    %s\n", line)
				}
			}
			fmt.Println()
		}
	}

	cprintf(colorGreen, "  ✓  All requirements met!\n")
	fmt.Println()

	ip := detectPublicIP(5 * time.Second)
	urlHint := fmt.Sprintf("http://YOUR-SERVER-IP:%d", version.AppPort)
	if ip != "" {
		urlHint = fmt.Sprintf("http://%s:%d", ip, version.AppPort)
	}

	cprintf(colorBold, "  Next steps:\n")
	fmt.Println()
	cprintf(colorGreen, "  1.  sudo %s install\n", version.AppBinary)
	fmt.Println("      Register Trangly as a systemd service (starts on boot)")
	fmt.Println()
	cprintf(colorGreen, "  2.  Open %s\n", urlHint)
	fmt.Println("      Complete setup in the browser — create admin account + connect GitHub")
	fmt.Println()
	cprintf(colorBold, "  Management commands:\n")
	fmt.Println()
	fmt.Printf("  %-34s %s\n", "trangly status", "show service status")
	fmt.Printf("  %-34s %s\n", "trangly logs", "tail live service logs")
	fmt.Printf("  %-34s %s\n", "sudo trangly stop", "gracefully stop the service")
	fmt.Printf("  %-34s %s\n", "sudo trangly uninstall", "remove service (your data is kept)")
	fmt.Printf("  %-34s %s\n", "trangly reset --force", "factory reset — deletes ALL data")
	fmt.Println()
	return nil
}

// ── stop command ───────────────────────────────────────────────────────────────

func stopCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "stop",
		Short:        "Gracefully stop the " + version.AppName + " service",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if runtime.GOOS != "linux" {
				return fmt.Errorf("stop is only supported on Linux — use your OS service manager")
			}
			if os.Getuid() != 0 {
				return fmt.Errorf("stop requires root — try: sudo %s stop", version.AppBinary)
			}
			fmt.Printf("  Stopping %s...\n", version.AppName)
			out, err := exec.Command("systemctl", "stop", version.AppSlug).CombinedOutput() //nolint:gosec
			if err != nil {
				return fmt.Errorf("stop failed: %s", strings.TrimSpace(string(out)))
			}
			cprintf(colorGreen, "  ✓  %s service stopped.\n", version.AppName)
			fmt.Println()
			fmt.Printf("  Start again :  sudo systemctl start %s\n", version.AppSlug)
			fmt.Printf("  Remove      :  sudo %s uninstall\n", version.AppBinary)
			fmt.Println()
			return nil
		},
	}
}

// detectPublicIP fetches the machine's public IPv4 address from api.ipify.org.
// Returns empty string on any failure — callers must handle the empty case gracefully.
func detectPublicIP(timeout time.Duration) string {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.ipify.org", nil)
	if err != nil {
		return ""
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// saveSetupConf writes port and public URL to {dataDir}/setup.conf (0600).
// Called by `trangly setup`; read by `trangly install`.
func saveSetupConf(dataDir string, port int, baseURL string) error {
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return err
	}
	contents := fmt.Sprintf("PORT=%d\nBASE_URL=%s\n", port, baseURL)
	return os.WriteFile(filepath.Join(dataDir, "setup.conf"), []byte(contents), 0600)
}

// loadSetupConf reads port and public URL from {dataDir}/setup.conf.
// Returns zero values silently if the file doesn't exist or can't be parsed.
func loadSetupConf(dataDir string) (port int, baseURL string) {
	data, err := os.ReadFile(filepath.Join(dataDir, "setup.conf"))
	if err != nil {
		return 0, ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "=", 2)
		if len(parts) != 2 {
			continue
		}
		switch parts[0] {
		case "PORT":
			if p, err := strconv.Atoi(parts[1]); err == nil && p > 0 {
				port = p
			}
		case "BASE_URL":
			baseURL = parts[1]
		}
	}
	return
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

func prompt(r *bufio.Reader, label string) (string, error) {
	fmt.Printf("%s: ", label)
	line, err := r.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("read %s: %w", label, err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func promptPassword(label string) (string, error) {
	fmt.Printf("%s: ", label)
	// Use os.Stdin.Fd() — on Windows this is the correct console HANDLE,
	// whereas syscall.Stdin (a constant 0) is not a valid Windows HANDLE.
	raw, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println() // newline after hidden input
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	return string(raw), nil
}

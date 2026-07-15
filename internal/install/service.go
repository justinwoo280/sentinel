package install

import (
	"crypto/ecdh"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Role identifies which sentinel role a service manages.
type Role string

const (
	RoleAgent  Role = "agent"
	RoleMaster Role = "master"
)

const (
	binaryPath    = "/usr/local/bin/sentinel"
	agentConfig   = "/etc/sentinel/agent.yaml"
	masterConfig  = "/etc/sentinel/master.yaml"
	agentUnit     = "/etc/systemd/system/sentinel-agent.service"
	masterUnit    = "/etc/systemd/system/sentinel-master.service"
	agentLogFile  = "/var/log/sentinel/agent.log"
	masterLogFile = "/var/log/sentinel/master.log"
)

// serviceName returns the systemd service name for a role.
func serviceName(r Role) string {
	return "sentinel-" + string(r)
}

// unitPath returns the systemd unit path for a role.
func unitPath(r Role) string {
	if r == RoleMaster {
		return masterUnit
	}
	return agentUnit
}

// configPathFor returns the default config path for a role.
func configPathFor(r Role) string {
	if r == RoleMaster {
		return masterConfig
	}
	return agentConfig
}

// logFileFor returns the log file path for a role (cron mode).
func logFileFor(r Role) string {
	if r == RoleMaster {
		return masterLogFile
	}
	return agentLogFile
}

// SystemdAvailable reports whether systemd is the init system.
func SystemdAvailable() bool {
	_, err := os.Stat("/run/systemd/system")
	return err == nil
}

// DerivePublicKeyFromFile reads a base64 X25519 private key from path and
// returns the corresponding base64 public key. Used by the management
// panel to re-display the master public key for agents.
func DerivePublicKeyFromFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read key %s: %w", path, err)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
	if err != nil {
		return "", fmt.Errorf("decode key: %w", err)
	}
	priv, err := ecdh.X25519().NewPrivateKey(raw)
	if err != nil {
		return "", fmt.Errorf("parse key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(priv.PublicKey().Bytes()), nil
}

// unitTemplate builds a systemd unit file for the given role.
func unitTemplate(r Role) string {
	desc := "Sentinel Agent"
	exec := fmt.Sprintf("%s agent -c %s", binaryPath, agentConfig)
	if r == RoleMaster {
		desc = "Sentinel Master"
		exec = fmt.Sprintf("%s master -c %s", binaryPath, masterConfig)
	}
	return fmt.Sprintf(`[Unit]
Description=%s
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s
Restart=always
RestartSec=5
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
`, desc, exec)
}

// InstallSystemd writes the systemd unit for a role. Returns an error if
// systemd is not available so callers can fall back to cron.
func InstallSystemd(r Role) error {
	if !SystemdAvailable() {
		return fmt.Errorf("systemd not detected")
	}
	path := unitPath(r)
	if err := os.WriteFile(path, []byte(unitTemplate(r)), 0644); err != nil {
		return fmt.Errorf("install: write systemd unit: %w", err)
	}
	// Reload systemd so the new unit is visible.
	_ = exec.Command("systemctl", "daemon-reload").Run()
	fmt.Printf("systemd unit installed: %s\n", path)
	fmt.Printf("Enable and start with: systemctl enable --now %s\n", serviceName(r))
	return nil
}

// InstallCron installs an @reboot crontab entry as a fallback for
// non-systemd systems. Uses nohup to survive the cron session.
func InstallCron(r Role) error {
	cfg := configPathFor(r)
	logf := logFileFor(r)
	entry := fmt.Sprintf("@reboot nohup %s %s -c %s >> %s 2>&1 &\n",
		binaryPath, string(r), cfg, logf)

	existing := ""
	if data, err := exec.Command("crontab", "-l").Output(); err == nil {
		existing = string(data)
	}
	marker := fmt.Sprintf("%s %s", binaryPath, string(r))
	if strings.Contains(existing, marker) {
		fmt.Println("Cron entry already exists, skipping.")
		return nil
	}
	cmd := exec.Command("crontab", "-")
	cmd.Stdin = strings.NewReader(existing + entry)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("install: crontab update failed: %w", err)
	}
	fmt.Println("Cron @reboot entry installed.")
	fmt.Printf("Start now with: nohup %s %s -c %s >> %s 2>&1 &\n",
		binaryPath, string(r), cfg, logf)
	return nil
}

// InstallService installs systemd if available, otherwise cron.
func InstallService(r Role) error {
	if err := InstallSystemd(r); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: systemd install failed (%v), trying cron fallback...\n", err)
		return InstallCron(r)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Service control (used by the management panel)
// ---------------------------------------------------------------------------

// ServiceStatus is a snapshot of a role's service state.
type ServiceStatus struct {
	Role        Role
	Installed   bool // unit file (or cron entry) exists
	UsesSystemd bool
	Active      bool   // running now
	Enabled     bool   // starts on boot
	Detail      string // raw systemctl output or note
}

// Status queries the current service state for a role.
func Status(r Role) ServiceStatus {
	s := ServiceStatus{Role: r, UsesSystemd: SystemdAvailable()}
	if s.UsesSystemd {
		if _, err := os.Stat(unitPath(r)); err == nil {
			s.Installed = true
		}
		s.Active = systemctlIs(r, "is-active", "active")
		s.Enabled = systemctlIs(r, "is-enabled", "enabled")
		if out, err := exec.Command("systemctl", "is-active", serviceName(r)).Output(); err == nil {
			s.Detail = strings.TrimSpace(string(out))
		}
		return s
	}
	// Cron mode.
	if data, err := exec.Command("crontab", "-l").Output(); err == nil {
		if strings.Contains(string(data), fmt.Sprintf("%s %s", binaryPath, string(r))) {
			s.Installed = true
			s.Enabled = true // @reboot
		}
	}
	// Best-effort running check via pgrep.
	if err := exec.Command("pgrep", "-f", fmt.Sprintf("sentinel %s", string(r))).Run(); err == nil {
		s.Active = true
	}
	s.Detail = "cron/nohup mode"
	return s
}

func systemctlIs(r Role, verb, want string) bool {
	out, _ := exec.Command("systemctl", verb, serviceName(r)).Output()
	return strings.TrimSpace(string(out)) == want
}

// Start starts the service.
func Start(r Role) error { return runSystemctl(r, "start") }

// Stop stops the service.
func Stop(r Role) error { return runSystemctl(r, "stop") }

// Restart restarts the service.
func Restart(r Role) error { return runSystemctl(r, "restart") }

// Enable enables the service on boot and starts it.
func Enable(r Role) error {
	if !SystemdAvailable() {
		return fmt.Errorf("enable requires systemd (cron @reboot already persistent)")
	}
	return exec.Command("systemctl", "enable", "--now", serviceName(r)).Run()
}

func runSystemctl(r Role, verb string) error {
	if !SystemdAvailable() {
		return fmt.Errorf("%s requires systemd; in cron mode manage the process manually", verb)
	}
	out, err := exec.Command("systemctl", verb, serviceName(r)).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s %s: %v: %s", verb, serviceName(r), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Logs returns the last n lines of the service log (journalctl or file).
func Logs(r Role, n int) (string, error) {
	if SystemdAvailable() {
		out, err := exec.Command("journalctl", "-u", serviceName(r),
			"-n", fmt.Sprintf("%d", n), "--no-pager").CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("journalctl: %v", err)
		}
		return string(out), nil
	}
	// Cron mode: tail the log file.
	data, err := os.ReadFile(logFileFor(r))
	if err != nil {
		return "", fmt.Errorf("read log %s: %w", logFileFor(r), err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n"), nil
}

// ---------------------------------------------------------------------------
// Uninstall
// ---------------------------------------------------------------------------

// UninstallOptions controls what uninstall removes.
type UninstallOptions struct {
	RemoveConfig bool // delete /etc/sentinel/<role>.yaml
	RemoveData   bool // delete /var/lib/sentinel (DB, keys, cookies, mmdb)
	RemoveBinary bool // delete /usr/local/bin/sentinel
}

// Uninstall stops the service, removes the unit/cron entry, and optionally
// removes config, data, and the binary. Returns a summary of actions.
func Uninstall(r Role, opts UninstallOptions) []string {
	var actions []string

	if SystemdAvailable() {
		_ = exec.Command("systemctl", "stop", serviceName(r)).Run()
		_ = exec.Command("systemctl", "disable", serviceName(r)).Run()
		if err := os.Remove(unitPath(r)); err == nil {
			actions = append(actions, "removed "+unitPath(r))
		}
		_ = exec.Command("systemctl", "daemon-reload").Run()
		actions = append(actions, "stopped and disabled "+serviceName(r))
	} else {
		// Remove cron entry.
		if data, err := exec.Command("crontab", "-l").Output(); err == nil {
			marker := fmt.Sprintf("%s %s", binaryPath, string(r))
			var kept []string
			for _, line := range strings.Split(string(data), "\n") {
				if !strings.Contains(line, marker) {
					kept = append(kept, line)
				}
			}
			cmd := exec.Command("crontab", "-")
			cmd.Stdin = strings.NewReader(strings.Join(kept, "\n"))
			_ = cmd.Run()
			actions = append(actions, "removed cron @reboot entry")
		}
	}

	if opts.RemoveConfig {
		cfg := configPathFor(r)
		if err := os.Remove(cfg); err == nil {
			actions = append(actions, "removed "+cfg)
		}
	}
	if opts.RemoveData {
		if err := os.RemoveAll("/var/lib/sentinel"); err == nil {
			actions = append(actions, "removed /var/lib/sentinel")
		}
	}
	if opts.RemoveBinary {
		if err := os.Remove(binaryPath); err == nil {
			actions = append(actions, "removed "+binaryPath)
		}
	}
	return actions
}

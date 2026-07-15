// Package manage implements the interactive management panel for the
// sentinel binary: view status, control the service, edit common config
// fields, regenerate the registration blob, view logs, and uninstall.
package manage

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/justinwoo280/sentinel/internal/config"
	"github.com/justinwoo280/sentinel/internal/install"
)

// Panel drives the interactive menu.
type Panel struct {
	in   *bufio.Scanner
	role install.Role
}

// Run launches the management panel. If role is empty it is auto-detected
// (or the user is prompted when both roles are installed).
func Run(roleHint string) error {
	p := &Panel{in: bufio.NewScanner(os.Stdin)}
	// Larger buffer for pasted values.
	p.in.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	role, err := resolveRole(roleHint)
	if err != nil {
		return err
	}
	p.role = role

	for {
		p.showHeader()
		p.showMenu()
		choice := p.prompt("Enter choice: ")
		cont := p.dispatch(choice)
		if !cont {
			return nil
		}
		fmt.Println()
	}
}

// resolveRole determines which role's config/service to manage.
func resolveRole(hint string) (install.Role, error) {
	h := strings.ToLower(strings.TrimSpace(hint))
	if h == "agent" {
		return install.RoleAgent, nil
	}
	if h == "master" {
		return install.RoleMaster, nil
	}

	agentInstalled := fileExists("/etc/sentinel/agent.yaml")
	masterInstalled := fileExists("/etc/sentinel/master.yaml")

	switch {
	case agentInstalled && !masterInstalled:
		return install.RoleAgent, nil
	case masterInstalled && !agentInstalled:
		return install.RoleMaster, nil
	case agentInstalled && masterInstalled:
		fmt.Println("Both agent and master are installed on this host.")
		fmt.Println("  1. Manage Agent")
		fmt.Println("  2. Manage Master")
		sc := bufio.NewScanner(os.Stdin)
		fmt.Print("Enter choice [1-2]: ")
		if sc.Scan() {
			if strings.TrimSpace(sc.Text()) == "2" {
				return install.RoleMaster, nil
			}
		}
		return install.RoleAgent, nil
	default:
		return "", fmt.Errorf("no sentinel config found (looked for /etc/sentinel/{agent,master}.yaml); run 'sentinel install' first")
	}
}

func (p *Panel) showHeader() {
	st := install.Status(p.role)
	active := "stopped"
	if st.Active {
		active = "running"
	}
	enabled := "disabled"
	if st.Enabled {
		enabled = "enabled"
	}
	svcMode := "cron/nohup"
	if st.UsesSystemd {
		svcMode = "systemd"
	}
	fmt.Println("========================================")
	fmt.Printf(" Sentinel Management — role: %s\n", p.role)
	fmt.Printf(" service: %s | %s | boot=%s\n", svcMode, active, enabled)
	fmt.Println("========================================")
}

func (p *Panel) showMenu() {
	fmt.Println(" 1. Show status & config summary")
	fmt.Println(" 2. Start service")
	fmt.Println(" 3. Stop service")
	fmt.Println(" 4. Restart service")
	fmt.Println(" 5. Enable on boot")
	fmt.Println(" 6. Edit configuration")
	if p.role == install.RoleAgent {
		fmt.Println(" 7. Regenerate registration blob")
	} else {
		fmt.Println(" 7. Show master public key")
	}
	fmt.Println(" 8. View recent logs")
	fmt.Println(" 9. Uninstall")
	fmt.Println(" 0. Exit")
}

// dispatch runs the chosen action; returns false to exit the panel.
func (p *Panel) dispatch(choice string) bool {
	switch strings.TrimSpace(choice) {
	case "1":
		p.showStatus()
	case "2":
		p.control(install.Start, "start")
	case "3":
		p.control(install.Stop, "stop")
	case "4":
		p.control(install.Restart, "restart")
	case "5":
		p.control(install.Enable, "enable")
	case "6":
		p.editConfig()
	case "7":
		if p.role == install.RoleAgent {
			p.regenRegistration()
		} else {
			p.showMasterPubKey()
		}
	case "8":
		p.viewLogs()
	case "9":
		return p.uninstall()
	case "0", "q", "exit", "quit":
		return false
	default:
		fmt.Println("Invalid choice.")
	}
	return true
}

func (p *Panel) control(fn func(install.Role) error, verb string) {
	if err := fn(p.role); err != nil {
		fmt.Printf("Failed to %s: %v\n", verb, err)
		return
	}
	fmt.Printf("Service %s: OK\n", verb)
}

func (p *Panel) showStatus() {
	st := install.Status(p.role)
	fmt.Printf("Role:       %s\n", p.role)
	fmt.Printf("Service:    %s\n", ternary(st.UsesSystemd, "systemd", "cron/nohup"))
	fmt.Printf("Installed:  %v\n", st.Installed)
	fmt.Printf("Active:     %v (%s)\n", st.Active, st.Detail)
	fmt.Printf("On boot:    %v\n", st.Enabled)
	fmt.Println()

	if p.role == install.RoleAgent {
		cfg, err := config.LoadAgent(agentCfgPath)
		if err != nil {
			fmt.Printf("(config not loadable: %v)\n", err)
			return
		}
		fmt.Println("--- Agent config summary ---")
		fmt.Printf("Node:     %s (%s)\n", cfg.Node.Name, cfg.Node.Alias)
		fmt.Printf("Region:   %s / %s / %s (%s)\n",
			cfg.Region.Code, cfg.Region.State, cfg.Region.City, cfg.Region.Name)
		fmt.Printf("Modules:  google=%v trust=%v\n", cfg.Modules.Google, cfg.Modules.Trust)
		fmt.Printf("Master:   %s\n", cfg.Master.Addr)
		fmt.Printf("Schedule: interval=%s jitter=%s\n",
			time.Duration(cfg.Schedule.Interval), time.Duration(cfg.Schedule.Jitter))
		fmt.Printf("OTA:      %v\n", cfg.Master.OTA)
		fmt.Printf("GeoIP:    %s\n", geoIPSummary(cfg.GeoIP))
	} else {
		cfg, err := config.LoadMaster(masterCfgPath)
		if err != nil {
			fmt.Printf("(config not loadable: %v)\n", err)
			return
		}
		fmt.Println("--- Master config summary ---")
		fmt.Printf("Listen:      %s\n", cfg.Control.Listen)
		fmt.Printf("Store:       %s\n", cfg.Store.Path)
		fmt.Printf("Telegram:    %s\n", tokenSummary(cfg.Telegram.Token))
		fmt.Printf("Admins:      %s\n", adminIDsSummary(cfg.Telegram.AdminIDs))
		fmt.Printf("OTA enabled: %v\n", cfg.Telegram.EnableOTA)
	}
}

func (p *Panel) viewLogs() {
	out, err := install.Logs(p.role, 40)
	if err != nil {
		fmt.Printf("Could not read logs: %v\n", err)
		return
	}
	fmt.Println("--- last 40 log lines ---")
	fmt.Println(out)
}

func (p *Panel) regenRegistration() {
	cfg, err := config.LoadAgent(agentCfgPath)
	if err != nil {
		fmt.Printf("Could not load agent config: %v\n", err)
		return
	}
	blob, err := install.RegistrationBlobFromConfig(cfg)
	if err != nil {
		fmt.Printf("Could not build blob: %v\n", err)
		return
	}
	install.PrintRegistrationBlob(blob)
}

func (p *Panel) showMasterPubKey() {
	cfg, err := config.LoadMaster(masterCfgPath)
	if err != nil {
		fmt.Printf("Could not load master config: %v\n", err)
		return
	}
	pub, err := install.DerivePublicKeyFromFile(cfg.Control.StaticKeyPath)
	if err != nil {
		fmt.Printf("Could not read/derive public key: %v\n", err)
		return
	}
	fmt.Println("Static public key (share with agents):")
	fmt.Println(pub)
}

func (p *Panel) uninstall() bool {
	fmt.Println("\n*** Uninstall ***")
	if !p.confirm("Stop and remove the service?") {
		fmt.Println("Cancelled.")
		return true
	}
	rmCfg := p.confirm("Also remove config file?")
	rmData := false
	rmBin := false
	if p.role == install.RoleMaster {
		fmt.Println("WARNING: removing data deletes the master DB and static key —")
		fmt.Println("all registered agents would need to be re-registered with a new key.")
	}
	rmData = p.confirm("Also remove /var/lib/sentinel (DB, keys, cookies, mmdb)?")
	rmBin = p.confirm("Also remove the /usr/local/bin/sentinel binary?")

	actions := install.Uninstall(p.role, install.UninstallOptions{
		RemoveConfig: rmCfg,
		RemoveData:   rmData,
		RemoveBinary: rmBin,
	})
	fmt.Println("\nUninstall complete:")
	for _, a := range actions {
		fmt.Printf("  - %s\n", a)
	}
	return false // exit after uninstall
}

// ---------------------------------------------------------------------------
// config editing
// ---------------------------------------------------------------------------

func (p *Panel) editConfig() {
	if p.role == install.RoleAgent {
		p.editAgentConfig()
	} else {
		p.editMasterConfig()
	}
}

func (p *Panel) editAgentConfig() {
	cfg, err := config.LoadAgent(agentCfgPath)
	if err != nil {
		fmt.Printf("Could not load config: %v\n", err)
		return
	}
	for {
		fmt.Println("\n--- Edit Agent config ---")
		fmt.Printf(" 1. Alias           [%s]\n", cfg.Node.Alias)
		fmt.Printf(" 2. Google module   [%v]\n", cfg.Modules.Google)
		fmt.Printf(" 3. Trust module    [%v]\n", cfg.Modules.Trust)
		fmt.Printf(" 4. Master address  [%s]\n", cfg.Master.Addr)
		fmt.Printf(" 5. Master pub key  [%s]\n", truncate(cfg.Master.StaticPub, 16))
		fmt.Printf(" 6. OTA enabled     [%v]\n", cfg.Master.OTA)
		fmt.Printf(" 7. Schedule interval [%s]\n", time.Duration(cfg.Schedule.Interval))
		fmt.Printf(" 8. Bind IP         [%s]\n", cfg.Network.BindIP)
		fmt.Printf(" 9. Quality API keys [%d set]\n", countSetKeys(cfg.Quality.APIKeys))
		fmt.Printf("10. GeoIP (MaxMind) [%s]\n", geoIPSummary(cfg.GeoIP))
		fmt.Println(" 0. Save and return")
		switch p.prompt("Field: ") {
		case "1":
			cfg.Node.Alias = p.prompt("New alias: ")
		case "2":
			cfg.Modules.Google = p.promptBool("Enable Google?", cfg.Modules.Google)
		case "3":
			cfg.Modules.Trust = p.promptBool("Enable Trust?", cfg.Modules.Trust)
		case "4":
			cfg.Master.Addr = p.prompt("Master address (host:port): ")
		case "5":
			cfg.Master.StaticPub = p.prompt("Master public key (base64): ")
		case "6":
			cfg.Master.OTA = p.promptBool("Enable OTA?", cfg.Master.OTA)
		case "7":
			if d := p.promptDuration("Interval (e.g. 20m)"); d > 0 {
				cfg.Schedule.Interval = config.Duration(d)
			}
		case "8":
			cfg.Network.BindIP = p.prompt("Bind IP (empty for default route): ")
		case "9":
			p.editQualityKeys(&cfg.Quality.APIKeys)
		case "10":
			p.editGeoIP(&cfg.GeoIP)
		case "0", "":
			if err := config.SaveAgent(agentCfgPath, cfg); err != nil {
				fmt.Printf("Save failed: %v\n", err)
				return
			}
			fmt.Println("Config saved. Restart the service to apply.")
			return
		default:
			fmt.Println("Invalid field.")
		}
	}
}

func (p *Panel) editMasterConfig() {
	cfg, err := config.LoadMaster(masterCfgPath)
	if err != nil {
		fmt.Printf("Could not load config: %v\n", err)
		return
	}
	for {
		fmt.Println("\n--- Edit Master config ---")
		fmt.Printf(" 1. Telegram token  [%s]\n", tokenSummary(cfg.Telegram.Token))
		fmt.Printf(" 2. OTA enabled     [%v]\n", cfg.Telegram.EnableOTA)
		fmt.Printf(" 3. Listen address  [%s]\n", cfg.Control.Listen)
		fmt.Printf(" 4. Admin IDs       [%s]\n", adminIDsSummary(cfg.Telegram.AdminIDs))
		fmt.Println(" 0. Save and return")
		switch p.prompt("Field: ") {
		case "1":
			cfg.Telegram.Token = p.prompt("Telegram bot token: ")
		case "2":
			cfg.Telegram.EnableOTA = p.promptBool("Enable OTA?", cfg.Telegram.EnableOTA)
		case "3":
			cfg.Control.Listen = p.prompt("Listen address (e.g. :8443): ")
		case "4":
			cfg.Telegram.AdminIDs = p.editAdminIDs(cfg.Telegram.AdminIDs)
		case "0", "":
			if err := config.SaveMaster(masterCfgPath, cfg); err != nil {
				fmt.Printf("Save failed: %v\n", err)
				return
			}
			fmt.Println("Config saved. Restart the service to apply.")
			return
		default:
			fmt.Println("Invalid field.")
		}
	}
}

// editAdminIDs manages the Telegram admin allowlist. Enter an ID to add
// it, "-<id>" to remove, or blank to finish.
func (p *Panel) editAdminIDs(ids []int64) []int64 {
	for {
		fmt.Println("\n--- Admin Telegram user IDs (allowlist) ---")
		if len(ids) == 0 {
			fmt.Println("  (none — the bot is FAIL-CLOSED and will deny everyone)")
		}
		for _, id := range ids {
			fmt.Printf("  %d\n", id)
		}
		fmt.Println("Commands:  <id> = add   -<id> = remove   [Enter] = done")
		fmt.Println("Tip: an unauthorized user messaging the bot is told their own ID.")
		v := p.prompt("ID (or Enter to finish): ")
		if v == "" {
			return ids
		}
		if strings.HasPrefix(v, "-") {
			rm, err := strconv.ParseInt(strings.TrimPrefix(v, "-"), 10, 64)
			if err != nil {
				fmt.Println("Invalid ID.")
				continue
			}
			if containsID(ids, rm) {
				ids = removeID(ids, rm)
				fmt.Printf("Removed %d.\n", rm)
			} else {
				fmt.Printf("%d is not in the list.\n", rm)
			}
			continue
		}
		add, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			fmt.Println("Invalid ID.")
			continue
		}
		if containsID(ids, add) {
			fmt.Printf("%d is already an admin.\n", add)
		} else {
			ids = append(ids, add)
			fmt.Printf("Added %d. Press Enter when done.\n", add)
		}
	}
}

func containsID(ids []int64, id int64) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

func removeID(ids []int64, id int64) []int64 {
	out := ids[:0]
	for _, x := range ids {
		if x != id {
			out = append(out, x)
		}
	}
	return out
}

func adminIDsSummary(ids []int64) string {
	if len(ids) == 0 {
		return "(none — fail-closed)"
	}
	return fmt.Sprintf("%d configured", len(ids))
}

// editQualityKeys edits the optional commercial-API keys for the quality
// module. All keys are optional; blank ones are skipped (that source is
// simply omitted from the report). Free sources always run.
func (p *Panel) editQualityKeys(k *config.QualityAPIKeys) {
	for {
		fmt.Println("\n--- Quality API keys (all optional) ---")
		fmt.Printf(" 1. Scamalytics  [%s]\n", keySummary(k.Scamalytics))
		fmt.Printf(" 2. AbuseIPDB    [%s]\n", keySummary(k.AbuseIPDB))
		fmt.Printf(" 3. IP2Location  [%s]\n", keySummary(k.IP2Location))
		fmt.Printf(" 4. IPQS         [%s]\n", keySummary(k.IPQS))
		fmt.Printf(" 5. ipdata       [%s]\n", keySummary(k.IPData))
		fmt.Printf(" 6. IPinfo       [%s]\n", keySummary(k.IPInfo))
		fmt.Println(" 0. Back (blank a key by entering '-')")
		switch p.prompt("Key: ") {
		case "1":
			k.Scamalytics = readKey(p, k.Scamalytics)
		case "2":
			k.AbuseIPDB = readKey(p, k.AbuseIPDB)
		case "3":
			k.IP2Location = readKey(p, k.IP2Location)
		case "4":
			k.IPQS = readKey(p, k.IPQS)
		case "5":
			k.IPData = readKey(p, k.IPData)
		case "6":
			k.IPInfo = readKey(p, k.IPInfo)
		case "0", "":
			return
		default:
			fmt.Println("Invalid key.")
		}
	}
}

// readKey prompts for a new key value. Empty input keeps the current
// value; "-" clears it.
func readKey(p *Panel, current string) string {
	v := p.prompt("New value (blank=keep, '-'=clear): ")
	switch v {
	case "":
		return current
	case "-":
		return ""
	default:
		return v
	}
}

// editGeoIP manages the MaxMind GeoLite2 license key and related
// settings. Without a key, the quality module's Info section (ASN,
// organization, coordinates, etc.) falls back to the free IPinfo/ipapi
// sources instead of the local mmdb database.
func (p *Panel) editGeoIP(g *config.GeoIPConfig) {
	for {
		fmt.Println("\n--- GeoIP (MaxMind GeoLite2) ---")
		fmt.Printf(" 1. Enabled       [%v]\n", g.Enabled)
		fmt.Printf(" 2. License key   [%s]\n", keySummary(g.LicenseKey))
		fmt.Printf(" 3. DB path       [%s]\n", g.DBPath)
		fmt.Printf(" 4. Update interval [%s]\n", time.Duration(g.UpdateInterval))
		fmt.Println(" 0. Back")
		fmt.Println("Get a free key at: https://www.maxmind.com/en/geolite2/signup")
		switch p.prompt("Field: ") {
		case "1":
			g.Enabled = p.promptBool("Enable GeoIP?", g.Enabled)
		case "2":
			g.LicenseKey = readKey(p, g.LicenseKey)
		case "3":
			if v := p.prompt("DB path (blank=keep): "); v != "" {
				g.DBPath = v
			}
		case "4":
			if d := p.promptDuration("Update interval (e.g. 24h)"); d > 0 {
				g.UpdateInterval = config.Duration(d)
			}
		case "0", "":
			return
		default:
			fmt.Println("Invalid field.")
		}
	}
}

// geoIPSummary renders a one-line GeoIP status for the config menu.
func geoIPSummary(g config.GeoIPConfig) string {
	if !g.Enabled {
		return "disabled"
	}
	if g.LicenseKey == "" {
		return "enabled, no license key"
	}
	return "enabled, key set"
}

// ---------------------------------------------------------------------------
// input helpers
// ---------------------------------------------------------------------------

const (
	agentCfgPath  = "/etc/sentinel/agent.yaml"
	masterCfgPath = "/etc/sentinel/master.yaml"
)

func (p *Panel) prompt(label string) string {
	fmt.Print(label)
	if p.in.Scan() {
		return strings.TrimSpace(p.in.Text())
	}
	return ""
}

func (p *Panel) promptBool(label string, current bool) bool {
	def := "y/N"
	if current {
		def = "Y/n"
	}
	ans := strings.ToLower(p.prompt(fmt.Sprintf("%s [%s]: ", label, def)))
	switch ans {
	case "y", "yes", "true", "1":
		return true
	case "n", "no", "false", "0":
		return false
	default:
		return current // keep current on empty/invalid
	}
}

func (p *Panel) promptDuration(label string) time.Duration {
	s := p.prompt(label + ": ")
	if s == "" {
		return 0
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		fmt.Printf("Invalid duration: %v (unchanged)\n", err)
		return 0
	}
	return d
}

func (p *Panel) confirm(label string) bool {
	return p.promptBool(label, false)
}

// ---------------------------------------------------------------------------
// misc helpers
// ---------------------------------------------------------------------------

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func tokenSummary(tok string) string {
	if tok == "" {
		return "(not set)"
	}
	if len(tok) <= 8 {
		return "***"
	}
	return tok[:4] + "..." + tok[len(tok)-4:]
}

// keySummary masks an API key for display (set/not set).
func keySummary(k string) string {
	if k == "" {
		return "(not set)"
	}
	return "set ***"
}

// countSetKeys returns how many of the quality API keys are populated.
func countSetKeys(k config.QualityAPIKeys) int {
	n := 0
	for _, v := range []string{k.Scamalytics, k.AbuseIPDB, k.IP2Location, k.IPQS, k.IPData, k.IPInfo} {
		if v != "" {
			n++
		}
	}
	return n
}

func ternary(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}

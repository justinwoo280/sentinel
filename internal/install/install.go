// Package install implements the interactive Agent installer: region
// selection, config generation, UUID creation, and systemd unit writing.
package install

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/justinwoo280/sentinel/internal/config"
	"github.com/justinwoo280/sentinel/internal/geo"
	"github.com/justinwoo280/sentinel/internal/protocol"
	"golang.org/x/term"
)

// Installer collects user input and generates agent files.
type Installer struct {
	region      string // country ID, e.g. "DE"
	state       string // state ID, e.g. "NW" (or "Default")
	city        string // city ID, e.g. "Aachen"
	regionName  string // display name, e.g. "Germany (德国) — Aachen (亚琛)"
	keywordFile string
	masterAddr  string
	masterPub   string
	uuid        string
	nodeName    string
	alias       string
	google      bool
	trust       bool
	ota         bool
}

// Run executes the interactive installation. The reader/writer are
// injectable for testing.
func Run(cfgPath string) error {
	// Guard: this command reads interactively from stdin (region, master
	// address, etc.). If stdin is not a terminal — e.g. this was invoked
	// via "curl ... | sh" where the pipe's stdin is already exhausted by
	// the time the shell exec's this binary — every prompt would silently
	// read EOF and fall through to its default, silently installing with
	// the WRONG region/settings instead of asking the user anything.
	// Failing loudly here is much safer than that. scripts/install.sh
	// works around this for the common curl-pipe case by reopening
	// stdin from /dev/tty before exec'ing this command; this check is
	// the defense-in-depth backstop for any other caller.
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return fmt.Errorf(
			"install: stdin is not a terminal — this command requires an interactive session.\n" +
				"If you ran this via 'curl ... | sh', that pipe consumes stdin before this\n" +
				"program starts, so prompts can't be answered. Re-run either as:\n" +
				"  sh -c \"$(curl -fsSL <install.sh-url>)\"\n" +
				"or attach an interactive terminal (e.g. 'docker run -it ...')")
	}

	i := &Installer{}

	// Guard: if an agent config already exists, warn before overwriting.
	// A fresh install generates a NEW UUID, which orphans the existing
	// registration (the node would need to be re-registered on the Master).
	if _, err := os.Stat(cfgPath); err == nil {
		fmt.Printf("\nWARNING: an agent config already exists at %s.\n", cfgPath)
		fmt.Println("Continuing will generate a NEW identity (UUID) and overwrite it,")
		fmt.Println("which orphans the current registration on the Master.")
		fmt.Println("To only change settings, use 'sentinel manage' instead.")
		fmt.Print("Type 'yes' to overwrite and re-install: ")
		var confirm string
		fmt.Scanln(&confirm)
		if strings.TrimSpace(strings.ToLower(confirm)) != "yes" {
			fmt.Println("Aborted; existing config left untouched.")
			return nil
		}
	}

	// Step 1: Region selection.
	if err := i.selectRegion(); err != nil {
		return err
	}

	// Step 2: Module selection.
	i.google = true
	i.trust = true

	// Step 3: Master connection.
	if err := i.inputMaster(); err != nil {
		return err
	}

	// Step 4: Generate identity.
	i.uuid = uuid.New().String()
	i.nodeName = generateNodeName()

	// Step 5: Write config.
	cfg := i.buildConfig()
	if err := i.writeConfig(cfgPath, cfg); err != nil {
		return err
	}

	// Step 6: Print registration blob.
	i.printRegistration()

	// Step 7: Install service (systemd or @reboot cron fallback).
	if err := InstallService(RoleAgent); err != nil {
		return fmt.Errorf("install: could not install service: %w", err)
	}

	return nil
}

// selectRegion walks the full 4-level region hierarchy (continent ->
// country -> state -> city), matching the original project's install
// flow exactly. A level is skipped automatically when it has exactly one
// option (e.g. most countries only have a "Default" state); "0" goes
// back to the previous level. The resolved base coordinates, lang_params,
// and trust whitelist are NOT stored here — they are re-resolved at agent
// startup from the embedded city JSON (single source of truth).
func (i *Installer) selectRegion() error {
	m, err := geo.LoadMap()
	if err != nil {
		return fmt.Errorf("install: load region data: %w", err)
	}
	if len(m.Continents) == 0 {
		return fmt.Errorf("install: region data has no continents")
	}

	var cont geo.Continent
	var country geo.Country
	var state geo.State
	var city geo.City

	const (
		stepContinent = 0
		stepCountry   = 1
		stepState     = 2
		stepCity      = 3
		stepDone      = 4
	)
	step := stepContinent

	for step != stepDone {
		switch step {
		case stepContinent:
			fmt.Println("\n[1/4] Select target continent:")
			for idx, c := range m.Continents {
				fmt.Printf("  %d. %s\n", idx+1, c.Name)
			}
			choice, err := promptChoice("Enter choice", len(m.Continents), false)
			if err != nil {
				return err
			}
			cont = m.Continents[choice-1]
			step = stepCountry

		case stepCountry:
			if len(cont.Countries) == 0 {
				return fmt.Errorf("install: continent %s has no countries", cont.ID)
			}
			fmt.Printf("\n[2/4] Select country in %s:\n", cont.Name)
			fmt.Println("  0. Back")
			for idx, c := range cont.Countries {
				fmt.Printf("  %d. %s (%s)\n", idx+1, c.Name, c.ID)
			}
			choice, err := promptChoice("Enter choice", len(cont.Countries), true)
			if err != nil {
				return err
			}
			if choice == 0 {
				step = stepContinent
				continue
			}
			country = cont.Countries[choice-1]
			step = stepState

		case stepState:
			if len(country.States) == 0 {
				return fmt.Errorf("install: country %s has no states defined", country.ID)
			}
			if len(country.States) == 1 {
				state = country.States[0]
				fmt.Printf("\n[3/4] %s has a single region (%s) — auto-selected.\n",
					country.Name, state.Name)
				step = stepCity
				continue
			}
			fmt.Printf("\n[3/4] Select state/province in %s:\n", country.Name)
			fmt.Println("  0. Back")
			for idx, s := range country.States {
				fmt.Printf("  %d. %s\n", idx+1, s.Name)
			}
			choice, err := promptChoice("Enter choice", len(country.States), true)
			if err != nil {
				return err
			}
			if choice == 0 {
				step = stepCountry
				continue
			}
			state = country.States[choice-1]
			step = stepCity

		case stepCity:
			if len(state.Cities) == 0 {
				return fmt.Errorf("install: state %s/%s has no cities defined", country.ID, state.ID)
			}
			if len(state.Cities) == 1 {
				city = state.Cities[0]
				fmt.Printf("\n[4/4] %s has a single city (%s) — auto-selected.\n",
					state.Name, city.Name)
				step = stepDone
				continue
			}
			fmt.Printf("\n[4/4] Select city:\n")
			fmt.Println("  0. Back")
			for idx, c := range state.Cities {
				fmt.Printf("  %d. %s\n", idx+1, c.Name)
			}
			choice, err := promptChoice("Enter choice", len(state.Cities), true)
			if err != nil {
				return err
			}
			if choice == 0 {
				if len(country.States) == 1 {
					step = stepCountry
				} else {
					step = stepState
				}
				continue
			}
			city = state.Cities[choice-1]
			step = stepDone
		}
	}

	i.region = country.ID
	i.state = state.ID
	i.city = city.ID
	i.regionName = fmt.Sprintf("%s — %s", country.Name, city.Name)
	i.keywordFile = country.KeywordFile
	if i.keywordFile == "" {
		i.keywordFile = "kw_" + country.ID + ".txt"
	}

	// Fail fast if the selected city has no region JSON — better to catch
	// a data/map.json inconsistency now than at agent runtime.
	if _, err := geo.LoadCityRegion(i.region, i.state, i.city); err != nil {
		return fmt.Errorf("install: selected region has no data (%s/%s/%s): %w",
			i.region, i.state, i.city, err)
	}

	fmt.Printf("\nRegion locked: %s\n", i.regionName)
	return nil
}

// promptChoice prints a prompt, reads a line from stdin, and validates it
// against [1,max] (or 0 if allowBack). Empty input defaults to 1
// (matching the original installer's convenience default). Re-prompts on
// invalid input.
func promptChoice(label string, max int, allowBack bool) (int, error) {
	lo := 1
	if allowBack {
		lo = 0
	}
	for {
		fmt.Printf("%s [%d-%d] (default 1): ", label, lo, max)
		var line string
		fmt.Scanln(&line)
		n, err := parseChoice(line, max, allowBack)
		if err != nil {
			fmt.Printf("Invalid input: %v. Try again.\n", err)
			continue
		}
		return n, nil
	}
}

// parseChoice is the pure, testable validation core of promptChoice: it
// takes the raw input string and returns the validated choice, or an
// error. Empty input defaults to "1". If allowBack, "0" is accepted and
// returned as 0 (meaning "go back").
func parseChoice(raw string, max int, allowBack bool) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = "1"
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%q is not a number", raw)
	}
	if allowBack && n == 0 {
		return 0, nil
	}
	if n < 1 || n > max {
		return 0, fmt.Errorf("%d is out of range [1-%d]", n, max)
	}
	return n, nil
}

func (i *Installer) inputMaster() error {
	fmt.Print("\nMaster EWP address (host:port): ")
	fmt.Scanln(&i.masterAddr)
	if i.masterAddr == "" {
		return fmt.Errorf("master address is required")
	}

	fmt.Print("Master static public key (base64): ")
	fmt.Scanln(&i.masterPub)
	if i.masterPub == "" {
		return fmt.Errorf("master public key is required")
	}

	fmt.Print("Agent alias (display name, optional): ")
	// Use Scanln with a buffer for names with spaces.
	var alias string
	fmt.Scanln(&alias)
	i.alias = alias

	i.ota = true
	return nil
}

func (i *Installer) buildConfig() config.AgentConfig {
	cfg := config.DefaultAgent()
	cfg.Node.Name = i.nodeName
	cfg.Node.Alias = i.alias
	cfg.Region.Code = i.region
	cfg.Region.Name = i.regionName
	cfg.Region.State = i.state
	cfg.Region.City = i.city
	cfg.Region.KeywordFile = i.keywordFile
	cfg.Modules = config.ModulesConfig{Google: i.google, Trust: i.trust}
	cfg.Master = config.MasterConn{
		Enabled:   true,
		Addr:      i.masterAddr,
		StaticPub: i.masterPub,
		UUID:      i.uuid,
		OTA:       i.ota,
	}
	return cfg
}

func (i *Installer) writeConfig(path string, cfg config.AgentConfig) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("install: create config dir: %w", err)
	}
	if err := config.SaveAgent(path, cfg); err != nil {
		return fmt.Errorf("install: write config: %w", err)
	}
	fmt.Printf("\nConfig written to %s\n", path)
	return nil
}

func (i *Installer) printRegistration() {
	blob, err := RegistrationBlobFromParts(i.region, i.nodeName, i.alias, i.uuid, i.ota)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not build registration blob: %v\n", err)
		return
	}
	PrintRegistrationBlob(blob)
}

// RegistrationBlobFromParts builds a SENTINEL-REG: blob from raw fields.
func RegistrationBlobFromParts(region, node, alias, uuidStr string, ota bool) (string, error) {
	reg := &protocol.Registration{
		Version: protocol.RegVersion,
		Region:  region,
		Node:    node,
		Alias:   alias,
		UUID:    uuidStr,
		OTA:     ota,
	}
	return reg.Encode()
}

// RegistrationBlobFromConfig rebuilds the registration blob from a loaded
// agent config (used by the management panel to re-print it).
func RegistrationBlobFromConfig(cfg config.AgentConfig) (string, error) {
	return RegistrationBlobFromParts(
		cfg.Region.Code, cfg.Node.Name, cfg.Node.Alias, cfg.Master.UUID, cfg.Master.OTA)
}

// PrintRegistrationBlob prints a registration blob with framing.
func PrintRegistrationBlob(blob string) {
	fmt.Println("\n=== Registration Blob ===")
	fmt.Println(blob)
	fmt.Println("=========================")
	fmt.Println("\nSend this to the Master bot to register this agent.")
}

func generateNodeName() string {
	host, err := os.Hostname()
	if err != nil {
		host = "node"
	}
	// Shorten hostname and add timestamp suffix.
	if len(host) > 12 {
		host = host[:12]
	}
	host = strings.ToLower(strings.ReplaceAll(host, "-", ""))
	ts := time.Now().Format("0102")
	return fmt.Sprintf("%s-%s", host, ts)
}

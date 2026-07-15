// Package install implements the interactive Agent installer: region
// selection, config generation, UUID creation, and systemd unit writing.
package install

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/justinwoo280/sentinel/internal/config"
	"github.com/justinwoo280/sentinel/internal/geo"
	"github.com/justinwoo280/sentinel/internal/protocol"
)

// Installer collects user input and generates agent files.
type Installer struct {
	region      string
	regionName  string
	lat         float64
	lon         float64
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
	i := &Installer{}

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

func (i *Installer) selectRegion() error {
	fmt.Println("\nSelect region:")
	regions := []struct {
		code string
		name string
	}{
		{"JP", "Japan (日本)"},
		{"US", "United States"},
		{"HK", "Hong Kong"},
		{"KR", "Korea"},
		{"SG", "Singapore"},
		{"DE", "Germany"},
		{"UK", "United Kingdom"},
		{"CA", "Canada"},
		{"AU", "Australia"},
	}
	for idx, r := range regions {
		fmt.Printf("  %d. %s (%s)\n", idx+1, r.name, r.code)
	}
	fmt.Print("\nEnter region number: ")

	var choice int
	fmt.Scanln(&choice)
	if choice < 1 || choice > len(regions) {
		return fmt.Errorf("invalid region selection: %d", choice)
	}

	r := regions[choice-1]
	i.region = r.code
	i.regionName = r.name
	i.keywordFile = "kw_" + r.code + ".txt"

	// Look up region from embedded map data.
	if m, err := geo.LoadMap(); err == nil {
		for _, cont := range m.Continents {
			for _, country := range cont.Countries {
				if country.ID == r.code {
					i.regionName = country.Name
					if country.KeywordFile != "" {
						i.keywordFile = country.KeywordFile
					}
					break
				}
			}
		}
	}

	// Default coordinates.
	coords := map[string][2]float64{
		"JP": {35.6762, 139.6503},
		"US": {40.7128, -74.0060},
		"HK": {22.3193, 114.1694},
		"KR": {37.5665, 126.9780},
		"SG": {1.3521, 103.8198},
		"DE": {52.5200, 13.4050},
		"UK": {51.5074, -0.1278},
		"CA": {45.4215, -75.6972},
		"AU": {-33.8688, 151.2093},
	}
	if c, ok := coords[r.code]; ok {
		i.lat = c[0]
		i.lon = c[1]
	}

	return nil
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
	cfg.Region.Lat = i.lat
	cfg.Region.Lon = i.lon
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

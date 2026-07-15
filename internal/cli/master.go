package cli

import (
	"context"
	"crypto/ecdh"
	"encoding/base64"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/justinwoo280/sentinel/internal/config"
	"github.com/justinwoo280/sentinel/internal/install"
	"github.com/justinwoo280/sentinel/internal/master"
	ewp "github.com/justinwoo280/sing-ewp"
	"github.com/spf13/cobra"
)

func newMasterCommand() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "master",
		Short: "Run as the control-plane Master (Telegram + EWP control server)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.LoadMaster(configPath)
			if err != nil {
				return err
			}
			// Ensure key exists before creating master.
			if _, _, err := loadOrCreateStaticKey(cfg.Control.StaticKeyPath); err != nil {
				return err
			}
			m, err := master.New(cfg, nil)
			if err != nil {
				return err
			}
			ctx, cancel := signal.NotifyContext(context.Background(),
				os.Interrupt, syscall.SIGTERM)
			defer cancel()
			return m.Run(ctx)
		},
	}
	cmd.PersistentFlags().StringVarP(&configPath, "config", "c",
		"/etc/sentinel/master.yaml", "path to master config file")

	var installService bool
	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Generate the Master static keypair and a starter config",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return masterInit(configPath, installService)
		},
	}
	initCmd.Flags().BoolVar(&installService, "service", false,
		"also install and enable the sentinel-master systemd unit (or cron fallback)")
	cmd.AddCommand(initCmd)
	return cmd
}

// loadOrCreateStaticKey reads the base64-encoded static private key from
// the given path; if the file does not exist it generates a new keypair
// via sing-ewp and writes the private key with 0600 permissions. In both
// cases the base64 public key is returned so it can be shared with agents.
func loadOrCreateStaticKey(path string) (privB64, pubB64 string, err error) {
	data, err := os.ReadFile(path)
	if err == nil {
		priv := string(data)
		// A derive failure here is non-fatal: the key file exists and is
		// usable by the server; we just can't display the public key.
		pub, _ := derivePublicKey(priv)
		return priv, pub, nil
	}
	if !os.IsNotExist(err) {
		return "", "", fmt.Errorf("master: read static key: %w", err)
	}
	// Ensure parent dir exists.
	if dir := filepath_(path); dir != "" {
		_ = os.MkdirAll(dir, 0700)
	}
	privB64, pubB64, err = ewp.GenerateServerStaticKeypair()
	if err != nil {
		return "", "", fmt.Errorf("master: generate keypair: %w", err)
	}
	if err := os.WriteFile(path, []byte(privB64), 0600); err != nil {
		return "", "", fmt.Errorf("master: write static key: %w", err)
	}
	return privB64, pubB64, nil
}

// derivePublicKey derives the base64 X25519 public key from a base64
// X25519 private key.
func derivePublicKey(privB64 string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(privB64)
	if err != nil {
		return "", err
	}
	priv, err := ecdh.X25519().NewPrivateKey(raw)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(priv.PublicKey().Bytes()), nil
}

func masterInit(configPath string, installService bool) error {
	cfg := config.DefaultMaster()
	// Ensure data dir exists.
	_ = os.MkdirAll(filepath_(cfg.Control.StaticKeyPath), 0700)
	// Ensure config dir exists.
	_ = os.MkdirAll(filepath_(configPath), 0755)

	existed := keyExists(cfg.Control.StaticKeyPath)
	_, pubB64, err := loadOrCreateStaticKey(cfg.Control.StaticKeyPath)
	if err != nil {
		return err
	}
	if err := config.SaveMaster(configPath, cfg); err != nil {
		return err
	}
	if existed {
		fmt.Printf("Master static key already exists at %s (preserved)\n", cfg.Control.StaticKeyPath)
	} else {
		fmt.Printf("Master static key generated at %s\n", cfg.Control.StaticKeyPath)
	}
	fmt.Printf("Master config written to %s\n", configPath)
	if pubB64 != "" {
		fmt.Printf("Static public key (share with agents): %s\n", pubB64)
	}

	if installService {
		if err := install.InstallService(install.RoleMaster); err != nil {
			return fmt.Errorf("master init: install service: %w", err)
		}
	} else {
		fmt.Println("\nNext: edit the config to add your Telegram token, then run:")
		fmt.Println("  sentinel master init --service   # to install the systemd unit")
		fmt.Println("  sentinel master                  # to run in the foreground")
	}
	return nil
}

// keyExists reports whether the static key file already exists.
func keyExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

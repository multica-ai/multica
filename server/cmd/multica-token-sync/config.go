package main

import (
	"flag"
	"fmt"
	"os/user"
	"time"
)

type Config struct {
	Once            bool          // exit after one sync
	Interval        time.Duration // daemon-mode tick interval (only used when Once=false)
	Context         string        // kubeconfig context (empty = current)
	Namespace       string        // cluster namespace holding the broker state Secret
	SecretName      string        // broker state Secret name
	KeychainService string        // macOS keychain service name to write
	KeychainAccount string        // macOS keychain account (default $USER)
	DryRun          bool          // print intended Keychain write, don't perform it
	Verbose         bool          // slog level → debug
}

// ParseFlags accepts an args slice (not os.Args[1:]) so tests can drive it.
// Setting --interval implies --once=false.
func ParseFlags(args []string) (*Config, error) {
	fs := flag.NewFlagSet("multica-token-sync", flag.ContinueOnError)
	cfg := &Config{}
	fs.BoolVar(&cfg.Once, "once", true, "run a single sync and exit (default)")
	fs.DurationVar(&cfg.Interval, "interval", 30*time.Minute, "daemon mode: sync every interval (≥10s)")
	fs.StringVar(&cfg.Context, "context", "", "kubeconfig context (default: current)")
	fs.StringVar(&cfg.Namespace, "namespace", "multica", "cluster namespace holding the broker state Secret")
	fs.StringVar(&cfg.SecretName, "secret", "multica-claude-oauth-broker", "broker state Secret name")
	fs.StringVar(&cfg.KeychainService, "keychain-service", "Claude Code-credentials", "macOS Keychain service")
	fs.StringVar(&cfg.KeychainAccount, "keychain-account", "", "macOS Keychain account (default: $USER)")
	fs.BoolVar(&cfg.DryRun, "dry-run", false, "print intended Keychain write, don't perform it")
	fs.BoolVar(&cfg.Verbose, "verbose", false, "verbose (debug-level) logging")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	intervalSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "interval" {
			intervalSet = true
		}
	})
	if intervalSet {
		cfg.Once = false
	}

	if cfg.KeychainAccount == "" {
		u, err := user.Current()
		if err != nil {
			return nil, fmt.Errorf("resolve current user: %w", err)
		}
		cfg.KeychainAccount = u.Username
	}
	if !cfg.Once && cfg.Interval < 10*time.Second {
		return nil, fmt.Errorf("--interval must be ≥ 10s (got %v)", cfg.Interval)
	}
	return cfg, nil
}

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config drives the repocache server. Env vars carry secrets and addresses;
// the YAML carries the list of workspaces whose repos to mirror. The Helm
// chart writes the YAML into a ConfigMap mounted at $REPOCACHE_CONFIG_DIR
// (default /etc/repocache).
type Config struct {
	ServerBaseURL string
	Token         string

	Workspaces []WorkspaceConfig `yaml:"workspaces"`

	RepoRoot      string
	FetchInterval time.Duration

	AdminAddr   string // e.g. ":8080"
	MetricsAddr string // e.g. ":9090"
}

// WorkspaceConfig is a structural subset of the controller's runtime.yaml
// schema; the repocache only needs the workspace id.
type WorkspaceConfig struct {
	ID string `yaml:"id"`
}

func LoadConfig() (*Config, error) {
	cfg := &Config{
		RepoRoot:      "/repos",
		FetchInterval: 60 * time.Second,
		AdminAddr:     ":8080",
		MetricsAddr:   ":9090",
	}

	cfg.ServerBaseURL = strings.TrimRight(os.Getenv("MULTICA_SERVER_URL"), "/")
	if cfg.ServerBaseURL == "" {
		return nil, fmt.Errorf("MULTICA_SERVER_URL not set")
	}
	cfg.Token = strings.TrimSpace(os.Getenv("MULTICA_TOKEN"))
	if cfg.Token == "" {
		return nil, fmt.Errorf("MULTICA_TOKEN not set")
	}
	if v := os.Getenv("REPOCACHE_REPO_ROOT"); v != "" {
		cfg.RepoRoot = v
	}
	if v := os.Getenv("REPOCACHE_FETCH_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("parse REPOCACHE_FETCH_INTERVAL: %w", err)
		}
		cfg.FetchInterval = d
	}
	if v := os.Getenv("REPOCACHE_ADMIN_ADDR"); v != "" {
		cfg.AdminAddr = v
	}
	if v := os.Getenv("REPOCACHE_METRICS_ADDR"); v != "" {
		cfg.MetricsAddr = v
	}

	dir := os.Getenv("REPOCACHE_CONFIG_DIR")
	if dir == "" {
		dir = "/etc/repocache"
	}
	y, err := os.ReadFile(filepath.Join(dir, "runtime.yaml"))
	if err != nil {
		return nil, fmt.Errorf("read runtime.yaml from %s: %w", dir, err)
	}
	if err := yaml.Unmarshal(y, cfg); err != nil {
		return nil, fmt.Errorf("parse runtime.yaml: %w", err)
	}
	if len(cfg.Workspaces) == 0 {
		return nil, fmt.Errorf("no workspaces configured in runtime.yaml")
	}
	for i, w := range cfg.Workspaces {
		if strings.TrimSpace(w.ID) == "" {
			return nil, fmt.Errorf("workspaces[%d].id required", i)
		}
	}
	return cfg, nil
}

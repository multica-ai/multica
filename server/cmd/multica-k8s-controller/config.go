package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config drives the controller. Env vars carry secrets and pod identity;
// the YAML file carries the per-workspace runtime declarations and Job spec
// knobs (image, sizing). The Helm chart writes the YAML into a ConfigMap
// mounted at $CONTROLLER_CONFIG_DIR.
type Config struct {
	ServerBaseURL string
	Token         string
	Namespace     string

	Workspaces      []WorkspaceConfig `yaml:"workspaces"`
	ImagePullSecret string            `yaml:"imagePullSecret"`

	PollInterval      time.Duration
	HeartbeatInterval time.Duration

	DaemonIDPrefix string // for register payload's daemon_id; default "k8s-controller"
	DeviceName     string // human-readable runtime name; default "multica-cluster"
	CLIVersion     string // multica CLI version reported to the server; set by main from -ldflags
}

type WorkspaceConfig struct {
	ID           string `yaml:"id"`
	Provider     string `yaml:"provider"`
	AgentName    string `yaml:"agentName"`
	RuntimeImage string `yaml:"runtimeImage"`
	PVCSize      string `yaml:"pvcSize"`
	StorageClass string `yaml:"storageClass"`
}

func LoadConfig() (*Config, error) {
	cfg := &Config{
		PollInterval:      3 * time.Second,
		HeartbeatInterval: 15 * time.Second,
		DaemonIDPrefix:    "k8s-controller",
		DeviceName:        "multica-cluster",
	}

	cfg.ServerBaseURL = strings.TrimRight(os.Getenv("MULTICA_SERVER_URL"), "/")
	if cfg.ServerBaseURL == "" {
		return nil, fmt.Errorf("MULTICA_SERVER_URL not set")
	}
	cfg.Token = strings.TrimSpace(os.Getenv("MULTICA_TOKEN"))
	if cfg.Token == "" {
		return nil, fmt.Errorf("MULTICA_TOKEN not set")
	}
	cfg.Namespace = strings.TrimSpace(os.Getenv("POD_NAMESPACE"))
	if cfg.Namespace == "" {
		return nil, fmt.Errorf("POD_NAMESPACE not set (use the downward API)")
	}

	if v := os.Getenv("DAEMON_ID_PREFIX"); v != "" {
		cfg.DaemonIDPrefix = v
	}
	if v := os.Getenv("DEVICE_NAME"); v != "" {
		cfg.DeviceName = v
	}

	dir := os.Getenv("CONTROLLER_CONFIG_DIR")
	if dir == "" {
		dir = "/etc/controller"
	}
	yamlBytes, err := os.ReadFile(filepath.Join(dir, "runtime.yaml"))
	if err != nil {
		return nil, fmt.Errorf("read runtime.yaml from %s: %w", dir, err)
	}
	if err := yaml.Unmarshal(yamlBytes, cfg); err != nil {
		return nil, fmt.Errorf("parse runtime.yaml: %w", err)
	}
	if len(cfg.Workspaces) == 0 {
		return nil, fmt.Errorf("no workspaces configured in runtime.yaml")
	}
	for i, w := range cfg.Workspaces {
		if w.ID == "" || w.Provider == "" || w.RuntimeImage == "" {
			return nil, fmt.Errorf("workspace[%d]: id, provider, runtimeImage are required", i)
		}
		if w.PVCSize == "" {
			cfg.Workspaces[i].PVCSize = "5Gi"
		}
		if w.AgentName == "" {
			cfg.Workspaces[i].AgentName = "multica-cluster"
		}
	}
	if cfg.ImagePullSecret == "" {
		cfg.ImagePullSecret = "ghcr-pull"
	}

	return cfg, nil
}

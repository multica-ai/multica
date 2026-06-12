package main

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Config is the broker's runtime configuration. Populated from env vars set
// by the Deployment (Helm chart in Task 12) — POD_NAMESPACE comes from the
// downward API, the rest from values.yaml.
type Config struct {
	Namespace         string
	SecretName        string
	AccessTokenSecret string // write-only mirror for worker pods to read

	// Refresh strategy.
	RefreshPad      time.Duration // refresh when expires_at - now < RefreshPad
	RefreshInterval time.Duration // ticker cadence for refresh checks
	LeaseName       string        // coordination.k8s.io/Lease name for leader election
	LeaseTTL        time.Duration // leader-elector lease duration

	// Plan-usage polling. The usage endpoint is account-global and 429s
	// below ~180s, so the floor is clamped hard regardless of config.
	UsageInterval time.Duration // cadence for polling the plan-usage endpoint

	AdminAddr   string // cluster-reachable: GET /access_token, /healthz, /readyz
	OpsAddr     string // loopback-only: POST /refresh (kubectl exec only)
	MetricsAddr string // cluster-reachable: /metrics
}

func LoadConfig() (*Config, error) {
	cfg := &Config{
		RefreshPad:      5 * time.Minute,
		RefreshInterval: 60 * time.Second,
		LeaseName:       "multica-claude-broker-refresh",
		LeaseTTL:        30 * time.Second,
		UsageInterval:   5 * time.Minute,
		AdminAddr:       ":8080",
		OpsAddr:         "127.0.0.1:8081",
		MetricsAddr:     ":9090",
	}
	cfg.Namespace = strings.TrimSpace(os.Getenv("POD_NAMESPACE"))
	if cfg.Namespace == "" {
		return nil, fmt.Errorf("POD_NAMESPACE not set (use the downward API)")
	}
	cfg.SecretName = strings.TrimSpace(os.Getenv("BROKER_SECRET_NAME"))
	if cfg.SecretName == "" {
		cfg.SecretName = "multica-claude-oauth-broker"
	}
	cfg.AccessTokenSecret = strings.TrimSpace(os.Getenv("BROKER_ACCESS_TOKEN_SECRET"))
	if cfg.AccessTokenSecret == "" {
		cfg.AccessTokenSecret = "multica-claude-broker-access-token"
	}
	if v := os.Getenv("BROKER_REFRESH_PAD"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("BROKER_REFRESH_PAD: %w", err)
		}
		cfg.RefreshPad = d
	}
	if v := os.Getenv("BROKER_REFRESH_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("BROKER_REFRESH_INTERVAL: %w", err)
		}
		cfg.RefreshInterval = d
	}
	if v := os.Getenv("BROKER_USAGE_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("BROKER_USAGE_INTERVAL: %w", err)
		}
		cfg.UsageInterval = d
	}
	// Hard floor: the usage endpoint returns persistent 429s when polled
	// faster than ~180s, so never let config drop below it.
	if cfg.UsageInterval < usageIntervalFloor {
		cfg.UsageInterval = usageIntervalFloor
	}
	if v := os.Getenv("BROKER_LEASE_NAME"); v != "" {
		cfg.LeaseName = v
	}
	if v := os.Getenv("BROKER_ADMIN_ADDR"); v != "" {
		cfg.AdminAddr = v
	}
	if v := os.Getenv("BROKER_OPS_ADDR"); v != "" {
		cfg.OpsAddr = v
	}
	if v := os.Getenv("BROKER_METRICS_ADDR"); v != "" {
		cfg.MetricsAddr = v
	}
	return cfg, nil
}

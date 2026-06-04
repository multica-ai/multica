package github

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config is the per-workspace connector configuration. In the default
// local / self-host mode every field is sourced from the environment;
// Token is read from MULTICA_GITHUB_TOKEN rather than the encrypted DB
// column (the column exists for a future multi-workspace install flow).
type Config struct {
	Token         string
	Org           string
	ProjectNumber int
	WorkspaceID   string
	PollInterval  time.Duration
}

// Env var names.
const (
	EnvToken         = "MULTICA_GITHUB_TOKEN"
	EnvOrg           = "MULTICA_GITHUB_PROJECT_OWNER"
	EnvProjectNumber = "MULTICA_GITHUB_PROJECT_NUMBER"
	EnvWorkspaceID   = "MULTICA_GITHUB_WORKSPACE_ID"
	EnvPollInterval  = "MULTICA_GITHUB_POLL_INTERVAL"
)

// ConfigFromEnv reads the connector config. enabled is false (with no
// error) when MULTICA_GITHUB_TOKEN is unset — self-host deployments that
// have not opted in pay no goroutine or network cost. When the token is
// set but a required companion var is missing, it returns an error so the
// misconfiguration is loud rather than silent.
func ConfigFromEnv() (cfg Config, enabled bool, err error) {
	token := os.Getenv(EnvToken)
	if token == "" {
		return Config{}, false, nil
	}
	cfg.Token = token
	cfg.Org = os.Getenv(EnvOrg)
	cfg.WorkspaceID = os.Getenv(EnvWorkspaceID)
	if cfg.Org == "" {
		return Config{}, true, fmt.Errorf("%s is set but %s is empty", EnvToken, EnvOrg)
	}
	if cfg.WorkspaceID == "" {
		return Config{}, true, fmt.Errorf("%s is set but %s is empty", EnvToken, EnvWorkspaceID)
	}
	num := os.Getenv(EnvProjectNumber)
	if num == "" {
		return Config{}, true, fmt.Errorf("%s is set but %s is empty", EnvToken, EnvProjectNumber)
	}
	cfg.ProjectNumber, err = strconv.Atoi(num)
	if err != nil {
		return Config{}, true, fmt.Errorf("%s must be an integer: %w", EnvProjectNumber, err)
	}
	cfg.PollInterval = 60 * time.Second
	if iv := os.Getenv(EnvPollInterval); iv != "" {
		if d, perr := time.ParseDuration(iv); perr == nil && d >= 10*time.Second {
			cfg.PollInterval = d
		}
	}
	return cfg, true, nil
}

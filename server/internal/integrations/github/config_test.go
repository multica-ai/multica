package github

import (
	"testing"
	"time"
)

// clearGitHubEnv blanks every connector env var for a clean slate. t.Setenv
// restores the prior value at test end, so this is safe across cases.
func clearGitHubEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{EnvToken, EnvOrg, EnvProjectNumber, EnvWorkspaceID, EnvPollInterval} {
		t.Setenv(k, "")
	}
}

// TestConfigFromEnv_DisabledWhenNoToken: the connector must cost nothing
// (no goroutine, no error) when the operator has not opted in.
func TestConfigFromEnv_DisabledWhenNoToken(t *testing.T) {
	clearGitHubEnv(t)
	// Even with companion vars set, no token means disabled-and-silent.
	t.Setenv(EnvOrg, "katalon-studio")
	t.Setenv(EnvProjectNumber, "16")
	t.Setenv(EnvWorkspaceID, "ws-1")

	cfg, enabled, err := ConfigFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enabled {
		t.Fatalf("enabled = true; want false when %s is unset", EnvToken)
	}
	if cfg != (Config{}) {
		t.Errorf("cfg = %+v; want zero value when disabled", cfg)
	}
}

// TestConfigFromEnv_LoudOnMisconfig: a token set with a missing companion
// var must fail loudly (enabled=true, err!=nil) rather than silently no-op.
func TestConfigFromEnv_LoudOnMisconfig(t *testing.T) {
	cases := []struct {
		name   string
		org    string
		ws     string
		number string
	}{
		{"missing org", "", "ws-1", "16"},
		{"missing workspace", "katalon-studio", "", "16"},
		{"missing number", "katalon-studio", "ws-1", ""},
		{"non-integer number", "katalon-studio", "ws-1", "sixteen"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clearGitHubEnv(t)
			t.Setenv(EnvToken, "ghp_test")
			t.Setenv(EnvOrg, c.org)
			t.Setenv(EnvWorkspaceID, c.ws)
			t.Setenv(EnvProjectNumber, c.number)

			_, enabled, err := ConfigFromEnv()
			if !enabled {
				t.Errorf("enabled = false; want true (token is set)")
			}
			if err == nil {
				t.Errorf("err = nil; want a misconfiguration error")
			}
		})
	}
}

func TestConfigFromEnv_DefaultsAndPollInterval(t *testing.T) {
	base := func(t *testing.T) {
		clearGitHubEnv(t)
		t.Setenv(EnvToken, "ghp_test")
		t.Setenv(EnvOrg, "katalon-studio")
		t.Setenv(EnvWorkspaceID, "ws-1")
		t.Setenv(EnvProjectNumber, "16")
	}

	t.Run("happy path with default poll", func(t *testing.T) {
		base(t)
		cfg, enabled, err := ConfigFromEnv()
		if err != nil || !enabled {
			t.Fatalf("enabled=%v err=%v; want enabled,no-error", enabled, err)
		}
		if cfg.Org != "katalon-studio" || cfg.ProjectNumber != 16 || cfg.WorkspaceID != "ws-1" || cfg.Token != "ghp_test" {
			t.Errorf("cfg = %+v; field mismatch", cfg)
		}
		if cfg.PollInterval != 60*time.Second {
			t.Errorf("PollInterval = %v; want default 60s", cfg.PollInterval)
		}
	})

	t.Run("valid override applied", func(t *testing.T) {
		base(t)
		t.Setenv(EnvPollInterval, "5m")
		cfg, _, err := ConfigFromEnv()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.PollInterval != 5*time.Minute {
			t.Errorf("PollInterval = %v; want 5m", cfg.PollInterval)
		}
	})

	t.Run("sub-floor override ignored (keeps 60s)", func(t *testing.T) {
		base(t)
		t.Setenv(EnvPollInterval, "2s") // below the 10s floor
		cfg, _, err := ConfigFromEnv()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.PollInterval != 60*time.Second {
			t.Errorf("PollInterval = %v; want 60s (sub-floor ignored)", cfg.PollInterval)
		}
	})

	t.Run("garbage override ignored (keeps 60s)", func(t *testing.T) {
		base(t)
		t.Setenv(EnvPollInterval, "not-a-duration")
		cfg, _, err := ConfigFromEnv()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.PollInterval != 60*time.Second {
			t.Errorf("PollInterval = %v; want 60s (garbage ignored)", cfg.PollInterval)
		}
	})
}

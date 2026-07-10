package traceupload

import (
	"path/filepath"
	"testing"
	"time"
)

func TestConfigFromEnvDefaultsOff(t *testing.T) {
	// No env set → disabled, safe defaults.
	t.Setenv(EnvEnabled, "")
	c := ConfigFromEnv("/ws/root")
	if c.Enabled {
		t.Error("flag must default off")
	}
	if c.MaxAge != DefaultMaxAge {
		t.Errorf("MaxAge=%v", c.MaxAge)
	}
	if c.MaxConcurrentPerWorkspace != DefaultMaxConcurrentPerWorkspace {
		t.Errorf("concurrency=%d", c.MaxConcurrentPerWorkspace)
	}
	if c.MaxAttempts != DefaultMaxAttempts {
		t.Errorf("MaxAttempts=%d want default %d", c.MaxAttempts, DefaultMaxAttempts)
	}
	if len(c.Backoff) != len(DefaultBackoff) || c.Backoff[len(c.Backoff)-1] != time.Hour {
		t.Errorf("Backoff not the progressive default: %v", c.Backoff)
	}
	if c.StateDir != filepath.Join("/ws/root", ".trace-upload") {
		t.Errorf("StateDir=%q", c.StateDir)
	}
}

func TestConfigFromEnvMaxAttemptsOverride(t *testing.T) {
	t.Setenv(EnvMaxAttempts, "3")
	if c := ConfigFromEnv("/ws"); c.MaxAttempts != 3 {
		t.Errorf("MaxAttempts override not applied: %d", c.MaxAttempts)
	}
	// Explicit 0 disables the cap (MaxAge still applies).
	t.Setenv(EnvMaxAttempts, "0")
	if c := ConfigFromEnv("/ws"); c.MaxAttempts != 0 {
		t.Errorf("MaxAttempts=0 (disabled) not honored: %d", c.MaxAttempts)
	}
}

func TestConfigFromEnvEnabled(t *testing.T) {
	t.Setenv(EnvEnabled, "true")
	t.Setenv(EnvEndpoint, "https://registry.example.com/api/agent-traces/ingest")
	t.Setenv(EnvAPIKey, "key-123")
	t.Setenv(EnvMaxConcurrency, "5")
	t.Setenv(EnvMaxAgeHours, "48")
	c := ConfigFromEnv("/ws")
	if !c.Enabled {
		t.Error("should be enabled")
	}
	if c.Endpoint == "" || c.APIKey != "key-123" {
		t.Errorf("endpoint/key not read: %+v", c)
	}
	if c.MaxConcurrentPerWorkspace != 5 {
		t.Errorf("concurrency=%d", c.MaxConcurrentPerWorkspace)
	}
	if c.MaxAge != 48*time.Hour {
		t.Errorf("maxage=%v", c.MaxAge)
	}
}

func TestParseBoolVariants(t *testing.T) {
	for _, v := range []string{"1", "true", "TRUE", "yes", "on", "  true  "} {
		if !parseBool(v) {
			t.Errorf("parseBool(%q) should be true", v)
		}
	}
	for _, v := range []string{"", "0", "false", "off", "no", "nonsense"} {
		if parseBool(v) {
			t.Errorf("parseBool(%q) should be false", v)
		}
	}
}

package traceupload

import (
	"path/filepath"
	"testing"
	"time"
)

// TECH-3621 fleet rollout: ConfigFromValues turns server-delivered endpoint/key
// into a ready-to-run Config so a single backend setting enables trace upload
// fleet-wide without the key living in each machine's env.
func TestConfigFromValuesEnabled(t *testing.T) {
	c := ConfigFromValues("/ws/root", "  https://reg.example.com/api/agent-traces/ingest  ", "  key-123  ", 0)
	if !c.Enabled {
		t.Error("ConfigFromValues must mark the config enabled")
	}
	if c.Endpoint != "https://reg.example.com/api/agent-traces/ingest" {
		t.Errorf("Endpoint not trimmed: %q", c.Endpoint)
	}
	if c.APIKey != "key-123" {
		t.Errorf("APIKey not trimmed: %q", c.APIKey)
	}
	if c.MaxAge != DefaultMaxAge {
		t.Errorf("MaxAge should default when maxAgeHours<=0: %v", c.MaxAge)
	}
	if c.MaxConcurrentPerWorkspace != DefaultMaxConcurrentPerWorkspace {
		t.Errorf("concurrency=%d", c.MaxConcurrentPerWorkspace)
	}
	if c.StateDir != filepath.Join("/ws/root", ".trace-upload") {
		t.Errorf("StateDir=%q", c.StateDir)
	}
}

func TestConfigFromValuesMaxAgeOverride(t *testing.T) {
	c := ConfigFromValues("/ws/root", "https://e", "k", 48)
	if c.MaxAge != 48*time.Hour {
		t.Errorf("MaxAge override not applied: %v", c.MaxAge)
	}
}

package handler

// CEREBRO-PATCH(trace-config-handler-test): TECH-3621 — tests for the net-new
// fleet trace-config handler (net-new fork _test.go in the upstream-zone path).

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/cerebro/traceupload"
)

// traceConfigFromEnv is the pure env→response mapping behind GetTraceConfig.
// The handler wraps it with requireDaemonRuntimeAccess (DB), so the security
// behaviour is exercised at the router level; here we lock the secret-handling
// rule: the key is returned ONLY when both endpoint and key are set.
func TestTraceConfigFromEnvEnabledWhenBothSet(t *testing.T) {
	t.Setenv(traceupload.EnvEndpoint, "https://reg.example.com/api/agent-traces/ingest")
	t.Setenv(traceupload.EnvAPIKey, "key-xyz")
	t.Setenv(traceupload.EnvMaxAgeHours, "48")

	got := traceConfigFromEnv()
	if !got.Enabled || got.Endpoint == "" || got.APIKey == "" {
		t.Fatalf("expected enabled config, got %+v", got)
	}
	if got.MaxAgeHours != 48 {
		t.Errorf("MaxAgeHours=%d, want 48", got.MaxAgeHours)
	}
}

func TestTraceConfigFromEnvDisabledNeverLeaksKey(t *testing.T) {
	// Endpoint missing but key set: must report disabled AND withhold the key.
	t.Setenv(traceupload.EnvEndpoint, "")
	t.Setenv(traceupload.EnvAPIKey, "key-should-not-leak")

	got := traceConfigFromEnv()
	if got.Enabled {
		t.Errorf("expected disabled when endpoint missing, got %+v", got)
	}
	if got.APIKey != "" || got.Endpoint != "" {
		t.Errorf("disabled response must not include secrets, got %+v", got)
	}
}

package agentroute

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMergeRuntimeConfigPreservesPolicyAndNestedSecrets(t *testing.T) {
	t.Parallel()

	merged, err := MergeRuntimeConfig(
		[]byte(`{
			"provider_failover_protected": true,
			"adaptive_routing": {"enabled": true},
			"baseline": true,
			"gateway": {"endpoint": "old", "token": "secret"}
		}`),
		[]byte(`{
			"provider_mode": "test",
			"gateway": {"endpoint": "new"}
		}`),
	)
	if err != nil {
		t.Fatalf("MergeRuntimeConfig: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(merged, &got); err != nil {
		t.Fatalf("decode merged config: %v", err)
	}
	if got["provider_failover_protected"] != true || got["baseline"] != true ||
		got["provider_mode"] != "test" {
		t.Fatalf("top-level merge lost source or override values: %#v", got)
	}
	gateway, ok := got["gateway"].(map[string]any)
	if !ok || gateway["endpoint"] != "new" || gateway["token"] != "secret" {
		t.Fatalf("nested merge lost gateway data: %#v", got["gateway"])
	}
}

func TestValidateRuntimeConfigOverrideRejectsAuthorityAndNonObjects(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		`{"provider_failover_protected": false}`,
		`{"provider_failover_target": true}`,
		`{"adaptive_routing": {"enabled": false}}`,
		`{"provider": "other"}`,
		`{"runtime_name": "other"}`,
		`["not", "an", "object"]`,
	} {
		if err := ValidateRuntimeConfigOverride([]byte(raw)); err == nil {
			t.Errorf("ValidateRuntimeConfigOverride(%s) succeeded, want error", raw)
		}
	}

	if err := ValidateRuntimeConfigOverride([]byte(`{"gateway":{"endpoint":"new"}}`)); err != nil {
		t.Fatalf("execution-only override rejected: %v", err)
	}
	if err := ValidateRuntimeConfigOverride([]byte(`null`)); err != nil {
		t.Fatalf("optional null override rejected: %v", err)
	}
}

func TestMergeRuntimeConfigReportsMalformedSource(t *testing.T) {
	t.Parallel()

	_, err := MergeRuntimeConfig([]byte(`["bad source"]`), []byte(`{"mode":"gateway"}`))
	if err == nil || !strings.Contains(err.Error(), "source runtime_config") {
		t.Fatalf("error = %v, want source runtime_config failure", err)
	}
}

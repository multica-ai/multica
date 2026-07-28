package grantrecovery

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestTransformPreservesGenericAndRegistrySemantics(t *testing.T) {
	grants := []LegacyGrant{
		{WorkspaceID: "workspace", AgentID: "agent", ToolName: "plain", Enabled: true},
		{WorkspaceID: "workspace", AgentID: "agent", ToolName: "blocked", Enabled: false},
		{
			WorkspaceID: "workspace", AgentID: "agent", ToolName: "firtal_registry", Enabled: true,
			Config: json.RawMessage(`{"allowed_data_sources":["source-b","source-a"],"allowed_apps":true,"allow_write":false}`),
		},
	}

	rules, unmapped := Transform(grants)
	if len(unmapped) != 0 {
		t.Fatalf("unmapped = %+v", unmapped)
	}
	if len(rules) != 5 {
		t.Fatalf("rules = %d, want generic allow + deny + three Registry rules: %+v", len(rules), rules)
	}
	byKey := map[string]Rule{}
	for _, rule := range rules {
		byKey[rule.ToolKey+"/"+rule.ResourcePattern] = rule
	}
	if byKey["plain/"].Setting != "allow" || byKey["blocked/"].Setting != "deny" {
		t.Fatalf("generic settings were not preserved: %+v", byKey)
	}
	if byKey["firtal_registry/action:list_apps"].Setting != "allow" || byKey["firtal_registry/action:update_app"].Setting != "deny" {
		t.Fatalf("Registry action settings were not preserved: %+v", byKey)
	}
	var conditions map[string]any
	if err := json.Unmarshal(byKey["firtal_registry/"].Conditions, &conditions); err != nil {
		t.Fatalf("decode Registry conditions: %v", err)
	}
	if conditions["arg_allowlist"] == nil {
		t.Fatalf("Registry data-source scope missing: %+v", conditions)
	}
}

func TestTransformQuarantinesUnknownToolConfig(t *testing.T) {
	grant := LegacyGrant{
		WorkspaceID: "workspace", AgentID: "agent", ToolName: "custom_tool", Enabled: true,
		Config: json.RawMessage(`{"tenant":"one"}`),
	}
	rules, unmapped := Transform([]LegacyGrant{grant})
	if len(rules) != 1 || rules[0].Setting != "allow" {
		t.Fatalf("generic decision must remain visible in the diff: %+v", rules)
	}
	if len(unmapped) != 1 || unmapped[0].Reason == "" {
		t.Fatalf("unknown config must block apply with an explanation: %+v", unmapped)
	}
}

func TestBuildDiffIsDeterministicIdempotentAndConflictSafe(t *testing.T) {
	grants := []LegacyGrant{
		{WorkspaceID: "workspace", AgentID: "agent", ToolName: "new", Enabled: true},
		{WorkspaceID: "workspace", AgentID: "agent", ToolName: "same", Enabled: false},
		{WorkspaceID: "workspace", AgentID: "agent", ToolName: "conflict", Enabled: true},
	}
	existing := []Rule{
		{WorkspaceID: "workspace", AgentID: "agent", ToolKey: "same", Setting: "deny"},
		{WorkspaceID: "workspace", AgentID: "agent", ToolKey: "conflict", Setting: "deny"},
	}
	diff := BuildDiff(grants, existing, map[string]bool{"agent": true})
	if len(diff.Mapped) != 1 || diff.Mapped[0].ToolKey != "new" {
		t.Fatalf("mapped = %+v", diff.Mapped)
	}
	if len(diff.AlreadyPresent) != 1 || diff.AlreadyPresent[0].ToolKey != "same" {
		t.Fatalf("already = %+v", diff.AlreadyPresent)
	}
	if len(diff.Conflicting) != 1 || diff.SafeToApply() {
		t.Fatalf("conflicts = %+v, safe = %v", diff.Conflicting, diff.SafeToApply())
	}

	second := BuildDiff(grants, append(existing, diff.Mapped...), map[string]bool{"agent": true})
	if len(second.Mapped) != 0 || len(second.AlreadyPresent) != 2 {
		t.Fatalf("second diff is not idempotent: %+v", second)
	}
	if diff.SourceFingerprint != second.SourceFingerprint {
		t.Fatal("source fingerprint changed for identical legacy input")
	}
}

func TestBuildDiffRejectsMissingTargetAgent(t *testing.T) {
	grant := LegacyGrant{WorkspaceID: "workspace", AgentID: "deleted", ToolName: "plain", Enabled: true}
	diff := BuildDiff([]LegacyGrant{grant}, nil, map[string]bool{})
	if diff.SafeToApply() || len(diff.Unmapped) != 1 {
		t.Fatalf("missing target actor must stop apply: %+v", diff)
	}
}

func TestFingerprintIgnoresInputOrder(t *testing.T) {
	one := LegacyGrant{WorkspaceID: "w", AgentID: "a", ToolName: "one", Enabled: true}
	two := LegacyGrant{WorkspaceID: "w", AgentID: "a", ToolName: "two", Enabled: false}
	if !reflect.DeepEqual(Fingerprint([]LegacyGrant{one, two}), Fingerprint([]LegacyGrant{two, one})) {
		t.Fatal("fingerprint must be stable across source row order")
	}
}

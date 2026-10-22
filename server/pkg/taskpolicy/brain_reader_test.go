package taskpolicy

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestNewBrainMCPAcceptanceUsesFixedProviderAndExactToolSet(t *testing.T) {
	policy, err := NewBrainMCPAcceptance(Request{
		Mode:       BrainMCPAcceptanceMode,
		MCPServer:  "brain-reader",
		ProjectIDs: []string{BrainProjectID, MulticaProjectID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if policy.Provider != BrainMCPReadOnlyProvider {
		t.Fatalf("provider = %q", policy.Provider)
	}
	want := []string{
		"mcp__brain-reader__brain_resolve",
		"mcp__brain-reader__brain_context",
		"mcp__brain-reader__brain_read",
		"mcp__brain-reader__brain_company_projects",
		"mcp__brain-reader__brain_projects",
		"mcp__brain-reader__company_events",
	}
	if !reflect.DeepEqual(policy.Tools, want) {
		t.Fatalf("tools = %#v, want %#v", policy.Tools, want)
	}
}

func TestNewBrainMCPAcceptanceRejectsInvalidModeOrServerOrProject(t *testing.T) {
	for _, request := range []Request{
		{Mode: "other", MCPServer: "brain"},
		{Mode: BrainMCPAcceptanceMode, MCPServer: ""},
		{Mode: BrainMCPAcceptanceMode, MCPServer: "../brain", ProjectIDs: []string{BrainProjectID, MulticaProjectID}},
		{Mode: BrainMCPAcceptanceMode, MCPServer: "brain"},
		{Mode: BrainMCPAcceptanceMode, MCPServer: "brain", ProjectIDs: []string{BrainProjectID}},
		{Mode: BrainMCPAcceptanceMode, MCPServer: "brain", ProjectIDs: []string{BrainProjectID, MulticaProjectID, "other"}},
	} {
		if _, err := NewBrainMCPAcceptance(request); err == nil {
			t.Errorf("NewBrainMCPAcceptance(%+v) succeeded", request)
		}
	}
}

func TestValidateRequiresExactUniqueAllowlist(t *testing.T) {
	policy, err := NewBrainMCPAcceptance(Request{Mode: BrainMCPAcceptanceMode, MCPServer: "brain", ProjectIDs: []string{BrainProjectID, MulticaProjectID}})
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(policy); err != nil {
		t.Fatal(err)
	}

	mutated := *policy
	mutated.Tools = append(append([]string(nil), policy.Tools...), "mcp__brain__unknown")
	if err := Validate(&mutated); err == nil {
		t.Fatal("accepted an additional tool")
	}
	mutated = *policy
	mutated.Tools[0] = mutated.Tools[1]
	if err := Validate(&mutated); err == nil {
		t.Fatal("accepted a duplicate tool")
	}
	mutated = *policy
	mutated.Provider = "codex"
	if err := Validate(&mutated); err == nil {
		t.Fatal("accepted a different provider")
	}
}

func TestIsBrainMCPReadOnlyToolFailsClosed(t *testing.T) {
	if IsBrainMCPReadOnlyTool(nil, "mcp__brain__brain_read") {
		t.Fatal("nil policy allowed a tool")
	}
	policy, err := NewBrainMCPAcceptance(Request{Mode: BrainMCPAcceptanceMode, MCPServer: "brain", ProjectIDs: []string{BrainProjectID, MulticaProjectID}})
	if err != nil {
		t.Fatal(err)
	}
	if !IsBrainMCPReadOnlyTool(policy, "mcp__brain__brain_read") {
		t.Fatal("exact authorized tool was rejected")
	}
	for _, tool := range []string{"Bash", "Read", "Write", "Edit", "mcp__other__brain_read", "mcp__brain__unknown"} {
		if IsBrainMCPReadOnlyTool(policy, tool) {
			t.Errorf("unauthorized tool %q was allowed", tool)
		}
	}
}

func TestFromTaskContextReadsAndValidatesPolicy(t *testing.T) {
	context := `{"head_sha":"abc","execution_policy":{"mode":"brain_mcp_acceptance_v1","provider":"claude","mcp_server":"brain","project_ids":["brain","multica"],"tools":["mcp__brain__brain_resolve","mcp__brain__brain_context","mcp__brain__brain_read","mcp__brain__brain_company_projects","mcp__brain__brain_projects","mcp__brain__company_events"],"company_events_selector":{"afterId":199900,"throughId":199901,"limit":1}}}`
	policy, err := FromTaskContext([]byte(context))
	if err != nil {
		t.Fatal(err)
	}
	if policy == nil || policy.MCPServer != "brain" {
		t.Fatalf("policy = %#v", policy)
	}
	if policy, err := FromTaskContext([]byte(`{"head_sha":"abc"}`)); err != nil || policy != nil {
		t.Fatalf("ordinary task context: policy=%#v err=%v", policy, err)
	}
	if _, err := FromTaskContext([]byte(`{"execution_policy":{"mode":"brain_mcp_acceptance_v1","provider":"claude","mcp_server":"brain","project_ids":["brain","multica"],"tools":["Bash"]}}`)); err == nil {
		t.Fatal("accepted malformed policy in task context")
	}
}

func TestFilterMCPConfigRetainsOnlySelectedConfiguredServerWithoutMutation(t *testing.T) {
	raw := json.RawMessage(`{"mcpServers":{"brain":{"type":"http","url":"https://brain.invalid/mcp"},"other":{"type":"http","url":"https://other.invalid/mcp"}},"globalSetting":true}`)
	before := append(json.RawMessage(nil), raw...)
	policy, err := NewBrainMCPAcceptance(Request{Mode: BrainMCPAcceptanceMode, MCPServer: "brain", ProjectIDs: []string{BrainProjectID, MulticaProjectID}})
	if err != nil {
		t.Fatal(err)
	}
	filtered, err := FilterMCPConfig(raw, policy)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(raw, before) {
		t.Fatalf("input config mutated: %s", raw)
	}
	var got struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(filtered, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.MCPServers) != 1 || len(got.MCPServers["brain"]) == 0 {
		t.Fatalf("filtered MCP servers = %s", filtered)
	}
}

func TestFilterMCPConfigFailsClosedWhenSelectedServerMissing(t *testing.T) {
	policy, err := NewBrainMCPAcceptance(Request{Mode: BrainMCPAcceptanceMode, MCPServer: "brain", ProjectIDs: []string{BrainProjectID, MulticaProjectID}})
	if err != nil {
		t.Fatal(err)
	}
	for _, config := range []json.RawMessage{
		nil,
		json.RawMessage(`{}`),
		json.RawMessage(`{"mcpServers":{"other":{"type":"http","url":"https://other.invalid"}}}`),
	} {
		if _, err := FilterMCPConfig(config, policy); err == nil {
			t.Errorf("accepted config %s", config)
		}
	}
}

func TestAllowsToolInputPinsExactAcceptanceScopes(t *testing.T) {
	policy, err := NewBrainMCPAcceptance(Request{Mode: BrainMCPAcceptanceMode, MCPServer: "brain", ProjectIDs: []string{BrainProjectID, MulticaProjectID}})
	if err != nil {
		t.Fatal(err)
	}
	revision := strings.Repeat("a", 64)
	for _, tc := range []struct {
		name  string
		tool  string
		input map[string]any
		want  bool
	}{
		{"unknown project exact identity", "brain_resolve", map[string]any{"identity": "not-an-enrolled-project"}, true},
		{"resolve scoped project", "brain_resolve", map[string]any{"identity": "brain"}, false},
		{"resolve other identity", "brain_resolve", map[string]any{"identity": "another-project"}, false},
		{"resolve path alternative", "brain_resolve", map[string]any{"path": "/tmp"}, false},
		{"A33 scoped context", "brain_context", map[string]any{"project": "brain", "q": frozenBrainContextQuestions[BrainProjectID]}, true},
		{"A36 scoped context", "brain_context", map[string]any{"project": "brain", "q": BrainAuthorityQuestion}, true},
		{"A28 scoped context", "brain_context", map[string]any{"project": "multica", "q": frozenBrainContextQuestions[MulticaProjectID]}, true},
		{"context question broadening", "brain_context", map[string]any{"project": "brain", "q": "everything"}, false},
		{"context other project", "brain_context", map[string]any{"project": "other", "q": frozenBrainContextQuestions[BrainProjectID]}, false},
		{"context missing question", "brain_context", map[string]any{"project": "brain"}, false},
		{"read immutable selector", "brain_read", map[string]any{"id": "doc-1", "revision": revision}, true},
		{"read missing revision", "brain_read", map[string]any{"id": "doc-1"}, false},
		{"company projects exact empty args", "brain_company_projects", map[string]any{}, true},
		{"generic projects exact empty args", "brain_projects", map[string]any{}, true},
		{"project discovery with args", "brain_projects", map[string]any{"project": "alpha"}, false},
		{"event exact bounded range", "company_events", map[string]any{"afterId": float64(199900), "throughId": float64(199901), "limit": float64(1)}, true},
		{"event broad scan rejected", "company_events", map[string]any{"afterId": float64(1), "throughId": float64(199901), "limit": float64(1)}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := AllowsToolInput(policy, tc.tool, tc.input); got != tc.want {
				t.Fatalf("AllowsToolInput(%s, %#v) = %t, want %t", tc.tool, tc.input, got, tc.want)
			}
		})
	}
	if AllowsToolInput(nil, "brain_context", map[string]any{"project": "brain", "q": frozenBrainContextQuestions[BrainProjectID]}) {
		t.Fatal("nil policy allowed a tool input")
	}
}

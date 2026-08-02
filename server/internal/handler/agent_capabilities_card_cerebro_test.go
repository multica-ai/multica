package handler

// CEREBRO-PATCH(agent-capabilities-card-test): TECH-3642 unit tests for the
// capabilities-card limits parser.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/capabilitycatalog"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/localtoolpolicy"
	"github.com/multica-ai/multica/server/internal/cerebro/platformcatalog"
	cerebrotoolpolicy "github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type capabilityTableCapture struct {
	query cerebrotoolpolicy.TableQuery
}

type rejectingCapabilityMandate struct{ denied map[string]bool }

func (m rejectingCapabilityMandate) Authorize(_ context.Context, _, _, _ pgtype.UUID, tool string) error {
	if m.denied[tool] {
		return fmt.Errorf("tool is outside task mandate")
	}
	return nil
}

func TestApplyTaskMandateDeniesAPIConnectionEndpointOnCapabilitiesCard(t *testing.T) {
	id := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	card := AgentCapabilities{Connections: []AgentCapabilityConnection{{
		Name:      "infisical-admin",
		Endpoints: []AgentCapabilityConnEndpoint{{Path: "/secrets", Methods: []string{"GET"}, Permission: "allow", Allowed: true, Callable: true}},
	}}}
	ApplyTaskMandate(context.Background(), true, rejectingCapabilityMandate{denied: map[string]bool{"infisical_admin__get_secrets": true}}, id, id, id, &card)
	got := card.Connections[0].Endpoints[0]
	if got.Permission != "deny" || got.Allowed || got.Callable || got.BlockedReason == "" || got.HowToFix == "" {
		t.Fatalf("API endpoint mandate denial must be visible on the capabilities card: %+v", got)
	}
}

func TestApplyTaskMandateDoesNotDenyPlatformActionsAfterRollback(t *testing.T) {
	id := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	card := AgentCapabilities{Tools: []AgentCapabilityTool{{
		Key: "read_issues", Permission: "allow", Allowed: true, Callable: true,
	}}}

	ApplyTaskMandate(context.Background(), true, rejectingCapabilityMandate{
		denied: map[string]bool{"read_issues": true},
	}, id, id, id, &card)

	got := card.Tools[0]
	if got.Permission != "allow" || !got.Allowed || !got.Callable || got.BlockedReason != "" {
		t.Fatalf("platform action must retain its permission decision after Task Mandate enforcement rollback: %+v", got)
	}
}

func TestApplyTaskMandateDisabledPreservesResolvedPermissions(t *testing.T) {
	id := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	card := AgentCapabilities{Connections: []AgentCapabilityConnection{{
		Name: "infisical-admin",
		Endpoints: []AgentCapabilityConnEndpoint{
			{Path: "/allowed", Methods: []string{"GET"}, Permission: "allow", Allowed: true, Callable: true},
			{Path: "/denied", Methods: []string{"GET"}, Permission: "deny", Allowed: false, Callable: false, BlockedReason: "Tool Policy denied the capability"},
		},
	}}}
	mandates := rejectingCapabilityMandate{denied: map[string]bool{
		"infisical_admin__get_allowed": true,
		"infisical_admin__get_denied":  true,
	}}

	ApplyTaskMandate(context.Background(), false, mandates, id, id, id, &card)

	allowed := card.Connections[0].Endpoints[0]
	if allowed.Permission != "allow" || !allowed.Allowed || !allowed.Callable || allowed.BlockedReason != "" {
		t.Fatalf("disabled Task Mandate changed Tool Policy Allow: %+v", allowed)
	}
	denied := card.Connections[0].Endpoints[1]
	if denied.Permission != "deny" || denied.Allowed || denied.Callable || denied.BlockedReason != "Tool Policy denied the capability" {
		t.Fatalf("disabled Task Mandate changed Tool Policy Deny: %+v", denied)
	}
}

func (c *capabilityTableCapture) Table(_ context.Context, in cerebrotoolpolicy.TableQuery) ([]cerebrotoolpolicy.TableRow, error) {
	c.query = in
	return nil, nil
}

func TestAgentCapabilityRowsScopesInventoryToAgentsRuntime(t *testing.T) {
	t.Parallel()

	runtimeID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	agentID := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}
	workspaceID := pgtype.UUID{Bytes: [16]byte{3}, Valid: true}
	capture := &capabilityTableCapture{}
	h := &Handler{CapabilityToolPolicy: capture}

	h.agentCapabilityRows(httptest.NewRequest("GET", "/", nil), workspaceID, runtimeID, agentID)

	if capture.query.RuntimeID != runtimeID {
		t.Fatalf("capabilities runtime scope = %+v, want %+v", capture.query.RuntimeID, runtimeID)
	}
}

func TestPermissionLookupUsesCanonicalRuntimeAndMCPAliases(t *testing.T) {
	t.Parallel()

	rows := []cerebrotoolpolicy.TableRow{
		{
			ToolKey: "tools:bash", Title: "bash", Source: capSourceRuntimeReport,
			Effective: cerebrotoolpolicy.Effective{Setting: cerebrotoolpolicy.SettingAllow},
		},
		{
			ToolKey: "tools:apply_patch", Title: "apply_patch", Source: capSourceRuntimeReport,
			Effective: cerebrotoolpolicy.Effective{Setting: cerebrotoolpolicy.SettingDeny},
		},
		{
			ToolKey: "connection:company-brain", ResourcePattern: "search", Source: capSourceConnectionTool,
			Effective: cerebrotoolpolicy.Effective{Setting: cerebrotoolpolicy.SettingAsk},
		},
	}

	got := permissionLookupFromRows(rows)
	if got["exec_command"] != "allow" {
		t.Fatalf("exec_command permission = %q, want allow", got["exec_command"])
	}
	if got["patch_apply"] != "deny" {
		t.Fatalf("patch_apply permission = %q, want deny", got["patch_apply"])
	}
	if got["mcp__company-brain__search"] != "ask" {
		t.Fatalf("company-brain search permission = %q, want ask", got["mcp__company-brain__search"])
	}
}

func TestCodexPermissionIdentityMatchesCatalogCapabilitiesAndCallTime(t *testing.T) {
	t.Parallel()

	catalog, err := capabilitycatalog.Build(capabilitycatalog.Inventory{
		Runtimes: []capabilitycatalog.RuntimeInventory{{
			Provider: "codex",
			Tools:    []string{"bash"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	observed, ok := catalog.Resolve(capabilitycatalog.RuntimeKey("codex", "exec_command"))
	if !ok {
		t.Fatal("Codex exec_command is absent from the canonical catalog")
	}
	policy, ok := catalog.Resolve(capabilitycatalog.RuntimePolicyKey("codex", "tools:bash", capSourceRuntimeReport))
	if !ok || policy.Capability.ID != observed.Capability.ID {
		t.Fatalf("catalog identities differ: observed=%+v policy=%+v", observed, policy)
	}

	callKey := localtoolpolicy.ProviderPolicyToolKey("codex", "exec_command")
	if callKey != "tools:bash" {
		t.Fatalf("call-time key = %q, want tools:bash", callKey)
	}
	lookup := permissionLookupFromRows([]cerebrotoolpolicy.TableRow{{
		ToolKey: callKey,
		Title:   "bash",
		Source:  capSourceRuntimeReport,
		Effective: cerebrotoolpolicy.Effective{
			Setting: cerebrotoolpolicy.SettingAllow,
		},
	}}, "codex")
	if lookup["exec_command"] != "allow" {
		t.Fatalf("Capabilities observed verdict = %q, want allow", lookup["exec_command"])
	}
}

func TestMergeCanonicalCapabilityToolsCollapsesLiveBridgeAndPlatformRow(t *testing.T) {
	t.Parallel()

	tools := []AgentCapabilityTool{
		{Key: "add_comment", Title: "add_comment", Source: capSourceScan, Permission: "allow", Allowed: true, Enforced: true},
		{Key: "add_comment", Title: "Add comment", Source: platformcatalog.Source, Permission: "allow", Allowed: true, Available: true, Enforced: true},
	}
	got := mergeCanonicalCapabilityTools(tools, "codex")
	if len(got) != 1 {
		t.Fatalf("merged tools = %+v, want one canonical add_comment row", got)
	}
	if got[0].Source != capSourceScan {
		t.Fatalf("merged source = %q, want live scan source", got[0].Source)
	}
}

func TestCapabilityToolFromPlatformRowReportsCallability(t *testing.T) {
	denied := capabilityToolFromRow(cerebrotoolpolicy.TableRow{
		ToolKey:   "hooks:write",
		Source:    platformcatalog.Source,
		Effective: cerebrotoolpolicy.Effective{Setting: cerebrotoolpolicy.SettingDeny, Reason: "Explicit grant required"},
	})
	if denied.Allowed || denied.Callable {
		t.Fatalf("denied hooks:write = %+v, want allowed=false callable=false", denied)
	}
	if !denied.Available || !denied.Enforced {
		t.Fatalf("denied hooks:write = %+v, want registered action available and enforced", denied)
	}
	if denied.BlockedReason != "Explicit grant required" || denied.HowToFix == "" {
		t.Fatalf("denied hooks:write explanation = %+v, want reason and remediation", denied)
	}

	allowed := capabilityToolFromRow(cerebrotoolpolicy.TableRow{
		ToolKey:   "hooks:write",
		Source:    platformcatalog.Source,
		Effective: cerebrotoolpolicy.Effective{Setting: cerebrotoolpolicy.SettingAllow, DecidedBy: cerebrotoolpolicy.LayerAgent},
	})
	if !allowed.Allowed || !allowed.Available || !allowed.Enforced || !allowed.Callable {
		t.Fatalf("allowed hooks:write = %+v, want callable effective action", allowed)
	}

	external := capabilityToolFromRow(cerebrotoolpolicy.TableRow{
		ToolKey:               "autopilot_webhook",
		Source:                platformcatalog.Source,
		ManagedExternally:     true,
		ExternalSecurityOwner: "Autopilot webhook secret",
		Effective:             cerebrotoolpolicy.Effective{Setting: cerebrotoolpolicy.SettingAllow},
	})
	if external.ExternalSecurityOwner != "Autopilot webhook secret" || external.Enforced {
		t.Fatalf("external permission projection = %+v, want named owner and policy-engine enforced=false", external)
	}
}

func TestBuildAgentCapabilityLimits_Empty(t *testing.T) {
	got := buildAgentCapabilityLimits(nil, nil)
	if got.HasMcpConfig {
		t.Fatalf("expected HasMcpConfig=false for empty config")
	}
	if got.Sandbox != nil {
		t.Fatalf("expected nil sandbox for empty runtime_config, got %s", got.Sandbox)
	}
	if len(got.McpServers) != 0 {
		t.Fatalf("expected no mcp servers, got %v", got.McpServers)
	}
}

func TestBuildAgentCapabilityLimits_SandboxAndMcp(t *testing.T) {
	runtimeConfig := []byte(`{"sandbox":{"network_allowlist":["api.anthropic.com:443"]},"other":1}`)
	mcpConfig := []byte(`{"mcpServers":{"multica":{"command":"x"},"bigquery":{"command":"y"}}}`)

	got := buildAgentCapabilityLimits(runtimeConfig, mcpConfig)

	if !got.HasMcpConfig {
		t.Fatalf("expected HasMcpConfig=true")
	}
	if len(got.Sandbox) == 0 {
		t.Fatalf("expected sandbox raw json to be populated")
	}
	var sb map[string]any
	if err := json.Unmarshal(got.Sandbox, &sb); err != nil {
		t.Fatalf("sandbox is not valid json: %v", err)
	}
	if _, ok := sb["network_allowlist"]; !ok {
		t.Fatalf("expected network_allowlist key in sandbox, got %v", sb)
	}

	sort.Strings(got.McpServers)
	want := []string{"bigquery", "multica"}
	if len(got.McpServers) != len(want) || got.McpServers[0] != want[0] || got.McpServers[1] != want[1] {
		t.Fatalf("expected mcp servers %v, got %v", want, got.McpServers)
	}
}

func TestBuildAgentCapabilityLimits_MalformedIsSafe(t *testing.T) {
	// A non-JSON blob must not panic and must yield an empty section.
	got := buildAgentCapabilityLimits([]byte("not json"), []byte("also not json"))
	if got.Sandbox != nil {
		t.Fatalf("expected nil sandbox for malformed runtime_config")
	}
	// mcp_config is present (non-empty) so HasMcpConfig is true even if unparseable.
	if !got.HasMcpConfig {
		t.Fatalf("expected HasMcpConfig=true when mcp_config bytes present")
	}
	if len(got.McpServers) != 0 {
		t.Fatalf("expected no mcp servers from malformed config, got %v", got.McpServers)
	}
}

// CEREBRO-PATCH(agent-capabilities-secret-set-test): TECH-3738 Bid A — the
// agent custom_env secret set is names-only and redacts for non-privileged
// callers; runtime secret status distinguishes "nothing" from "unknown".

func TestBuildAgentSecretSet_RevealVsRedacted(t *testing.T) {
	agent := db.Agent{CustomEnv: []byte(`{"OPENAI_API_KEY":"x","SLIPLANE_KEY":"y"}`)}

	// Owner/admin caller: names revealed, sorted, not redacted.
	revealed := buildAgentSecretSet(agent, true)
	if revealed.Status != capStatusKnown {
		t.Fatalf("expected status=known, got %q", revealed.Status)
	}
	if revealed.Count != 2 || revealed.Redacted {
		t.Fatalf("expected count=2 not redacted, got count=%d redacted=%v", revealed.Count, revealed.Redacted)
	}
	if len(revealed.Names) != 2 || revealed.Names[0] != "OPENAI_API_KEY" || revealed.Names[1] != "SLIPLANE_KEY" {
		t.Fatalf("expected sorted names, got %v", revealed.Names)
	}

	// Non-privileged caller: count preserved, names withheld, redacted=true.
	redacted := buildAgentSecretSet(agent, false)
	if redacted.Count != 2 || !redacted.Redacted {
		t.Fatalf("expected count=2 redacted=true, got count=%d redacted=%v", redacted.Count, redacted.Redacted)
	}
	if len(redacted.Names) != 0 {
		t.Fatalf("expected no names leaked to non-privileged caller, got %v", redacted.Names)
	}
}

func TestBuildAgentSecretSet_EmptyIsNotConfigured(t *testing.T) {
	got := buildAgentSecretSet(db.Agent{CustomEnv: []byte(`{}`)}, true)
	if got.Status != capStatusNotConfigured {
		t.Fatalf("expected status=not_configured for empty custom_env, got %q", got.Status)
	}
	if got.Count != 0 || got.Names == nil {
		t.Fatalf("expected count=0 and non-nil names slice, got count=%d names=%v", got.Count, got.Names)
	}
}

func TestRuntimeSecretSetFromCaps_Status(t *testing.T) {
	// Known: bindings present, normal discovery method.
	known := runtimeSecretSetFromCaps(map[string]any{
		"secret_bindings":  []string{"ANTHROPIC_API_KEY"},
		"discovery_method": "static",
	}, "rt-1", true)
	if known.Status != capStatusKnown || known.Count != 1 || len(known.Names) != 1 {
		t.Fatalf("expected known/1/[name], got %+v", known)
	}
	if known.RuntimeID != "rt-1" {
		t.Fatalf("expected runtime_id propagated, got %q", known.RuntimeID)
	}

	// Not configured: empty bindings but the runtime WAS mapped.
	none := runtimeSecretSetFromCaps(map[string]any{
		"secret_bindings":  []string{},
		"discovery_method": "static",
	}, "rt-2", true)
	if none.Status != capStatusNotConfigured {
		t.Fatalf("expected not_configured for mapped-but-empty, got %q", none.Status)
	}

	// Unknown: an unmapped runtime — absence of bindings is NOT confirmed-empty.
	unknown := runtimeSecretSetFromCaps(map[string]any{
		"discovery_method": "unmapped",
	}, "rt-3", true)
	if unknown.Status != capStatusUnknown {
		t.Fatalf("expected unknown for unmapped runtime, got %q", unknown.Status)
	}

	// Redaction applies to runtime bindings too.
	redacted := runtimeSecretSetFromCaps(map[string]any{
		"secret_bindings":  []string{"ANTHROPIC_API_KEY"},
		"discovery_method": "static",
	}, "rt-4", false)
	if !redacted.Redacted || len(redacted.Names) != 0 || redacted.Count != 1 {
		t.Fatalf("expected redacted with count=1 and no names, got %+v", redacted)
	}
}

// CEREBRO-PATCH(agent-capabilities-observed-test): TECH-3738 Bid B unit tests
// for the observed-access drift logic — observed tool usage compared against the
// declared policy, with blocked/unmapped use flagged as drift.

func TestObservedToolStatus(t *testing.T) {
	cases := []struct {
		perm       string
		hasRow     bool
		wantStatus string
		wantDrift  bool
	}{
		{"allow", true, observedStatusAllowed, false},
		{"ask", true, observedStatusNeedsApproval, false},
		{"deny", true, observedStatusBlocked, true},
		{"", false, observedStatusUnmapped, true},     // no policy row → drift
		{"weird", true, observedStatusUnmapped, true}, // enum drift → treated as unmapped
	}
	for _, c := range cases {
		gotStatus, gotDrift := observedToolStatus(c.perm, c.hasRow)
		if gotStatus != c.wantStatus || gotDrift != c.wantDrift {
			t.Fatalf("observedToolStatus(%q,%v) = (%q,%v); want (%q,%v)",
				c.perm, c.hasRow, gotStatus, gotDrift, c.wantStatus, c.wantDrift)
		}
	}
}

func TestPermissionLookupFromRows_MatchesTitleAndKey(t *testing.T) {
	rows := []cerebrotoolpolicy.TableRow{
		{ToolKey: "bash", Title: "Bash", Effective: cerebrotoolpolicy.Effective{Setting: cerebrotoolpolicy.SettingAllow}},
		{ToolKey: "bigquery.query", Title: "", Effective: cerebrotoolpolicy.Effective{Setting: cerebrotoolpolicy.SettingDeny}},
		{ToolKey: "", Title: "", Effective: cerebrotoolpolicy.Effective{Setting: cerebrotoolpolicy.SettingAsk}}, // skipped: no key/title
	}
	perm := permissionLookupFromRows(rows)
	// task_message.tool stores "Bash"; the catalog key is "bash" — case-folded match.
	if perm["bash"] != "allow" {
		t.Fatalf("expected bash→allow, got %q", perm["bash"])
	}
	if perm["bigquery.query"] != "deny" {
		t.Fatalf("expected bigquery.query→deny, got %q", perm["bigquery.query"])
	}
}

func TestObservedAccessFromUsage_KnownWithDrift(t *testing.T) {
	usage := []cerebrodb.ListAgentObservedToolUsageRow{
		{Tool: "Bash", Uses: 12, LastUsed: pgtype.Timestamptz{Time: time.Unix(1_700_000_000, 0), Valid: true}},
		{Tool: "Read", Uses: 5},
		{Tool: "WebFetch", Uses: 2},  // no policy row → unmapped drift
		{Tool: "DropTable", Uses: 1}, // denied → blocked drift
	}
	perm := map[string]string{"bash": "allow", "read": "allow", "droptable": "deny"}

	got := observedAccessFromUsage(usage, 7, perm, observedAccessWindowDays)

	if got.Status != capStatusKnown {
		t.Fatalf("expected status known, got %q", got.Status)
	}
	if got.TaskCount != 7 {
		t.Fatalf("expected task_count 7, got %d", got.TaskCount)
	}
	if got.DriftCount != 2 {
		t.Fatalf("expected 2 drift tools (WebFetch unmapped + DropTable blocked), got %d", got.DriftCount)
	}
	if len(got.Tools) != 4 {
		t.Fatalf("expected 4 observed tools, got %d", len(got.Tools))
	}
	// Bash: allowed, no drift, last_used populated as RFC3339.
	if got.Tools[0].Name != "Bash" || got.Tools[0].Status != observedStatusAllowed || got.Tools[0].Drift {
		t.Fatalf("unexpected Bash entry: %+v", got.Tools[0])
	}
	if got.Tools[0].LastUsed == "" {
		t.Fatalf("expected Bash last_used to be populated")
	}
}

func TestObservedAccessFromUsage_TaskMandateDenialIsDrift(t *testing.T) {
	usage := []cerebrodb.ListAgentObservedToolUsageRow{
		{
			Tool:           "mcp__customer_service__handle_message",
			Uses:           1,
			MandateDenials: 1,
			LastUsed:       pgtype.Timestamptz{Time: time.Unix(1_700_000_000, 0), Valid: true},
		},
	}
	perm := map[string]string{"mcp__customer_service__handle_message": "allow"}

	got := observedAccessFromUsage(usage, 1, perm, observedAccessWindowDays)

	if got.DriftCount != 1 || len(got.Tools) != 1 {
		t.Fatalf("task mandate denial must be one visible drift: %+v", got)
	}
	tool := got.Tools[0]
	if !tool.Drift || tool.Status != observedStatusBlocked || tool.MandateDenials != 1 {
		t.Fatalf("allowed tool denied by task mandate must be blocked drift: %+v", tool)
	}
}

func TestObservedAccessFromUsage_NoRunsIsNotConfigured(t *testing.T) {
	got := observedAccessFromUsage(nil, 0, map[string]string{}, observedAccessWindowDays)
	if got.Status != capStatusNotConfigured {
		t.Fatalf("expected not_configured when the agent ran nothing, got %q", got.Status)
	}
	if got.DriftCount != 0 || len(got.Tools) != 0 {
		t.Fatalf("expected empty observed access, got %+v", got)
	}
	if got.WindowDays != observedAccessWindowDays {
		t.Fatalf("expected window_days %d, got %d", observedAccessWindowDays, got.WindowDays)
	}
}

// CEREBRO-PATCH(agent-capabilities-runtime-options-test): FIR-3212 slice 6 —
// the runtime_options section that lets the Setup screen drive its fields from
// what the agent's engine actually honours.

func TestRuntimeExecOptionsFromProvider_KnownProvider(t *testing.T) {
	got := runtimeExecOptionsFromProvider("claude", "2.1.209", "rt-1")
	if got.Status != capStatusKnown {
		t.Fatalf("status = %q, want %q", got.Status, capStatusKnown)
	}
	if got.Provider != "claude" || got.CliVersion != "2.1.209" || got.RuntimeID != "rt-1" {
		t.Fatalf("identity fields wrong: %+v", got)
	}
	if len(got.ExecOptions) == 0 {
		t.Fatal("claude must expose a non-empty exec-options matrix")
	}
	if got.SystemPrompt == nil {
		t.Fatal("claude must report system-prompt support")
	}
	if !got.SystemPrompt.Native {
		t.Fatal("claude has a native system-prompt channel")
	}
	modes := map[string]bool{}
	for _, m := range got.SystemPrompt.Modes {
		modes[m] = true
	}
	if !modes["append"] || !modes["replace"] {
		t.Fatalf("claude modes = %v, want append+replace", got.SystemPrompt.Modes)
	}
}

func TestRuntimeExecOptionsFromProvider_UnknownProviderIsUnknownNotEmpty(t *testing.T) {
	got := runtimeExecOptionsFromProvider("some-future-engine", "", "rt-2")
	if got.Status != capStatusUnknown {
		t.Fatalf("status = %q, want %q — unknown must never read as 'supports nothing'", got.Status, capStatusUnknown)
	}
	if got.ExecOptions == nil || got.SilentlyIgnored == nil {
		t.Fatal("slices must be non-nil so JSON renders [] instead of null")
	}
	if got.SystemPrompt != nil {
		t.Fatal("no system-prompt claim may be made for an unknown provider")
	}
}

func TestRuntimeExecOptionsFromProvider_PrependOnlyProviderIsNotNative(t *testing.T) {
	got := runtimeExecOptionsFromProvider("opencode", "", "rt-3")
	if got.SystemPrompt == nil {
		t.Fatal("opencode must report system-prompt support")
	}
	if got.SystemPrompt.Native {
		t.Fatal("opencode splices into the user message — it must not claim native system-prompt semantics")
	}
}

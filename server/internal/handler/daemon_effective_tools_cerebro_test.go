package handler

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/cerebro/claudehook"
	"github.com/multica-ai/multica/server/internal/cerebro/localtoolpolicy"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type fakeRuntimeToolAccess struct {
	rows []RuntimeToolEffectiveAccessView
	err  error
}

func (f fakeRuntimeToolAccess) ListEffectiveTools(_ context.Context, _ RuntimeToolAccessQuery) ([]RuntimeToolEffectiveAccessView, error) {
	return f.rows, f.err
}

func exposedToolView(toolKey, source, mcpServer, desc, verdict string, exposed bool) RuntimeToolEffectiveAccessView {
	return RuntimeToolEffectiveAccessView{
		Descriptor:        RuntimeToolDescriptorView{ToolKey: toolKey, DisplayName: toolKey, Description: desc, Source: source},
		Inventory:         RuntimeToolInventoryStateView{ToolName: toolKey, Source: source, MCPServerName: mcpServer},
		Policy:            RuntimeToolPolicyStateView{Effective: verdict},
		ExposureEffective: RuntimeToolExposureEffectiveView{Effective: exposed},
	}
}

func TestCerebroEffectiveToolsForBriefMapsAndFilters(t *testing.T) {
	agentID := "11111111-1111-1111-1111-111111111111"
	h := &Handler{runtimeToolAccess: fakeRuntimeToolAccess{rows: []RuntimeToolEffectiveAccessView{
		exposedToolView("schedule_wakeup", "platform", "", "Schedule a wakeup", "allow", true),
		exposedToolView("lookup_order", "mcp", "customer-service", "Look up an order", "allow", true),
		exposedToolView("draft_reply", "mcp", "customer-service", "Draft a reply", "ask", true),
		exposedToolView("search_knowledge", "mcp", "", "Search the KB", "allow", true),
		exposedToolView("secret_tool", "mcp", "vault", "secret", "deny", false), // not exposed → dropped
	}}}

	got := h.cerebroEffectiveToolsForBrief(context.Background(), db.AgentRuntime{}, &TaskAgentData{ID: agentID}, "agent", "")
	if len(got) != 4 {
		t.Fatalf("expected 4 exposed tools, got %d: %+v", len(got), got)
	}

	byName := map[string]AgentTaskToolEntry{}
	for _, e := range got {
		byName[e.Name] = e
	}
	if e, ok := byName["customer-service / lookup_order"]; !ok || e.Family != "Connections" {
		t.Errorf("expected connection tool grouped under Connections with server prefix, got %+v", e)
	}
	if e, ok := byName["search_knowledge"]; !ok || e.Family != "MCP tools" {
		t.Errorf("expected mcp tool with no server under MCP tools, got %+v", e)
	}
	if e, ok := byName["schedule_wakeup"]; !ok || e.Family != "Platform tools" {
		t.Errorf("expected platform tool under Platform tools, got %+v", e)
	}
	if _, ok := byName["vault / secret_tool"]; ok {
		t.Errorf("did not expect a non-exposed tool in the result")
	}
	if e := byName["customer-service / draft_reply"]; e.Verdict != "ask" {
		t.Errorf("expected ask verdict carried through, got %q", e.Verdict)
	}
}

func TestRuntimeToolDecisionParityAcrossInventoryCapabilitiesMandateAndCall(t *testing.T) {
	row := exposedToolView("customer-service.lookup_order", "mcp", "customer-service", "Look up order", "ask", true)
	h := &Handler{runtimeToolAccess: fakeRuntimeToolAccess{rows: []RuntimeToolEffectiveAccessView{row}}}
	tools, mandate, err := h.cerebroEffectiveToolsForClaim(context.Background(), db.AgentRuntime{}, &TaskAgentData{ID: "11111111-1111-1111-1111-111111111111"}, "agent", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Verdict != "ask" {
		t.Fatalf("inventory tools = %+v, want one Ask tool", tools)
	}
	if len(mandate) != 1 || mandate[0] != "mcp__customer-service__lookup_order" {
		t.Fatalf("mandate = %v, want canonical callable MCP name", mandate)
	}

	// The Capabilities card is a projection of this same effective row.
	capability := AgentCapabilityTool{
		Key:        row.Descriptor.ToolKey,
		Source:     row.Descriptor.Source,
		Permission: row.Policy.Effective,
	}
	if capability.Permission != tools[0].Verdict {
		t.Fatalf("Capabilities permission = %q, inventory verdict = %q", capability.Permission, tools[0].Verdict)
	}

	// The local hook's call-time lookup must hit the exact key in the issued
	// mandate; otherwise inventory, UI and execution would disagree.
	if callKey := claudehook.PolicyToolKey("mcp__customer-service__lookup_order"); callKey != mandate[0] {
		t.Fatalf("call key = %q, mandate key = %q", callKey, mandate[0])
	}
}

func TestCerebroEffectiveToolsForClaimLocksInitialPolicyAndSessionIntersection(t *testing.T) {
	rows := []RuntimeToolEffectiveAccessView{
		exposedToolView("Read", "runtime", "", "Read files", "allow", true),
		exposedToolView("Bash", "runtime", "", "Run commands", "deny", false),
		exposedToolView("Write", "runtime", "", "Write files", "allow", true),
	}
	h := &Handler{runtimeToolAccess: fakeRuntimeToolAccess{rows: rows}}
	agent := &TaskAgentData{ID: "11111111-1111-1111-1111-111111111111"}

	tools, mandate, err := h.cerebroEffectiveToolsForClaim(context.Background(), db.AgentRuntime{}, agent, "agent", "")
	if err != nil {
		t.Fatalf("cerebroEffectiveToolsForClaim: unexpected error %v", err)
	}
	tools, mandate = filterClaimToolsForSessionMode(tools, mandate, []string{"Read", "Bash"})
	if len(mandate) != 1 || mandate[0] != "Read" {
		t.Fatalf("initial task mandate = %v, want exact policy/runtime/session intersection [Read]", mandate)
	}

	// Opening the later policy changes a fresh resolution, but cannot expand the
	// already-issued per-run snapshot. Bash therefore remains outside this run.
	rows[1].Policy.Effective = "allow"
	rows[1].ExposureEffective.Effective = true
	_, freshlyResolved, freshErr := h.cerebroEffectiveToolsForClaim(context.Background(), db.AgentRuntime{}, agent, "agent", "")
	if freshErr != nil {
		t.Fatalf("cerebroEffectiveToolsForClaim (fresh): unexpected error %v", freshErr)
	}
	if !containsString(freshlyResolved, "Bash") {
		t.Fatalf("fresh policy resolution = %v, want Bash after opening the rule", freshlyResolved)
	}
	if containsString(mandate, "Bash") {
		t.Fatalf("issued task mandate expanded after policy change: %v", mandate)
	}
}

func TestCerebroEffectiveToolsForClaimSessionIntersectionKeepsDirectMCPMandateAligned(t *testing.T) {
	brief := &fakeAPIConnBrief{tools: []CerebroAPIConnectionBriefTool{
		{
			Connection:    "atlas-mcp",
			Name:          "mcp__atlas-mcp__search_registry",
			Description:   "Search Atlas",
			Verdict:       "allow",
			MandatePrefix: "mcp__atlas-mcp__*",
		},
	}}
	h := &Handler{
		runtimeToolAccess: fakeRuntimeToolAccess{rows: []RuntimeToolEffectiveAccessView{
			exposedToolView("Read", "runtime", "", "Read files", "allow", true),
			exposedToolView("Write", "runtime", "", "Write files", "allow", true),
		}},
		APIConnectionBrief: brief,
	}
	agent := &TaskAgentData{ID: "11111111-1111-1111-1111-111111111111"}

	tools, mandate, err := h.cerebroEffectiveToolsForClaim(context.Background(), db.AgentRuntime{}, agent, "agent", "")
	if err != nil {
		t.Fatalf("cerebroEffectiveToolsForClaim: unexpected error %v", err)
	}
	tools, mandate = filterClaimToolsForSessionMode(tools, mandate, []string{"Read", "mcp__atlas-mcp__search_registry"})
	if got, want := len(tools), 2; got != want {
		t.Fatalf("session tools = %+v, want %d tools", tools, want)
	}
	for _, want := range []string{"Read", "mcp__atlas-mcp__search_registry", "mcp__atlas-mcp__*"} {
		if !containsString(mandate, want) {
			t.Fatalf("session mandate = %v, want %q", mandate, want)
		}
	}
	if containsString(mandate, "Write") {
		t.Fatalf("session mandate widened past the allowed tools: %v", mandate)
	}

	freshTools, freshMandate, err := h.cerebroEffectiveToolsForClaim(context.Background(), db.AgentRuntime{}, agent, "agent", "")
	if err != nil {
		t.Fatalf("cerebroEffectiveToolsForClaim (fresh): unexpected error %v", err)
	}
	_, mandate = filterClaimToolsForSessionMode(freshTools, freshMandate, []string{"Read"})
	if containsString(mandate, "mcp__atlas-mcp__*") {
		t.Fatalf("server wildcard survived after its connection tool was filtered out: %v", mandate)
	}
}

type fakeAPIConnBrief struct {
	tools    []CerebroAPIConnectionBriefTool
	gotIdent CerebroAPIConnectionBriefIdentity
}

func (f *fakeAPIConnBrief) APIConnectionToolsForBrief(_ context.Context, ident CerebroAPIConnectionBriefIdentity) []CerebroAPIConnectionBriefTool {
	f.gotIdent = ident
	return f.tools
}

// FIR-2388: api-type connection endpoint tools resolved by the shared resolver
// are appended to the brief under "Connections", carrying their Allow/Ask
// verdict, alongside the tool-policy chain's own tools.
func TestCerebroEffectiveToolsForBriefIncludesAPIConnectionTools(t *testing.T) {
	agentID := "11111111-1111-1111-1111-111111111111"
	brief := &fakeAPIConnBrief{tools: []CerebroAPIConnectionBriefTool{
		{Name: "infisical_admin__get_api_v3_secrets_raw", Description: "Read a secret", Verdict: "allow"},
		{Name: "infisical_admin__post_api_v3_secrets", Description: "Write a secret", Verdict: "ask"},
	}}
	h := &Handler{
		runtimeToolAccess: fakeRuntimeToolAccess{rows: []RuntimeToolEffectiveAccessView{
			exposedToolView("schedule_wakeup", "platform", "", "Schedule a wakeup", "allow", true),
		}},
		APIConnectionBrief: brief,
	}

	got := h.cerebroEffectiveToolsForBrief(context.Background(), db.AgentRuntime{}, &TaskAgentData{ID: agentID}, "agent", "")
	byName := map[string]AgentTaskToolEntry{}
	for _, e := range got {
		byName[e.Name] = e
	}

	e, ok := byName["infisical_admin__get_api_v3_secrets_raw"]
	if !ok || e.Family != "Connections" || e.Verdict != "allow" {
		t.Fatalf("expected api-connection tool under Connections with allow verdict, got %+v (ok=%v)", e, ok)
	}
	if e := byName["infisical_admin__post_api_v3_secrets"]; e.Verdict != "ask" {
		t.Errorf("expected ask verdict carried through, got %q", e.Verdict)
	}
	if _, ok := byName["schedule_wakeup"]; !ok {
		t.Errorf("tool-policy chain tools must remain alongside api-connection tools")
	}
}

func TestCerebroEffectiveToolsForClaimUsesConnectionDispatchMandateIdentity(t *testing.T) {
	agentID := "11111111-1111-1111-1111-111111111111"
	brief := &fakeAPIConnBrief{tools: []CerebroAPIConnectionBriefTool{
		{Name: "infisical_admin__get_api_v3_secrets_raw", Description: "Read a secret", Verdict: "allow"},
	}}
	h := &Handler{
		runtimeToolAccess:  fakeRuntimeToolAccess{},
		APIConnectionBrief: brief,
	}

	_, mandate, err := h.cerebroEffectiveToolsForClaim(
		context.Background(),
		db.AgentRuntime{},
		&TaskAgentData{ID: agentID},
		"agent",
		"",
	)
	if err != nil {
		t.Fatalf("cerebroEffectiveToolsForClaim: %v", err)
	}
	if !containsString(mandate, "infisical_admin__get_api_v3_secrets_raw") {
		t.Fatalf("claim mandate = %v, want connection dispatch identity", mandate)
	}

	hookTool := "mcp__multica__infisical_admin__get_api_v3_secrets_raw"
	if got := localtoolpolicy.ProviderMandateToolKey(hookTool); got != mandate[0] {
		t.Fatalf("local hook mandate identity = %q, want claimed identity %q", got, mandate[0])
	}
}

func TestCerebroEffectiveToolsForClaimIncludesDirectMCPAndCapabilityDiagnosis(t *testing.T) {
	agentID := "11111111-1111-1111-1111-111111111111"
	brief := &fakeAPIConnBrief{tools: []CerebroAPIConnectionBriefTool{
		{
			Connection:    "atlas-mcp",
			Name:          "mcp__atlas-mcp__search_registry",
			Description:   "Search Atlas",
			Verdict:       "allow",
			MandatePrefix: "mcp__atlas-mcp__*",
		},
	}}
	h := &Handler{
		runtimeToolAccess: fakeRuntimeToolAccess{rows: []RuntimeToolEffectiveAccessView{
			exposedToolView("mcp__multica__get_agent_capabilities", "mcp", "", "Inspect effective access", "allow", true),
		}},
		APIConnectionBrief: brief,
	}

	_, mandate, err := h.cerebroEffectiveToolsForClaim(
		context.Background(),
		db.AgentRuntime{},
		&TaskAgentData{ID: agentID},
		"agent",
		"",
	)
	if err != nil {
		t.Fatalf("cerebroEffectiveToolsForClaim: %v", err)
	}
	for _, want := range []string{
		"mcp__atlas-mcp__search_registry",
		"mcp__atlas-mcp__*",
		"mcp__multica__get_agent_capabilities",
	} {
		if !containsString(mandate, want) {
			t.Errorf("claim mandate = %v, want exact callable identity %q", mandate, want)
		}
	}
}

// When the tool-policy chain exposes nothing but the agent HAS api-connection
// tools, the brief still lists them (not nil) — the whole point of FIR-2388.
func TestCerebroEffectiveToolsForBriefAPIConnectionOnly(t *testing.T) {
	brief := &fakeAPIConnBrief{tools: []CerebroAPIConnectionBriefTool{
		{Name: "infisical_admin__get_api_v3_secrets_raw", Description: "Read a secret", Verdict: "allow"},
	}}
	h := &Handler{
		runtimeToolAccess:  fakeRuntimeToolAccess{},
		APIConnectionBrief: brief,
	}
	got := h.cerebroEffectiveToolsForBrief(context.Background(), db.AgentRuntime{}, &TaskAgentData{ID: "11111111-1111-1111-1111-111111111111"}, "agent", "")
	if len(got) != 1 || got[0].Name != "infisical_admin__get_api_v3_secrets_raw" || got[0].Family != "Connections" {
		t.Fatalf("expected the api-connection tool listed even with an empty chain, got %+v", got)
	}
}

func TestCerebroEffectiveToolsForBriefNilService(t *testing.T) {
	h := &Handler{}
	if got := h.cerebroEffectiveToolsForBrief(context.Background(), db.AgentRuntime{}, &TaskAgentData{ID: "x"}, "agent", ""); got != nil {
		t.Fatalf("expected nil when service unset, got %+v", got)
	}
}

func TestCerebroEffectiveToolsForBriefBadAgentID(t *testing.T) {
	h := &Handler{runtimeToolAccess: fakeRuntimeToolAccess{}}
	if got := h.cerebroEffectiveToolsForBrief(context.Background(), db.AgentRuntime{}, &TaskAgentData{ID: "not-a-uuid"}, "agent", ""); got != nil {
		t.Fatalf("expected nil for invalid agent id, got %+v", got)
	}
	// sanity: util.ParseUUID rejects the bad id
	if _, err := util.ParseUUID("not-a-uuid"); err == nil {
		t.Fatalf("expected ParseUUID to reject bad id")
	}
}

type fakeConnectionInstructions struct{ values map[string]string }

func (f fakeConnectionInstructions) ConnectionInstructionsForBrief(_ context.Context, _ pgtype.UUID, names []string) map[string]string {
	got := map[string]string{}
	for _, name := range names {
		if v := f.values[name]; v != "" {
			got[name] = v
		}
	}
	return got
}

func TestCerebroEffectiveToolsForBriefAddsInstructionsOnlyForExposedConnections(t *testing.T) {
	agentID := "11111111-1111-1111-1111-111111111111"
	h := &Handler{
		runtimeToolAccess:           fakeRuntimeToolAccess{rows: []RuntimeToolEffectiveAccessView{exposedToolView("search", "mcp", "company-brain", "Search", "allow", true)}},
		ConnectionInstructionsBrief: fakeConnectionInstructions{values: map[string]string{"company-brain": "Search before answering.", "hidden": "Never include me."}},
	}
	got := h.cerebroEffectiveToolsForBrief(context.Background(), db.AgentRuntime{}, &TaskAgentData{ID: agentID}, "agent", "")
	if len(got) != 1 || got[0].Instructions != "Search before answering." || got[0].Connection != "company-brain" {
		t.Fatalf("unexpected tools: %#v", got)
	}
}

// TestCerebroEffectiveToolsForClaimDistinguishesResolveErrorFromEmpty is the
// FIR-3403 TRIN 1b regression guard. The task mandate is a fail-closed allowlist:
// an empty mandate denies EVERY tool (including built-in Bash/Read/Edit), so
// "could not resolve" must never collapse into the same nil result as "resolved
// to zero tools". A resolver error must surface as an error (the claim path fails
// the claim), while a genuine zero-tool resolution must stay a non-error empty
// result (an empty mandate may legitimately be issued).
func TestCerebroEffectiveToolsForClaimDistinguishesResolveErrorFromEmpty(t *testing.T) {
	agentID := "11111111-1111-1111-1111-111111111111"

	// 1) Resolver present but ListEffectiveTools fails → error, no silent lockout.
	hErr := &Handler{runtimeToolAccess: fakeRuntimeToolAccess{err: errors.New("db unreachable")}}
	tools, mandate, err := hErr.cerebroEffectiveToolsForClaim(context.Background(), db.AgentRuntime{}, &TaskAgentData{ID: agentID}, "agent", "")
	if err == nil {
		t.Fatalf("resolve error must return a non-nil error, got tools=%v mandate=%v", tools, mandate)
	}
	if tools != nil || mandate != nil {
		t.Fatalf("resolve error must return nil tools/mandate, got tools=%v mandate=%v", tools, mandate)
	}

	// 2) Malformed agent id (resolver would be consulted) → error.
	if _, _, badErr := hErr.cerebroEffectiveToolsForClaim(context.Background(), db.AgentRuntime{}, &TaskAgentData{ID: "not-a-uuid"}, "agent", ""); badErr == nil {
		t.Fatalf("a malformed agent id must return an error, not a silent empty mandate")
	}

	// 3) Resolver returns zero rows (no error) → legitimate empty result, no error.
	hEmpty := &Handler{runtimeToolAccess: fakeRuntimeToolAccess{rows: nil}}
	_, emptyMandate, emptyErr := hEmpty.cerebroEffectiveToolsForClaim(context.Background(), db.AgentRuntime{}, &TaskAgentData{ID: agentID}, "agent", "")
	if emptyErr != nil {
		t.Fatalf("a genuine zero-tool resolution must not be an error, got %v", emptyErr)
	}
	if len(emptyMandate) != 0 {
		t.Fatalf("zero-tool resolution must yield an empty mandate, got %v", emptyMandate)
	}

	// 4) Resolver not wired (feature unavailable) → non-error, unchanged behaviour.
	hNil := &Handler{}
	if _, _, nilErr := hNil.cerebroEffectiveToolsForClaim(context.Background(), db.AgentRuntime{}, &TaskAgentData{ID: agentID}, "agent", ""); nilErr != nil {
		t.Fatalf("an unwired resolver must not fail the claim, got %v", nilErr)
	}
}

package handler

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
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

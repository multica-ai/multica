package handler

import (
	"context"
	"testing"

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

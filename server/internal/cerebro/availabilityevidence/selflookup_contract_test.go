package availabilityevidence_test

// The self-lookup coverage contract (FIR-3398, Trin 2).
//
// This file exists because of a real gap. `get_agent_capabilities` was marked
// callable in runtime.multicaMCPToolMatrix, so SeedKristianTools granted it in
// agent_tool_grant — but the Firtal Gateway registry never registered an
// implementation, and Registry.GetEnabledToolsForAgent silently drops a granted
// tool it cannot find. The grant said yes; the runtime had nothing to call.
// An agent on the Gateway asking "what am I allowed to do?" got no answer, and
// no test noticed, because every check read the DECLARED matrix rather than the
// runtime's REAL surface.
//
// These tests read the live surfaces instead: the Gateway's actual registry, and
// the actual MCP tool server that local runtimes and Claude Code consume. If the
// self-lookup ever goes missing on any runtime family again, this fails.
//
// Nothing here asserts on config. That is the whole point.

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/availabilityevidence"
	"github.com/multica-ai/multica/server/internal/cerebro/clitools"
	cerebroruntime "github.com/multica-ai/multica/server/internal/cerebro/runtime"
	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/mcp"
)

// selfLookupCapability is the canonical capability ID minted by Trin 1's
// capabilitycatalog.PlatformTool("get_agent_capabilities"). Evidence is keyed to
// the canonical name so the two steps cannot drift apart.
const selfLookupCapability = "platform:get_agent_capabilities"

const selfLookupTool = "get_agent_capabilities"

// fakeCapabilitiesProvider stands in for *handler.Handler, which needs a live
// database. Registration is what is under test here, not card contents.
type fakeCapabilitiesProvider struct{}

func (fakeCapabilitiesProvider) BuildAgentCapabilitiesCard(context.Context, pgtype.UUID) (handler.AgentCapabilities, error) {
	return handler.AgentCapabilities{AgentID: "fake"}, nil
}

// gatewaySurface reads the Firtal Gateway's real in-process tool registry — the
// same registry GetEnabledToolsForAgent resolves a grant against.
func gatewaySurface(t *testing.T) []string {
	t.Helper()
	tctx := cerebroruntime.ToolContext{
		AgentID:              pgtype.UUID{Valid: true},
		WorkspaceID:          pgtype.UUID{Valid: true},
		CapabilitiesProvider: fakeCapabilitiesProvider{},
	}
	return cerebroruntime.NewDefaultRegistry(nil, nil, tctx).Names()
}

// mcpSurface reads the real Multica MCP tool server that `multica mcp serve`
// exposes. Both Claude Code and every other local runtime consume this exact
// server, so it is the live surface for both families.
func mcpSurface(t *testing.T) []string {
	t.Helper()
	srv := mcp.NewServer("multica", "test")
	// A client pointed at a closed port: registration of connection-backed tools
	// is best-effort and degrades to "no tools" on error. The built-in tool set —
	// which is what this contract is about — registers unconditionally.
	clitools.RegisterTools(srv, cli.NewAPIClient("http://127.0.0.1:1", "", ""), &clitools.SessionState{}, "", "", "")
	tools := srv.Tools()
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	return names
}

// surfaceFor returns the live capability surface for one runtime family, mapped
// onto canonical capability IDs.
func surfaceFor(t *testing.T, rt availabilityevidence.RuntimeType) *availabilityevidence.StaticSurface {
	t.Helper()
	var names []string
	switch rt {
	case availabilityevidence.RuntimeFirtalGateway:
		names = gatewaySurface(t)
	case availabilityevidence.RuntimeClaudeCode, availabilityevidence.RuntimeLocal:
		names = mcpSurface(t)
	default:
		t.Fatalf("no live surface wired for runtime %q — a new runtime family must be probed, not assumed", rt)
	}

	ids := make([]string, 0, len(names))
	for _, name := range names {
		ids = append(ids, "platform:"+name)
	}
	return availabilityevidence.NewStaticSurface(rt, ids)
}

// selfLookupGate models the ceiling both surfaces enforce: an agent may read its
// own card, and only its own. The Gateway tool rejects a foreign agent_id; the
// HTTP route's task scope (AllowTaskScopeForAgent) does the same.
type selfLookupGate struct{}

func (selfLookupGate) Decide(_ context.Context, _ string, s availabilityevidence.Subject) (availabilityevidence.Outcome, error) {
	if s.Authorized {
		return availabilityevidence.OutcomeAllowed, nil
	}
	return availabilityevidence.OutcomeDenied, nil
}

// This is the regression guard for the Kristian case. If get_agent_capabilities
// is dropped from any runtime family's real surface, this test fails and names
// the runtime.
func TestSelfLookupIsRealOnEveryRuntime(t *testing.T) {
	var ledger availabilityevidence.Ledger

	for _, rt := range availabilityevidence.RuntimeTypes() {
		prober := availabilityevidence.Prober{Surface: surfaceFor(t, rt), Gate: selfLookupGate{}}
		if err := prober.Probe(context.Background(), &ledger, []string{selfLookupCapability},
			availabilityevidence.Subject{Name: "self", Authorized: true},
			availabilityevidence.Subject{Name: "other-agent"},
		); err != nil {
			t.Fatalf("probing %s: %v", rt, err)
		}
	}

	for _, rt := range availabilityevidence.RuntimeTypes() {
		got := ledger.Lookup(selfLookupCapability, rt)
		if !got.Level.IsReality() {
			t.Errorf("the agent self-lookup is not real on runtime %q: level=%s, reason=%s\n"+
				"An agent on this runtime cannot find out what it is allowed to do.",
				rt, got.Level, got.Reason)
		}
	}
}

// The Gateway is the runtime that regressed. Assert its real registry directly,
// so the failure names the cause rather than the symptom.
func TestGatewayRegistersSelfLookup(t *testing.T) {
	for _, name := range gatewaySurface(t) {
		if name == selfLookupTool {
			return
		}
	}
	t.Fatalf("%q is missing from the Firtal Gateway tool registry. It is marked callable in "+
		"the tool matrix, so it is granted in agent_tool_grant — but a granted tool with no "+
		"implementation is silently dropped by GetEnabledToolsForAgent.", selfLookupTool)
}

func TestMCPSurfaceRegistersSelfLookup(t *testing.T) {
	for _, name := range mcpSurface(t) {
		if name == selfLookupTool {
			return
		}
	}
	t.Fatalf("%q is missing from the Multica MCP tool server that local runtimes and "+
		"Claude Code consume.", selfLookupTool)
}

// A provider-less Gateway must omit the tool rather than answer a self-lookup
// with a card it assembled itself. Absent-and-honest beats present-and-wrong.
func TestGatewayOmitsSelfLookupWithoutProvider(t *testing.T) {
	tctx := cerebroruntime.ToolContext{AgentID: pgtype.UUID{Valid: true}, WorkspaceID: pgtype.UUID{Valid: true}}
	for _, name := range cerebroruntime.NewDefaultRegistry(nil, nil, tctx).Names() {
		if name == selfLookupTool {
			t.Fatal("the Gateway must not expose the self-lookup without a card provider")
		}
	}
}

package toolaccess

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/capabilityregistry"
	"github.com/multica-ai/multica/server/internal/cerebro/platformaccess"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
)

// TestResolutionModeDecidesWhetherAMemberCanLoosenAWorkspaceDeny proves the two
// modes are not interchangeable, which is the whole reason the listing path
// must not drift between them.
//
// Same authored rows — a workspace-level deny with a user-level allow under it.
// Hard floor keeps the deny (a lower layer may only tighten). Openable lets the
// user row win. If this test ever passes with both modes agreeing, the modes
// have collapsed and the assertion below is worthless.
func TestResolutionModeDecidesWhetherAMemberCanLoosenAWorkspaceDeny(t *testing.T) {
	in := toolpolicy.Input{Settings: map[toolpolicy.Layer]toolpolicy.Setting{
		toolpolicy.LayerWorkspace: toolpolicy.SettingDeny,
		toolpolicy.LayerUser:      toolpolicy.SettingAllow,
	}}

	floor := toolpolicy.ResolveWithMode(toolpolicy.ModeHardFloor, in)
	openable := toolpolicy.ResolveWithMode(toolpolicy.ModeOpenable, in)

	if floor.Setting != toolpolicy.SettingDeny {
		t.Fatalf("hard floor let a user row loosen a workspace deny: got %q", floor.Setting)
	}
	if openable.Setting != toolpolicy.SettingAllow {
		t.Fatalf("openable did not let the user row win: got %q — "+
			"the two modes no longer differ, so this test proves nothing", openable.Setting)
	}
}

// modeRecordingPolicyStub answers Resolve and ResolvePermission differently so a
// test can tell which one the listing path called.
type modeRecordingPolicyStub struct {
	resolveCalls           *int
	resolvePermissionCalls *int
}

func (s modeRecordingPolicyStub) Resolve(_ context.Context, _ toolpolicy.Query) (toolpolicy.Effective, error) {
	*s.resolveCalls++
	return toolpolicy.Effective{Setting: toolpolicy.SettingAllow}, nil
}

func (s modeRecordingPolicyStub) ResolvePermission(_ context.Context, _ toolpolicy.Query, _ platformaccess.Actor) (toolpolicy.Effective, error) {
	*s.resolvePermissionCalls++
	return toolpolicy.Effective{Setting: toolpolicy.SettingAllow}, nil
}

// TestOrdinaryToolsResolveThroughTheTightenOnlyChain pins the security floor for
// the tool listing an agent is handed at claim time.
//
// FIR-3781: #2687 routed every key through ResolvePermission, which sends a
// non-special key to ResolveDeclared, which switches the chain to ModeOpenable
// whenever cerebro_member_override is on — and it is on by default. The effect
// on production, same runtime and same agent, was that the effective tool list
// in a claim went from ~34KB to ~68KB: agents were handed roughly twice the
// tools their administrators had granted.
//
// Nothing failed. No verdict test caught it, because every existing test
// asserted WHICH answer came back, never under WHICH model it was decided.
// This test asserts the model.
func TestOrdinaryToolsResolveThroughTheTightenOnlyChain(t *testing.T) {
	var resolveCalls, resolvePermissionCalls int
	service := New(capabilityListStub{views: []capabilityregistry.View{
		{Key: "mcp__github__create_issue", Title: "Create issue", Source: "scan"},
		{Key: "mcp__slack__post", Title: "Post message", Source: "scan"},
	}}, modeRecordingPolicyStub{
		resolveCalls:           &resolveCalls,
		resolvePermissionCalls: &resolvePermissionCalls,
	})

	if _, err := service.ListEffectiveTools(context.Background(), Query{
		RuntimeMode:     "cloud",
		RuntimeProvider: "firtal-gateway",
		AgentID:         pgtype.UUID{Valid: true},
	}); err != nil {
		t.Fatal(err)
	}

	if resolvePermissionCalls != 0 {
		t.Fatalf("ordinary tools went through ResolvePermission %d times; that path can "+
			"resolve ModeOpenable and let a member row loosen an inherited deny",
			resolvePermissionCalls)
	}
	if resolveCalls != 2 {
		t.Fatalf("tighten-only Resolve called %d times, want one per capability", resolveCalls)
	}
}

package runtime

import (
	"context"
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	"github.com/multica-ai/multica/server/internal/handler"
)

func claimIdent() handler.CerebroConnectionClaimIdentity {
	return handler.CerebroConnectionClaimIdentity{
		WorkspaceID: gateTestUUID(1),
		RuntimeID:   gateTestUUID(2),
		AgentID:     gateTestUUID(3),
		OwnerID:     gateTestUUID(4),
	}
}

// The claim consumes the mcp_http half of Resolve. With the flag on it is
// authoritative (handled=true): the injected --mcp-config carries only the
// Allow-only connection (alpha), the mixed connections (beta Ask, gamma Deny) are
// withheld from the raw relay, and the Deny tool surfaces as a --disallowedTools
// token — all from ONE Resolve so servers and deny can never drift.
func TestClaimConnectionMCPFlagOnAuthoritative(t *testing.T) {
	mcp, deny, handled := newMixedResolver().ClaimConnectionMCP(context.Background(), claimIdent())
	if !handled {
		t.Fatal("flag on must make the resolver authoritative (handled=true)")
	}

	servers := parseMCPServers(t, mcp)
	if _, ok := servers["alpha"]; !ok {
		t.Error("alpha is Allow-only and must be injected as a raw relay entry at claim")
	}
	if _, ok := servers["beta"]; ok {
		t.Error("beta has an Ask tool and must be withheld from the claim relay")
	}
	if _, ok := servers["gamma"]; ok {
		t.Error("gamma has a Deny tool and must be withheld from the claim relay")
	}
	if _, ok := servers["apiconn"]; ok {
		t.Error("an api connection must never be injected as an mcp relay entry")
	}

	if len(deny) != 1 || deny[0] != toolpolicy.MCPToolToken("gamma", "g2") {
		t.Errorf("claim deny = %v, want exactly [%s]", deny, toolpolicy.MCPToolToken("gamma", "g2"))
	}
}

// With the flag off the resolver is NOT authoritative (handled=false) and returns
// nil/nil, so handler.injectClaimConnectionMCP falls back to the legacy
// BuildMCPConfig/DisallowedMCPTools path — the reversibility guarantee.
func TestClaimConnectionMCPFlagOffFallsBackToLegacy(t *testing.T) {
	conns := fakeConnLister{conns: mixedVerdictConns()}
	apiOff := NewAPIConnectionResolver(conns,
		fakeEndpointPolicy{verdicts: map[string]toolpolicy.Setting{"apiconn GET /allow": toolpolicy.SettingAllow}},
		fakeFlag{on: false}, slog.Default())
	off := NewConnectionToolResolver(apiOff, conns, fakeMCPVerdicts{}, fakeFlag{on: false}, fakeEntry, slog.Default())

	mcp, deny, handled := off.ClaimConnectionMCP(context.Background(), claimIdent())
	if handled {
		t.Error("flag off must NOT be authoritative — the legacy claim path must run")
	}
	if mcp != nil || deny != nil {
		t.Errorf("flag off must return nil mcp + nil deny, got %v / %v", mcp, deny)
	}
}

// An incomplete identity (no agent) is never authoritative, so a malformed claim
// can never blank out the legacy injection by accident.
func TestClaimConnectionMCPRequiresAgent(t *testing.T) {
	ident := claimIdent()
	ident.AgentID = pgtype.UUID{}
	_, _, handled := newMixedResolver().ClaimConnectionMCP(context.Background(), ident)
	if handled {
		t.Error("a claim without an agent must not be authoritative")
	}
}

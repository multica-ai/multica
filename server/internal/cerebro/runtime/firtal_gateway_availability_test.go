package runtime

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/availabilityevidence"
	"github.com/multica-ai/multica/server/internal/handler"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type stubCapabilitiesProvider struct{}

func (stubCapabilitiesProvider) BuildAgentCapabilitiesCard(context.Context, pgtype.UUID) (handler.AgentCapabilities, error) {
	return handler.AgentCapabilities{}, nil
}

// The self-lookup is registered on the Gateway, so probing the REAL registry
// must find it. This is the positive half of the Kristian regression guard,
// asserted through the ledger the server actually serves from.
func TestGatewayLedgerFindsRegisteredTool(t *testing.T) {
	ledger := NewGatewayAvailabilityLedger(context.Background(), stubCapabilitiesProvider{}, db.New(nil))

	got := ledger.Lookup("platform:get_agent_capabilities", availabilityevidence.RuntimeFirtalGateway)
	if got.Level != availabilityevidence.LevelDiscovered {
		t.Errorf("level = %q (%s), want %q — the self-lookup IS in the Gateway registry",
			got.Level, got.Reason, availabilityevidence.LevelDiscovered)
	}
}

// The Kristian case itself: a tool the matrix declares callable, that the
// registry has no implementation for, must come back as declared — "the
// configuration says so and nothing found it" — never as an absent row a reader
// could mistake for an absent grant.
func TestGatewayLedgerReportsDeclaredButUnimplementedTool(t *testing.T) {
	ledger := NewGatewayAvailabilityLedger(context.Background(), stubCapabilitiesProvider{}, db.New(nil))

	var declaredOnly int
	for _, e := range ledger.All() {
		if e.Level == availabilityevidence.LevelDeclared {
			declaredOnly++
		}
	}
	if declaredOnly == 0 {
		t.Skip("every declared gateway tool is implemented — nothing to assert here today")
	}

	// Whatever the drift is today, it must be named rather than missing.
	for _, e := range ledger.All() {
		if e.Level == availabilityevidence.LevelDeclared && e.Reason == "" {
			t.Errorf("%s is unproven without saying why", e.CapabilityID)
		}
	}
}

// A probe must never present a found-but-untested tool as reality. Presence is
// not proof that the gate works.
func TestGatewayLedgerNeverClaimsVerifiedFromPresenceAlone(t *testing.T) {
	ledger := NewGatewayAvailabilityLedger(context.Background(), stubCapabilitiesProvider{}, db.New(nil))

	for _, e := range ledger.All() {
		if e.Level.IsReality() {
			t.Errorf("%s was presented as verified from a presence probe alone: %s",
				e.CapabilityID, e.Reason)
		}
	}
}

// Every declared-callable tool must be accounted for. A probe that silently
// skips names would reproduce the exact blindness this step removes.
func TestGatewayLedgerCoversEveryDeclaredCallableTool(t *testing.T) {
	ledger := NewGatewayAvailabilityLedger(context.Background(), stubCapabilitiesProvider{}, db.New(nil))

	for _, meta := range MulticaMCPToolMatrix() {
		if meta.Status == ToolStatusExcluded {
			continue
		}
		got := ledger.Lookup("platform:"+meta.Name, availabilityevidence.RuntimeFirtalGateway)
		if got.Reason == "no probe has run for this capability on this runtime" {
			t.Errorf("%q is declared callable but the probe never looked at it", meta.Name)
		}
	}
}

// A probe wired more weakly than the runtime invents gaps. Registration of the
// attachment tools is conditional on queries, so a probe handed nil reports them
// missing on a server where they work — a false alarm that would discredit the
// evidence as fast as a missed real one. Both tools must be found when the probe
// is wired as the runtime is.
func TestGatewayLedgerDoesNotInventGapsFromItsOwnWiring(t *testing.T) {
	ledger := NewGatewayAvailabilityLedger(context.Background(), stubCapabilitiesProvider{}, db.New(nil))

	for _, name := range []string{"list_attachments", "read_attachment"} {
		got := ledger.Lookup("platform:"+name, availabilityevidence.RuntimeFirtalGateway)
		if got.Level == availabilityevidence.LevelDeclared {
			t.Errorf("%q reported missing (%s) — the probe is wired more weakly than the runtime",
				name, got.Reason)
		}
	}
}

// The self-lookup registers only when a capabilities provider is present. A
// probe without one would report the Gateway's self-lookup missing on a server
// where it is registered — the mirror image of the Kristian case.
func TestGatewayLedgerWithoutProviderReportsSelfLookupMissing(t *testing.T) {
	ledger := NewGatewayAvailabilityLedger(context.Background(), nil, db.New(nil))

	got := ledger.Lookup("platform:get_agent_capabilities", availabilityevidence.RuntimeFirtalGateway)
	if got.Level != availabilityevidence.LevelDeclared {
		t.Errorf("level = %q, want %q — without a provider the Gateway really does not expose it",
			got.Level, availabilityevidence.LevelDeclared)
	}
}

// An excluded tool is not callable, so it must not appear as a capability at
// all — the ledger reports the declared-callable surface, not every string in
// the matrix.
func TestGatewayLedgerIgnoresExcludedTools(t *testing.T) {
	ledger := NewGatewayAvailabilityLedger(context.Background(), stubCapabilitiesProvider{}, db.New(nil))

	var excluded string
	for _, meta := range MulticaMCPToolMatrix() {
		if meta.Status == ToolStatusExcluded {
			excluded = meta.Name
			break
		}
	}
	if excluded == "" {
		t.Skip("no excluded tool in the matrix")
	}

	got := ledger.Lookup("platform:"+excluded, availabilityevidence.RuntimeFirtalGateway)
	if got.Reason != "no probe has run for this capability on this runtime" {
		t.Errorf("excluded tool %q was probed as if it were callable: %s", excluded, got.Reason)
	}
}

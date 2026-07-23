package runtime

// The Firtal Gateway availability probe (FIR-3398, Trin 2).
//
// This is the live version of the Kristian detector. The tool matrix declares
// which tools are callable, and SeedKristianTools grants exactly those — but a
// granted tool with no implementation is silently dropped by
// GetEnabledToolsForAgent. The declared list and the real registry drifted apart
// and nothing noticed.
//
// So this probe reads the two against each other on the running server: every
// tool the matrix declares callable, checked against the registry the Gateway
// actually executes from. A declared tool the registry does not have comes back
// as `declared` — configuration says so, nothing found it — which the
// capabilities card then shows the agent as "not proven" instead of as a
// capability it does not really have.
//
// Scope, stated plainly because the evidence is only as good as its scope: this
// reads the DEFAULT registry. The FIR-1449 in-process bridge (default-off, env
// gated) can widen a per-task registry beyond it, so on a bridge-enabled server
// a tool reported `declared` here may still be reachable. The probe under-claims
// in that case rather than over-claims, which is the right direction to fail —
// but it means "declared" here means "not in the default registry", not "cannot
// be called on any Gateway".
//
// It proves presence only, never `verified`. Verified demands a test call that
// proves the gate both lets the right caller in and turns the wrong caller away,
// and there is no such probe on the running server yet. Manufacturing one out of
// a subject that is trivially refused would make `verified` mean nothing, which
// would cost more than the gap it papered over.

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/availabilityevidence"
	"github.com/multica-ai/multica/server/internal/cerebro/capabilitycatalog"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// declaredCallableToolNames returns every tool the Multica MCP matrix marks
// callable — the exact set SeedKristianTools grants. Excluded tools are left
// out: they are not offered, so their absence from the registry is intended
// rather than drift.
func declaredCallableToolNames() []string {
	var out []string
	for _, meta := range MulticaMCPToolMatrix() {
		if meta.Status == ToolStatusExcluded {
			continue
		}
		out = append(out, meta.Name)
	}
	return out
}

// NewGatewayAvailabilityLedger probes the Gateway's real tool registry and
// returns the resulting evidence, keyed to Trin 1's canonical capability names.
//
// It must be handed the SAME dependencies the executor builds its per-task
// registries with, because registration is conditional on them: queries==nil
// drops the attachment tools, and a nil provider drops the self-lookup itself.
// A probe wired more weakly than the runtime reports gaps that do not exist,
// which discredits the evidence exactly as fast as missing a real one.
func NewGatewayAvailabilityLedger(ctx context.Context, provider AgentCapabilitiesProvider, queries *db.Queries) *availabilityevidence.Ledger {
	tctx := ToolContext{
		AgentID:              pgtype.UUID{Valid: true},
		WorkspaceID:          pgtype.UUID{Valid: true},
		CapabilitiesProvider: provider,
	}

	// The registry the Gateway executes from — not the matrix that declares it.
	// Reading the declaration here would only confirm the declaration.
	present := make([]string, 0)
	for _, name := range NewDefaultRegistry(nil, queries, tctx).Names() {
		present = append(present, capabilitycatalog.PlatformTool(name).ID)
	}
	surface := availabilityevidence.NewStaticSurface(availabilityevidence.RuntimeFirtalGateway, present)

	declared := make([]string, 0)
	for _, name := range declaredCallableToolNames() {
		declared = append(declared, capabilitycatalog.PlatformTool(name).ID)
	}

	ledger := &availabilityevidence.Ledger{}
	// Gate is nil on purpose: presence is all this probe can honestly prove.
	prober := availabilityevidence.Prober{Surface: surface}
	_ = prober.Probe(ctx, ledger, declared,
		availabilityevidence.Subject{Name: "self", Authorized: true},
		availabilityevidence.Subject{Name: "other-agent"},
	)
	return ledger
}

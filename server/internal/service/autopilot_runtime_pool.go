package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// SE-37711 / SE-37664: runtime selection for automatic run_only dispatch.
//
// This is the breaker-aware replacement for a bare AgentReadiness call on the
// automatic path. It walks the agent's ordered runtime pool and returns the
// first runtime that is both ready (online + access-valid) and not parked by a
// provider circuit hold, so a quota/auth refusal on the primary runtime fails
// over to the next binding instead of stalling the agent.
//
// The pure ordered pick and its all-held reporting live in runtime_circuit.go
// (selectFallbackRuntime); this method only resolves the eligibility facts each
// candidate needs from the runtime row and its circuit.

// selectPoolRuntime resolves which runtime an automatic run_only dispatch should
// pin from the agent's ordered pool.
//
// The pool is the agent's agent_runtime_binding rows in (priority, id) order; an
// agent with no binding rows falls back to the singleton pool implied by
// agent.runtime_id, so legacy single-runtime agents behave exactly as before
// (backfill migration 506 also keeps agent.runtime_id present as the priority-0
// binding once bindings exist, so the two views agree).
//
// firstVerdict is the readiness verdict of the highest-priority candidate; the
// caller uses it to phrase a fallbackNoneAvailable skip the same way the legacy
// single-runtime gate did.
//
// A NEW binding, runtime, or circuit lookup error fails CLOSED (returns err):
// a selector that cannot read the breaker must not route work past it. The
// callers map that error the same way they already map an AgentReadiness DB
// error — the admission gate fails open (a transient hiccup must not swallow a
// scheduled run, and nothing is routed yet), and dispatchRunOnly fails the run
// (no task is created, so no work lands on a runtime whose circuit we could not
// read).
func (s *AutopilotService) selectPoolRuntime(ctx context.Context, agent db.Agent) (fallbackDecision, AgentVerdict, error) {
	now := time.Now().UTC()
	lookup := s.runtimeLookup()

	bindings, err := s.Queries.ListAgentRuntimeBindings(ctx, agent.ID)
	if err != nil {
		return fallbackDecision{}, AgentVerdict{}, fmt.Errorf("list agent runtime bindings: %w", err)
	}

	var runtimeIDs []pgtype.UUID
	if len(bindings) == 0 {
		if !agent.RuntimeID.Valid {
			return fallbackDecision{Outcome: fallbackEmptyPool}, AgentVerdict{}, nil
		}
		runtimeIDs = []pgtype.UUID{agent.RuntimeID}
	} else {
		runtimeIDs = make([]pgtype.UUID, len(bindings))
		for i, b := range bindings {
			runtimeIDs[i] = b.RuntimeID
		}
	}

	candidates := make([]runtimeCandidate, 0, len(runtimeIDs))
	var firstVerdict AgentVerdict
	for i, rid := range runtimeIDs {
		rt, err := lookup.Get(ctx, rid)
		if err != nil {
			return fallbackDecision{}, AgentVerdict{}, fmt.Errorf("load runtime %s: %w", util.UUIDToString(rid), err)
		}
		verdict := runtimeVerdict(rt, agent)
		if i == 0 {
			firstVerdict = verdict
		}

		held := false
		var holdUntil time.Time
		var heldClass string
		var resetSource string
		var generation int64
		probeWindow := false
		circuit, err := s.Queries.GetRuntimeProviderCircuit(ctx, db.GetRuntimeProviderCircuitParams{
			RuntimeID: rid,
			Provider:  rt.Provider,
		})
		switch {
		case err == nil:
			// A circuit row exists: record its generation and reset source for the
			// dispatch audit (F6) regardless of whether it currently holds, so the
			// audit pins the exact failure epoch and how its window was derived.
			generation = circuit.Generation
			resetSource = circuit.ResetSource.String
			held = circuitHeldAt(circuit.State, circuit.ResetAt.Time, circuit.ResetAt.Valid, now)
			if held {
				// The failure class decides whether an ordered fallback is even
				// allowed: an auth/access hold on the primary never switches (F3),
				// a quota hold falls through. Empty when the row predates the
				// reason column; the selector then treats it as a plain hold.
				heldClass = circuit.Reason.String
			}
			// Only an open hold carries a meaningful deadline; a half_open probe
			// has no fixed reset, so its HoldUntil stays zero ("unknown", I9).
			if held && circuit.State == "open" && circuit.ResetAt.Valid {
				holdUntil = circuit.ResetAt.Time
			}
			// An open circuit whose reset window has elapsed is not held, but the
			// runtime is only eligible as a half-open probe: a dispatch onto it
			// must win the exactly-one-probe lease first (F4).
			if !held && circuit.State == "open" && circuit.ResetAt.Valid && !circuit.ResetAt.Time.After(now) {
				probeWindow = true
			}
		case errors.Is(err, pgx.ErrNoRows):
			// No circuit row means the breaker has never opened for this runtime:
			// closed, not held.
		default:
			return fallbackDecision{}, AgentVerdict{}, fmt.Errorf("load runtime circuit %s: %w", util.UUIDToString(rid), err)
		}

		candidates = append(candidates, runtimeCandidate{
			RuntimeID:   rid,
			Provider:    rt.Provider,
			Priority:    int64(i),
			Available:   verdict.Ready(),
			Held:        held,
			HeldClass:   heldClass,
			HoldUntil:   holdUntil,
			ResetSource: resetSource,
			Generation:  generation,
			ProbeWindow: probeWindow,
		})
	}

	return selectFallbackRuntime(candidates), firstVerdict, nil
}

// errProbeLeaseLost signals that a concurrent dispatch already holds the
// half-open probe lease for the chosen runtime, so this dispatch must defer
// instead of creating a duplicate probe task (F4).
var errProbeLeaseLost = errors.New("half-open probe lease already held")

// createRunOnlyTask inserts the run_only autopilot task, first winning the
// exactly-one half-open probe lease when the chosen runtime is in its probe
// window (an open circuit whose reset has elapsed — F4). An ordinary dispatch
// (no probe window) creates the task directly, exactly as before.
//
// When a probe IS required, the lease acquire and the task insert run in ONE
// transaction keyed to the new task id: if a concurrent scheduler already holds
// the lease, AcquireRuntimeProviderHalfOpenProbe returns zero rows, the
// transaction rolls back (no task row is left behind), and the caller defers via
// errProbeLeaseLost. So two schedulers racing the same elapsed circuit create
// exactly one probe task; the loser is held.
func (s *AutopilotService) createRunOnlyTask(ctx context.Context, chosen runtimeCandidate, params db.CreateAutopilotTaskParams) (db.AgentTaskQueue, error) {
	if !chosen.ProbeWindow {
		return s.Queries.CreateAutopilotTask(ctx, params)
	}
	if s.TxStarter == nil {
		// No transaction wiring (e.g. a narrow unit context): the acquire UPDATE
		// is still atomic at the DB level, so exactly-one holds; only the create
		// is not rolled back on a mid-flight crash, which is acceptable here.
		return s.acquireProbeAndCreate(ctx, s.Queries, chosen, params)
	}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return db.AgentTaskQueue{}, fmt.Errorf("begin probe tx: %w", err)
	}
	defer tx.Rollback(ctx)
	task, err := s.acquireProbeAndCreate(ctx, s.Queries.WithTx(tx), chosen, params)
	if err != nil {
		return db.AgentTaskQueue{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return db.AgentTaskQueue{}, fmt.Errorf("commit probe tx: %w", err)
	}
	return task, nil
}

func (s *AutopilotService) acquireProbeAndCreate(ctx context.Context, q *db.Queries, chosen runtimeCandidate, params db.CreateAutopilotTaskParams) (db.AgentTaskQueue, error) {
	rows, err := q.AcquireRuntimeProviderHalfOpenProbe(ctx, db.AcquireRuntimeProviderHalfOpenProbeParams{
		RuntimeID:      chosen.RuntimeID,
		Provider:       chosen.Provider,
		ProbeTaskID:    params.ID,
		ProbeExpiresAt: pgtype.Timestamptz{Time: time.Now().UTC().Add(circuitHalfOpenProbeLease), Valid: true},
	})
	if err != nil {
		return db.AgentTaskQueue{}, fmt.Errorf("acquire half-open probe: %w", err)
	}
	if len(rows) == 0 {
		return db.AgentTaskQueue{}, errProbeLeaseLost
	}
	return q.CreateAutopilotTask(ctx, params)
}

// dispatchRuntimeAudit is the never-silent evidence (F6) recorded on a run_only
// task whenever the pool selector routes it off a plain dispatch onto the
// agent's default runtime — an automatic failover to a lower-priority binding,
// or a half-open probe onto a recovering runtime. It is serialized into
// agent_task_queue.dispatch_runtime_audit and surfaced verbatim in task/run
// detail, so the reassignment is auditable long after the log line scrolls away.
// The daemon merges the per-execution model fail-safe outcome (model_action, F8)
// into the same object at task pickup; that field is empty here.
type dispatchRuntimeAudit struct {
	// Reason names the route class: runtime_failover, half_open_probe, or
	// failover_half_open_probe (a failover that also happens to be a probe).
	Reason string `json:"reason"`
	// Source is the agent's default runtime the dispatch moved away FROM; Target
	// is where the task was actually pinned. Both carry the provider so the audit
	// is legible without re-joining the runtime rows.
	SourceRuntimeID string `json:"source_runtime_id"`
	SourceProvider  string `json:"source_provider,omitempty"`
	TargetRuntimeID string `json:"target_runtime_id"`
	TargetProvider  string `json:"target_provider,omitempty"`
	TargetPriority  int64  `json:"target_priority"`
	// ProbeWindow is true when the target was chosen as a half-open probe (its
	// circuit was open but the reset window had elapsed).
	ProbeWindow bool `json:"probe_window,omitempty"`
	// Pool is the full ordered set of bindings the selector evaluated, each with
	// its priority and resolved circuit facts (hold class/until, reset source,
	// generation), so the audit records not just the winner but why every earlier
	// binding was skipped.
	Pool []dispatchRuntimeAuditPoolItem `json:"pool"`
	// ModelAction is merged in by the daemon (F8) via SetTaskDispatchModelAction;
	// left empty at dispatch time.
	ModelAction string `json:"model_action,omitempty"`
}

// dispatchRuntimeAuditPoolItem is one binding's snapshot in the dispatch audit.
type dispatchRuntimeAuditPoolItem struct {
	RuntimeID   string `json:"runtime_id"`
	Provider    string `json:"provider,omitempty"`
	Priority    int64  `json:"priority"`
	Chosen      bool   `json:"chosen,omitempty"`
	Held        bool   `json:"held,omitempty"`
	HeldClass   string `json:"held_class,omitempty"`
	HoldUntil   string `json:"hold_until,omitempty"`
	ResetSource string `json:"reset_source,omitempty"`
	Generation  int64  `json:"circuit_generation,omitempty"`
}

// dispatchAuditReason classifies the route the selector took, for the audit's
// top-level reason field.
func dispatchAuditReason(fellBack bool, chosen runtimeCandidate) string {
	switch {
	case fellBack && chosen.ProbeWindow:
		return "failover_half_open_probe"
	case chosen.ProbeWindow:
		return "half_open_probe"
	default:
		return "runtime_failover"
	}
}

// fallbackRouteMarker builds the specific, human-readable route suffix appended
// to a run_only task's trigger summary when the pool selector routed it off the
// agent's default runtime (F6: name the route, not a generic "via fallback
// runtime"). It names the reason and the target provider so a task-list row
// alone tells an operator WHERE the work went and WHY. Returns "" for an
// ordinary pin onto the default runtime (no marker, no audit).
func fallbackRouteMarker(fellBack bool, chosen runtimeCandidate) string {
	switch {
	case chosen.ProbeWindow:
		return " · half-open probe→" + chosen.Provider
	case fellBack:
		return " · failover→" + chosen.Provider
	default:
		return ""
	}
}

// buildDispatchRuntimeAudit assembles the F6 evidence object from the resolved
// pool. source is the agent's default runtime; chosen is the winning candidate;
// candidates is the full evaluated pool in priority order.
func buildDispatchRuntimeAudit(reason string, source pgtype.UUID, chosen runtimeCandidate, candidates []runtimeCandidate) dispatchRuntimeAudit {
	sourceID := util.UUIDToString(source)
	chosenID := util.UUIDToString(chosen.RuntimeID)
	audit := dispatchRuntimeAudit{
		Reason:          reason,
		SourceRuntimeID: sourceID,
		TargetRuntimeID: chosenID,
		TargetProvider:  chosen.Provider,
		TargetPriority:  chosen.Priority,
		ProbeWindow:     chosen.ProbeWindow,
		Pool:            make([]dispatchRuntimeAuditPoolItem, 0, len(candidates)),
	}
	for _, c := range candidates {
		cID := util.UUIDToString(c.RuntimeID)
		item := dispatchRuntimeAuditPoolItem{
			RuntimeID:   cID,
			Provider:    c.Provider,
			Priority:    c.Priority,
			Chosen:      cID == chosenID,
			Held:        c.Held,
			HeldClass:   c.HeldClass,
			ResetSource: c.ResetSource,
			Generation:  c.Generation,
		}
		if !c.HoldUntil.IsZero() {
			item.HoldUntil = c.HoldUntil.UTC().Format(time.RFC3339)
		}
		if cID == sourceID {
			audit.SourceProvider = c.Provider
		}
		audit.Pool = append(audit.Pool, item)
	}
	return audit
}

// poolAllHeldReason phrases the deferral message when every ready runtime in the
// agent's pool is under a provider circuit hold. It surfaces the earliest known
// reset so the skipped-run record and the failure monitor show when work can
// resume; when no held runtime carries a known deadline (e.g. a half_open probe
// in flight) it says manual action is required rather than fabricate a time (I9).
func poolAllHeldReason(ap db.Autopilot, decision fallbackDecision) string {
	who := "assignee agent"
	if ap.AssigneeType == "squad" {
		who = "squad leader"
	}
	if decision.EarliestKnown {
		return fmt.Sprintf(
			"%s: all %d bound runtime(s) held by provider circuit; earliest reset at %s",
			who, decision.HeldCount, decision.EarliestReset.UTC().Format(time.RFC3339),
		)
	}
	return fmt.Sprintf(
		"%s: all %d bound runtime(s) held by provider circuit; reset time unknown, manual action required",
		who, decision.HeldCount,
	)
}

// poolAuthHeldReason phrases the deferral when the primary runtime is held by an
// auth/access circuit. Unlike a quota hold, this never falls over to a lower
// binding (F3): the credential must be repaired, so the message names re-auth as
// the action and surfaces the hold window when known rather than implying a
// fallback ran.
func poolAuthHeldReason(ap db.Autopilot, decision fallbackDecision) string {
	who := "assignee agent"
	if ap.AssigneeType == "squad" {
		who = "squad leader"
	}
	if decision.EarliestKnown {
		return fmt.Sprintf(
			"%s: primary runtime held by provider auth/access circuit; no fallback (re-authentication required); hold until %s",
			who, decision.EarliestReset.UTC().Format(time.RFC3339),
		)
	}
	return fmt.Sprintf(
		"%s: primary runtime held by provider auth/access circuit; no fallback, re-authentication required",
		who,
	)
}

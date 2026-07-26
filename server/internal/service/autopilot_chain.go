package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// MaxChainDepth is the runtime backstop against a cycle that slipped past the
// config-time DFS check (concurrent chain-trigger creation) or a pathologically
// deep legitimate chain. Each fan-out increments the downstream run's chain_depth;
// when it would exceed this cap the successor is recorded as a `skipped` run with
// failure_reason=chain_depth_exceeded instead of being dispatched. Skipped runs
// do not themselves fire chain edges, so the recursion stops here.
const MaxChainDepth = 16

// chainEdgeStatuses returns the chain_on_status values that match an upstream
// run's terminal status: a 'completed' upstream fires 'completed'+'any' edges,
// a 'failed' upstream fires 'failed'+'any' edges. Anything else (e.g. a stray
// 'skipped') returns nil so the fan-out query matches nothing.
func chainEdgeStatuses(terminalStatus string) []string {
	switch terminalStatus {
	case "completed":
		return []string{"completed", "any"}
	case "failed":
		return []string{"failed", "any"}
	default:
		return nil
	}
}

// chainUpstreamStatuses are the terminal run statuses that fan out to chain
// successors. A `skipped` run is deliberately excluded: it is not an outcome
// (the work was never attempted / a concurrency cap held it back), so it must
// not fire a 'failed' edge and must not be treated as success either.
var chainUpstreamStatuses = map[string]bool{"completed": true, "failed": true}

// dispatchChainSuccessors fans out one downstream run per enabled chain
// trigger whose upstream is the given run's autopilot and whose chain_on_status
// matches the run's terminal status. It is the hook called at the terminal
// state transition (right after publishRunDone) - NOT inside publishRunDone,
// to keep that function single-responsibility.
//
// A `skipped` terminal status is a no-op (see chainUpstreamStatuses). Fan-out is
// synchronous for now (fan-out counts are small); a threshold-based move to the
// webhook-delivery worker pattern is a documented future option. Failures
// dispatching a single successor are logged and do not abort the remaining
// fan-out - siblings are isolated, by design (A -> {B, C}: B's dispatch error
// must not starve C).
func (s *AutopilotService) dispatchChainSuccessors(ctx context.Context, upstreamRun db.AutopilotRun, terminalStatus string) {
	if !chainUpstreamStatuses[terminalStatus] {
		return
	}
	if !upstreamRun.AutopilotID.Valid {
		return
	}
	if upstreamRun.ChainDepth >= MaxChainDepth {
		// This run is already at the depth cap - its successors would exceed it.
		// The cap is normally enforced in DispatchAutopilotForChain (which
		// records the skipped successor); reaching it here means this run was
		// created outside the chain path but somehow carries a maxed depth, so
		// just stop the fan-out defensively.
		slog.Warn("autopilot chain fan-out aborted: upstream run at depth cap",
			"upstream_run_id", util.UUIDToString(upstreamRun.ID),
			"autopilot_id", util.UUIDToString(upstreamRun.AutopilotID),
			"chain_depth", upstreamRun.ChainDepth,
		)
		return
	}

	successors, err := s.Queries.ListEnabledChainSuccessors(ctx, db.ListEnabledChainSuccessorsParams{
		UpstreamAutopilotID: upstreamRun.AutopilotID,
		AllowedStatuses:     chainEdgeStatuses(terminalStatus),
	})
	if err != nil {
		slog.Warn("autopilot chain fan-out: list successors failed",
			"upstream_run_id", util.UUIDToString(upstreamRun.ID),
			"autopilot_id", util.UUIDToString(upstreamRun.AutopilotID),
			"terminal_status", terminalStatus,
			"error", err,
		)
		return
	}
	if len(successors) == 0 {
		return
	}

	// Cache downstream autopilots so a fan-out to N successors on the same
	// downstream (rare but possible) loads it once.
	loaded := make(map[string]db.Autopilot, len(successors))
	for _, trig := range successors {
		downstream, ok := loaded[util.UUIDToString(trig.AutopilotID)]
		if !ok {
			downstream, err = s.Queries.GetAutopilot(ctx, trig.AutopilotID)
			if err != nil {
				slog.Warn("autopilot chain fan-out: load downstream autopilot failed",
					"upstream_run_id", util.UUIDToString(upstreamRun.ID),
					"downstream_autopilot_id", util.UUIDToString(trig.AutopilotID),
					"chain_trigger_id", util.UUIDToString(trig.ID),
					"error", err,
				)
				continue
			}
			loaded[util.UUIDToString(trig.AutopilotID)] = downstream
		}
		payload := buildChainPayload(upstreamRun, downstream, terminalStatus, trig.ChainOnStatus)
		if _, err := s.DispatchAutopilotForChain(ctx, upstreamRun, downstream, trig, payload); err != nil {
			slog.Warn("autopilot chain fan-out: dispatch successor failed",
				"upstream_run_id", util.UUIDToString(upstreamRun.ID),
				"downstream_autopilot_id", util.UUIDToString(downstream.ID),
				"chain_trigger_id", util.UUIDToString(trig.ID),
				"error", err,
			)
			continue
		}
	}
}

// DispatchAutopilotForChain is the per-successor dispatch entry point fired by
// dispatchChainSuccessors. It mirrors the webhook-delivery admission path:
// admission gate -> idempotent run creation (anchored on
// (chain_upstream_run_id, trigger_id)) -> downstream side effect.
//
// Depth cap: the downstream run's chain_depth is upstream_run.chain_depth + 1.
// If that exceeds MaxChainDepth the successor is recorded as a `skipped` run
// with failure_reason=chain_depth_exceeded and NO side effect / NO further
// fan-out - the backstop stops the recursion cleanly without phantom failures
// cascading down a 'failed' edge.
//
// The downstream cap (max_concurrent_runs from Stage 2, not yet on main) applies
// naturally because this entry point funnels through the same dispatchAutopilotRun
// path as schedule / webhook / api. When Stage 2 lands, a capped dispatch records
// `skipped`(reason=concurrency_cap) and - per the chain semantics - that skipped
// run fires no further chain edges.
func (s *AutopilotService) DispatchAutopilotForChain(
	ctx context.Context,
	upstreamRun db.AutopilotRun,
	downstreamAP db.Autopilot,
	chainTrigger db.AutopilotTrigger,
	payload []byte,
) (*db.AutopilotRun, error) {
	if chainTrigger.Kind != "chain" || !chainTrigger.UpstreamAutopilotID.Valid {
		return nil, fmt.Errorf("chain dispatch: trigger %s is not a chain trigger", util.UUIDToString(chainTrigger.ID))
	}
	if !upstreamRun.ID.Valid {
		return nil, fmt.Errorf("chain dispatch: upstream run id is required")
	}

	depth := upstreamRun.ChainDepth + 1
	if depth > MaxChainDepth {
		return s.recordChainDepthExceededRun(ctx, downstreamAP, chainTrigger, upstreamRun, depth, payload)
	}

	// Idempotency fast path: a replayed terminal event (SyncRunFrom* racing
	// itself, event redelivery) reuses the run already anchored on
	// (upstream_run, chain trigger) instead of creating a second downstream
	// run. The partial unique index (migration 215) is the hard guard.
	if existing, err := s.Queries.GetAutopilotRunByChainUpstream(ctx, db.GetAutopilotRunByChainUpstreamParams{
		ChainUpstreamRunID: upstreamRun.ID,
		TriggerID:          chainTrigger.ID,
	}); err == nil {
		return &existing, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("chain dispatch: lookup existing run: %w", err)
	}

	// Admission gate (same as schedule / webhook / api): an offline assignee
	// runtime records a `skipped` run instead of enqueuing a doomed task.
	if reason, _, skip := s.shouldSkipDispatch(ctx, downstreamAP, pgtype.UUID{}); skip {
		return s.recordChainSkippedRun(ctx, downstreamAP, chainTrigger, upstreamRun, depth, payload, reason)
	}

	initialStatus := "issue_created"
	if downstreamAP.ExecutionMode == "run_only" {
		initialStatus = "running"
	}
	run, err := s.Queries.CreateAutopilotRun(ctx, db.CreateAutopilotRunParams{
		AutopilotID:        downstreamAP.ID,
		TriggerID:          chainTrigger.ID,
		Source:             "chain",
		Status:             initialStatus,
		TriggerPayload:     payload,
		SquadID:            autopilotSquadAttribution(downstreamAP),
		ChainDepth:         pgtype.Int4{Int32: int32(depth), Valid: true},
		ChainUpstreamRunID: upstreamRun.ID,
	})
	if err != nil {
		// Race: another caller won the idempotent-insert race between our
		// lookup and our INSERT. Reuse the winner.
		if existing, ok := s.recoverConcurrentChainAdmission(ctx, upstreamRun.ID, chainTrigger.ID, err); ok {
			return existing, nil
		}
		return nil, fmt.Errorf("chain dispatch: create run: %w", err)
	}
	s.captureAutopilotRunStarted(downstreamAP, run, "chain")

	// Drive the downstream side effect (create_issue / run_only). dispatchAutopilotRun
	// owns the post-admission errDispatchSkipped -> skipped rewrite and the
	// run-start event publish, mirroring every other dispatch entry point.
	finalRun, _, err := s.dispatchAutopilotRun(ctx, downstreamAP, chainTrigger.ID, "chain", &run, pgtype.UUID{})
	if err != nil {
		slog.Warn("autopilot chain dispatch: downstream side effect failed",
			"upstream_run_id", util.UUIDToString(upstreamRun.ID),
			"downstream_run_id", util.UUIDToString(run.ID),
			"downstream_autopilot_id", util.UUIDToString(downstreamAP.ID),
			"error", err,
		)
	}
	return finalRun, nil
}

// recoverConcurrentChainAdmission mirrors recoverConcurrentWebhookAdmission: a
// 23505 (unique_violation) on CreateAutopilotRun means another replica / event
// replay won the idempotent-insert race and already created the downstream run
// for this (upstream run, chain trigger) pair. Reuse it.
func (s *AutopilotService) recoverConcurrentChainAdmission(ctx context.Context, upstreamRunID, chainTriggerID pgtype.UUID, cause error) (*db.AutopilotRun, bool) {
	var pgErr *pgconn.PgError
	if !errors.As(cause, &pgErr) || pgErr.Code != "23505" {
		return nil, false
	}
	existing, err := s.Queries.GetAutopilotRunByChainUpstream(ctx, db.GetAutopilotRunByChainUpstreamParams{
		ChainUpstreamRunID: upstreamRunID,
		TriggerID:          chainTriggerID,
	})
	if err == nil {
		return &existing, true
	}
	return nil, false
}

// recordChainSkippedRun writes a `skipped` chain run when the downstream
// admission gate fails (offline assignee runtime, archived agent, etc.). The
// run is anchored on (chain_upstream_run_id, trigger_id) so a replayed terminal
// event reuses it instead of stacking duplicate skipped runs. Skipped runs do
// not fire chain edges, so this never cascades.
func (s *AutopilotService) recordChainSkippedRun(
	ctx context.Context,
	downstreamAP db.Autopilot,
	chainTrigger db.AutopilotTrigger,
	upstreamRun db.AutopilotRun,
	depth int32,
	payload []byte,
	reason string,
) (*db.AutopilotRun, error) {
	run, err := s.Queries.CreateAutopilotRun(ctx, db.CreateAutopilotRunParams{
		AutopilotID:        downstreamAP.ID,
		TriggerID:          chainTrigger.ID,
		Source:             "chain",
		Status:             "skipped",
		TriggerPayload:     payload,
		SquadID:            autopilotSquadAttribution(downstreamAP),
		ChainDepth:         pgtype.Int4{Int32: depth, Valid: true},
		ChainUpstreamRunID: upstreamRun.ID,
	})
	if err != nil {
		if existing, ok := s.recoverConcurrentChainAdmission(ctx, upstreamRun.ID, chainTrigger.ID, err); ok {
			return existing, nil
		}
		return nil, fmt.Errorf("chain dispatch: create skipped run: %w", err)
	}
	updated, uerr := s.Queries.UpdateAutopilotRunSkipped(ctx, db.UpdateAutopilotRunSkippedParams{
		ID:            run.ID,
		FailureReason: pgtype.Text{String: reason, Valid: true},
	})
	if uerr == nil {
		run = updated
	}
	slog.Info("autopilot chain dispatch skipped at admission",
		"upstream_run_id", util.UUIDToString(upstreamRun.ID),
		"downstream_autopilot_id", util.UUIDToString(downstreamAP.ID),
		"run_id", util.UUIDToString(run.ID),
		"reason", reason,
	)
	s.captureAutopilotRunStarted(downstreamAP, run, "chain")
	s.publishRunDone(util.UUIDToString(downstreamAP.WorkspaceID), run, "skipped")
	return &run, nil
}

// recordChainDepthExceededRun is the depth-cap backstop: the downstream would
// land at chain_depth > MaxChainDepth, so instead of dispatching its side
// effect we record a single `skipped` run carrying failure_reason=
// chain_depth_exceeded. Skipped (not failed): it must not fire any
// chain_on_status='failed' edge and must not trip the failure-rate auto-pause.
func (s *AutopilotService) recordChainDepthExceededRun(
	ctx context.Context,
	downstreamAP db.Autopilot,
	chainTrigger db.AutopilotTrigger,
	upstreamRun db.AutopilotRun,
	depth int32,
	payload []byte,
) (*db.AutopilotRun, error) {
	run, err := s.Queries.CreateAutopilotRun(ctx, db.CreateAutopilotRunParams{
		AutopilotID:        downstreamAP.ID,
		TriggerID:          chainTrigger.ID,
		Source:             "chain",
		Status:             "skipped",
		TriggerPayload:     payload,
		SquadID:            autopilotSquadAttribution(downstreamAP),
		ChainDepth:         pgtype.Int4{Int32: depth, Valid: true},
		ChainUpstreamRunID: upstreamRun.ID,
	})
	if err != nil {
		if existing, ok := s.recoverConcurrentChainAdmission(ctx, upstreamRun.ID, chainTrigger.ID, err); ok {
			return existing, nil
		}
		return nil, fmt.Errorf("chain dispatch: create depth-exceeded run: %w", err)
	}
	updated, uerr := s.Queries.UpdateAutopilotRunSkipped(ctx, db.UpdateAutopilotRunSkippedParams{
		ID:            run.ID,
		FailureReason: pgtype.Text{String: "chain_depth_exceeded", Valid: true},
	})
	if uerr == nil {
		run = updated
	}
	slog.Warn("autopilot chain dispatch aborted: chain depth exceeded",
		"upstream_run_id", util.UUIDToString(upstreamRun.ID),
		"downstream_autopilot_id", util.UUIDToString(downstreamAP.ID),
		"run_id", util.UUIDToString(run.ID),
		"chain_depth", depth,
	)
	s.captureAutopilotRunStarted(downstreamAP, run, "chain")
	s.publishRunDone(util.UUIDToString(downstreamAP.WorkspaceID), run, "skipped")
	return &run, nil
}

// chainRunPayload is the structured trigger_payload stored on a chain-fired
// downstream run. It carries enough upstream context for observability and for
// a future template-interpolation hook (out of scope: parameterized manual run,
// gap #4). chain_depth lives on the column, not here.
type chainRunPayload struct {
	Chain struct {
		UpstreamRunID       string `json:"upstream_run_id"`
		UpstreamAutopilotID string `json:"upstream_autopilot_id"`
		UpstreamStatus      string `json:"upstream_status"`
		ChainOnStatus       string `json:"chain_on_status"`
	} `json:"chain"`
	UpstreamResult json.RawMessage `json:"upstream_result,omitempty"`
}

func buildChainPayload(upstreamRun db.AutopilotRun, downstreamAP db.Autopilot, terminalStatus, chainOnStatus string) []byte {
	p := chainRunPayload{}
	p.Chain.UpstreamRunID = util.UUIDToString(upstreamRun.ID)
	p.Chain.UpstreamAutopilotID = util.UUIDToString(upstreamRun.AutopilotID)
	p.Chain.UpstreamStatus = terminalStatus
	p.Chain.ChainOnStatus = chainOnStatus
	if len(upstreamRun.Result) > 0 {
		p.UpstreamResult = json.RawMessage(upstreamRun.Result)
	}
	b, err := json.Marshal(p)
	if err != nil {
		return nil
	}
	return b
}

// DetectChainCycle is the config-time cycle guard. proposedUpstream ->
// proposedDownstream is the chain edge about to be created (the trigger lives
// on proposedDownstream, naming proposedUpstream as its upstream). A cycle
// exists iff proposedDownstream can already reach proposedUpstream through the
// existing chain edges in the workspace - because then adding the edge closes
// the loop (proposedUpstream -> proposedDownstream -> ... -> proposedUpstream).
// A self-edge (proposedUpstream == proposedDownstream) is also a cycle.
//
// The walk is in Go (not SQL) because depth-bounded recursive CTEs are awkward
// to bound correctly and the per-workspace chain graph is small. q is the
// caller's *db.Queries (tx-scoped where the caller wants the cycle check to
// see in-flight rows from the same transaction).
//
// This is the PRIMARY cycle defense; the runtime depth cap (MaxChainDepth) is
// the documented backstop for the concurrent-create race where two edges that
// individually pass DFS together form a cycle.
func DetectChainCycle(ctx context.Context, q *db.Queries, workspaceID, proposedDownstream, proposedUpstream pgtype.UUID) (bool, error) {
	if !workspaceID.Valid || !proposedDownstream.Valid || !proposedUpstream.Valid {
		return false, nil
	}
	if proposedDownstream == proposedUpstream {
		return true, nil
	}
	edges, err := q.ListChainTriggersInWorkspace(ctx, workspaceID)
	if err != nil {
		return false, fmt.Errorf("list chain triggers for cycle check: %w", err)
	}
	// adjacency: upstream -> [downstreams]. An edge "U fires D" is stored as
	// trigger(upstream_autopilot_id=U, autopilot_id=D), so from U we can
	// reach D.
	adj := make(map[string][]string, len(edges))
	for _, e := range edges {
		from := util.UUIDToString(e.UpstreamAutopilotID)
		to := util.UUIDToString(e.AutopilotID)
		adj[from] = append(adj[from], to)
	}
	target := util.UUIDToString(proposedUpstream)
	start := util.UUIDToString(proposedDownstream)
	visited := map[string]bool{start: true}
	stack := []string{start}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, next := range adj[cur] {
			if next == target {
				return true, nil
			}
			if !visited[next] {
				visited[next] = true
				stack = append(stack, next)
			}
		}
	}
	return false, nil
}

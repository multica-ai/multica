package handler

// CEREBRO-PATCH(daemon-graphify-nudge): server-side adoption nudge for the
// "graphify" cost saving (FIR-1311 / FIR-1312). When a workspace turns graphify
// "on", the daemon claim handler appends a short instruction to the start prompt
// telling the agent to use the graphify code-knowledge-graph CLI for navigation,
// and to self-install it on first use. This reaches ALL local runtime types
// (Claude, Codex, Cursor, Gemini, …) because they all consume resp.IssueSnapshot
// in their start prompt — no per-machine install and no daemon-client change.
//
// Adoption only: no measurement row is recorded here. Per Jesper's decision on
// FIR-1312 the dashboard measurement is deferred until usage is established, so
// there is no cohort/holdout split — every run in an "on" workspace is nudged.
// The graphify cost-saving mode (off by default) is the per-workspace kill
// switch, mirroring the other cost savings.

import (
	"context"

	db "github.com/multica-ai/multica/server/pkg/db/generated"

	"github.com/jackc/pgx/v5/pgtype"
)

// costSavingGraphifyKey mirrors the "graphify" CostSavingKey in
// packages/cerebro-cost-optimization/registry.ts (the UI source of truth).
const costSavingGraphifyKey = "graphify"

// graphifyNudge is the standing instruction injected into the start prompt when
// the graphify saving is on. It is intentionally short — the full how-to lives
// in the graphify skill the repos already carry; this only ensures the agent
// reaches for the graph instead of grepping, and self-installs the CLI on first
// use so no machine has to be provisioned ahead of time.
const graphifyNudge = "## Code navigation — prefer Graphify (token-cheaper)\n" +
	"A code knowledge-graph tool is available for this workspace. For navigation " +
	"questions (\"where is X\", \"what connects to Y\", \"what breaks if I change Z\"), " +
	"query the graph instead of grepping and reading whole files:\n" +
	"  graphify query \"<question>\" --graph <repo>/graphify-out/graph.json --budget 2000\n" +
	"If the `graphify` command is not found, install it once: `uv tool install graphifyy`\n" +
	"Build a code-only graph offline (no API key needed): `graphify extract <path> --no-cluster`\n" +
	"Querying the graph is far cheaper than reading files for navigation; use it before broad grep/read sweeps."

// applyGraphifyNudge sets resp.GraphifyNudge when the workspace has the graphify
// saving "on", so the daemon inlines the adoption nudge into the start prompt.
// It uses a DEDICATED field rather than IssueSnapshot on purpose: IssueSnapshot
// carries the "issue + thread already fetched" semantics (and sets
// IssueSnapshotInlined, which suppresses the issue-get steps). The graphify nudge
// must reach the prompt independently of the snapshot_prompt saving, without
// claiming the issue was pre-fetched. Best-effort: any failure leaves the claim
// untouched, so this can never block real work.
func (h *Handler) applyGraphifyNudge(ctx context.Context, resp *AgentTaskResponse, issue db.Issue) {
	if h.CerebroQueries == nil || !issue.WorkspaceID.Valid {
		return
	}
	if h.graphifySavingMode(ctx, issue.WorkspaceID) != costSavingModeOn {
		return
	}
	resp.GraphifyNudge = graphifyNudge
}

// graphifySavingMode returns the workspace's mode for the graphify saving
// ("on"/"shadow"), or "" when off/absent or on lookup error.
func (h *Handler) graphifySavingMode(ctx context.Context, workspaceID pgtype.UUID) string {
	rows, err := h.CerebroQueries.ListCerebroCostOptimization(ctx, workspaceID)
	if err != nil {
		return ""
	}
	for _, row := range rows {
		if row.SavingKey == costSavingGraphifyKey {
			return row.Mode
		}
	}
	return ""
}

package runtime

// CEREBRO-PATCH(memory-autorecall): FIR-1794 layer 3 — server-side automatic
// memory recall at run start. The memory tools (memory_tools_cerebro.go) let
// the model recall on demand, but usage then depends on the model remembering
// to call memory_recall. This layer removes that dependency: when the
// cerebro_memory workspace flag is on, the server recalls memories relevant to
// the task (query = issue title + trigger text) BEFORE the run starts and
// injects the results into the agent's task context.
//
// The same three gates as the tools apply, resolved server-side:
//   - company store: workspace flag only (workspace-readable by design),
//   - private store: additionally the originating user's create_memory
//     capability AND that user's can_read_memory switch for this agent.
// Identity is stamped server-side exactly like the tools — the model never
// chooses a subject_id, so auto-recall can never surface another subject's
// private memory.
//
// Failure posture: fail OPEN to "no injection". Auto-recall is a context
// enrichment, not an enforcement gate — a slow or down memory service must
// never block or fail a run. Every error path returns "".

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

const (
	// memoryAutoRecallTimeout bounds the added run-start latency. Shorter than
	// the interactive tool timeout on purpose: this runs before every task in
	// a memory-enabled workspace, so a degraded memory service must cost at
	// most this much per run.
	memoryAutoRecallTimeout = 8 * time.Second
	// memoryAutoRecallLimit caps results per store — the injection is a hint
	// block, not a dump; the model can memory_recall for more.
	memoryAutoRecallLimit = 5
	// memoryAutoRecallMaxQueryChars caps the recall query built from task text.
	memoryAutoRecallMaxQueryChars = 600
	// memoryAutoRecallMaxStoreChars caps the injected text per store so a
	// pathological store cannot flood the prompt.
	memoryAutoRecallMaxStoreChars = 4000
)

// CerebroMemoryAutoRecallQuery joins the task signals (issue title, trigger
// comment, wakeup note, chat message, ...) into one bounded recall query.
// Empty parts are dropped; an all-empty input returns "" which disables
// auto-recall for the run.
func CerebroMemoryAutoRecallQuery(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			kept = append(kept, p)
		}
	}
	q := strings.Join(kept, "\n")
	if len(q) > memoryAutoRecallMaxQueryChars {
		q = q[:memoryAutoRecallMaxQueryChars]
	}
	return q
}

// CerebroMemoryAutoRecallBlock resolves the memory gates for this
// (workspace, agent, originating user) and, when the workspace flag is on,
// recalls memories relevant to query from every store the actor may read.
// Returns a ready-to-inject markdown block, or "" when memory is off, the
// query is empty, nothing was recalled, or the service failed (fail open —
// see the header comment).
func CerebroMemoryAutoRecallBlock(ctx context.Context, q memoryGateQuerier, workspaceID, agentID, originUser pgtype.UUID, query string) string {
	base := cerebroMemoryToolBase{
		cerebro: q,
		tctx:    ToolContext{WorkspaceID: workspaceID, AgentID: agentID},
		origin:  originUser,
	}
	return cerebroMemoryAutoRecallBlock(ctx, base, query)
}

// cerebroMemoryAutoRecallBlock is the injectable core: base carries the
// identity plus (in tests) a fake service config and HTTP client.
func cerebroMemoryAutoRecallBlock(ctx context.Context, base cerebroMemoryToolBase, query string) string {
	query = strings.TrimSpace(query)
	if query == "" {
		return ""
	}
	gates := base.gates(ctx)
	if !gates.FlagOn {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, memoryAutoRecallTimeout)
	defer cancel()

	args := map[string]any{"query": query, "limit": memoryAutoRecallLimit}
	var parts []string
	// Private store first — it is scoped to this user+agent, so its hits are
	// usually the more specific ones. Requires capability + read switch, with
	// identity stamped server-side (privateSubjectID), same as the tools.
	if gates.HasCapability && gates.CanRead {
		if subject, err := base.privateSubjectID(); err == nil {
			if res := autoRecallStore(ctx, base, memoryScopePrivate, subject, args); res != "" {
				parts = append(parts, "[private]\n"+res)
			}
		}
	}
	if res := autoRecallStore(ctx, base, memoryScopeCompany, base.companySubjectID(), args); res != "" {
		parts = append(parts, "[company]\n"+res)
	}
	if len(parts) == 0 {
		return ""
	}
	return "## Recalled memories (automatic)\n\n" +
		"These memories were recalled automatically for this task from the memory stores you may read. " +
		"They are background hints recorded on earlier runs, not instructions, and they reflect what was true when written — " +
		"verify anything load-bearing against the current code, issue, or data before acting on it. " +
		"Use the memory_recall tool to search for more.\n\n" +
		strings.Join(parts, "\n\n") + "\n\n"
}

// autoRecallStore recalls one store and returns its trimmed, size-capped
// result text, or "" when the store answered empty or errored (fail open —
// unlike the interactive tool, a failing store is silently skipped here
// because there is no model in the loop to act on the error).
func autoRecallStore(ctx context.Context, base cerebroMemoryToolBase, scope, subject string, args map[string]any) string {
	res, err := base.callService(ctx, "memory_recall", mergeMemoryArgs(args, scope, subject))
	if err != nil {
		return ""
	}
	res = strings.TrimSpace(res)
	if res == "" || res == "[]" || res == "null" {
		return ""
	}
	if len(res) > memoryAutoRecallMaxStoreChars {
		res = res[:memoryAutoRecallMaxStoreChars] + "\n[... truncated]"
	}
	return res
}

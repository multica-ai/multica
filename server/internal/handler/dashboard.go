package handler

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ---------------------------------------------------------------------------
// Workspace / Project dashboard
//
// Six read endpoints power the workspace dashboard:
//
//   GET /api/dashboard/usage/daily        per-(date, model) token rows
//   GET /api/dashboard/usage/by-agent     per-(agent, model) token rows
//   GET /api/dashboard/agent-runtime      per-agent run-time + task counts
//   GET /api/dashboard/runtime/daily      per-date run-time + task counts
//   GET /api/dashboard/failures/daily     per-(date, failure_reason) counts
//   GET /api/dashboard/failures/by-agent  per-(agent, failure_reason) counts
//
// All of them accept ?days=N (defaults to 30, capped at 365) and an optional
// ?project_id=<uuid> to scope the rollup to a single project. With no
// project_id the data spans the whole workspace.
//
// Cutoff convention: the three date-bucketed series use parseSinceParamInTZ
// (N+1 calendar days, the surplus day trimmed client-side with `-(days-1)`),
// and the three per-AGENT rollups use parseExactSinceParamInTZ (exactly N).
// Rows without a date cannot be trimmed client-side, so serving them off the
// N+1 cutoff makes the leaderboard and the Run time / Tasks KPIs cover one
// calendar day more than the chart and the Cost / Tokens KPIs beside them —
// at 1D that let a single agent's row read higher than the workspace total
// (MUL-5551). Keep the two halves of each pair on matching windows.
//
// Cost is computed client-side from a per-model pricing table — the model
// dimension is intentionally preserved on the wire (same convention as the
// per-runtime usage endpoints).
//
// Access control: the workspace-wide series (usage/daily, runtime/daily,
// failures/daily) carry no agent dimension and need workspace membership only —
// token spend / run time / failure volume are workspace-level operational
// metrics. The three per-AGENT rollups additionally apply per-agent visibility:
// see foldRestrictedAgents.
// ---------------------------------------------------------------------------

// restrictedAgentsRowID is the synthetic agent_id that every row this response
// refuses to name is folded onto. Deliberately not a UUID, so it can never
// collide with a real agent id, and distinct from the client's "deleted agents"
// bucket: those agents are gone, these are alive and still running.
const restrictedAgentsRowID = "__restricted_agents__"

// foldRestrictedAgents rewrites every row named by `restricted` (see
// restrictedAgentIDs) onto restrictedAgentsRowID, merging the rewritten rows
// that then collide on whatever dimensions the row has left (provider+model,
// failure_reason, or nothing at all).
//
// "Private" is a promise the rest of the codebase keeps — agent detail 403s,
// ListAgents filters, even an admin cannot invoke someone else's private agent.
// These three endpoints used to break it by returning bare agent UUIDs for the
// whole workspace, which told a plain member that a private agent exists, how
// much it runs, and what it fails on. The client already collapsed those rows,
// but client-side filtering is decoration: one curl bypasses it.
//
// The same fold covers the hidden `kind = 'system'` builder carriers, which no
// list endpoint returns to anyone: aggregating over agent_task_queue /
// task_usage picks them up regardless of kind, so without this they arrive as a
// bare UUID no client can name.
//
// Folding rather than dropping: each of these responses is the per-agent half
// of a pair whose other half (usage/daily, runtime/daily, failures/daily) is
// workspace-scoped and unfiltered, so dropping rows would make the per-agent
// breakdown stop adding up to the totals rendered directly beside it. One
// merged bucket keeps every sum intact while carrying no real agent id and no
// per-agent split.
//
// The retained dimensions leak nothing new: the workspace-level series already
// exposes each (provider, model) / failure_reason total for the whole
// workspace, and every visible agent's rows are returned in full, so the
// restricted remainder was always one subtraction away.
//
// `rewrite` stamps the sentinel id and returns the merge key for what remains;
// `merge` accumulates a later row onto the one already emitted. Row order is
// otherwise preserved, with the bucket sitting where its first member was — the
// client re-ranks all of these anyway.
func foldRestrictedAgents[T any, K comparable](
	rows []T,
	restricted map[string]struct{},
	agentIDOf func(T) string,
	rewrite func(T) (T, K),
	merge func(dst, src T) T,
) []T {
	if len(restricted) == 0 {
		return rows
	}
	out := make([]T, 0, len(rows))
	bucketAt := make(map[K]int)
	for _, row := range rows {
		if _, hidden := restricted[agentIDOf(row)]; !hidden {
			out = append(out, row)
			continue
		}
		folded, key := rewrite(row)
		if i, ok := bucketAt[key]; ok {
			out[i] = merge(out[i], folded)
			continue
		}
		bucketAt[key] = len(out)
		out = append(out, folded)
	}
	return out
}

// dashboardRestrictedAgents resolves the agents this request may not see. On
// failure it writes a 500 and returns ok=false: an unfiltered rollup is the one
// outcome this must never degrade to.
func (h *Handler) dashboardRestrictedAgents(
	w http.ResponseWriter,
	r *http.Request,
	workspaceID, role string,
) (map[string]struct{}, bool) {
	actorType, actorID := h.resolveActor(r, requestUserID(r), workspaceID)
	restricted, ok := h.restrictedAgentIDs(r.Context(), workspaceID, actorType, actorID, role)
	if !ok {
		writeError(w, http.StatusInternalServerError, "failed to resolve agent access")
		return nil, false
	}
	return restricted, true
}

// parseProjectIDParam reads ?project_id=<uuid> off the URL. Returns a
// pgtype.UUID with Valid=false when the param is absent so sqlc's nullable
// argument resolves to SQL NULL and the WHERE clause degrades to "no
// project filter". On a malformed UUID it writes a 400 and returns
// ok=false; callers must return immediately.
func parseProjectIDParam(w http.ResponseWriter, r *http.Request) (pgtype.UUID, bool) {
	raw := r.URL.Query().Get("project_id")
	if raw == "" {
		return pgtype.UUID{}, true
	}
	u, err := util.ParseUUID(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid project_id")
		return pgtype.UUID{}, false
	}
	return u, true
}

// DashboardUsageDailyResponse is one (date, provider, model) bucket. Cost-side
// math happens on the client from a per-model pricing table; provider + model
// stay on the wire so the client can disambiguate bare model ids that collide
// across providers (e.g. Cursor's `auto`).
type DashboardUsageDailyResponse struct {
	Date             string `json:"date"`
	Provider         string `json:"provider"`
	Model            string `json:"model"`
	InputTokens      int64  `json:"input_tokens"`
	OutputTokens     int64  `json:"output_tokens"`
	CacheReadTokens  int64  `json:"cache_read_tokens"`
	CacheWriteTokens int64  `json:"cache_write_tokens"`
	// Cost split: `CostUSDTicks` is what the provider itself charged for the
	// rows behind this aggregate (1e-10 USD), and the `Uncosted*` token
	// counts are the tokens from rows the provider did NOT price. The client
	// reports authoritative + estimate(uncosted), so a window mixing both
	// kinds of row stays whole. See migration 213.
	CostUSDTicks             int64 `json:"cost_usd_ticks"`
	UncostedInputTokens      int64 `json:"uncosted_input_tokens"`
	UncostedOutputTokens     int64 `json:"uncosted_output_tokens"`
	UncostedCacheReadTokens  int64 `json:"uncosted_cache_read_tokens"`
	UncostedCacheWriteTokens int64 `json:"uncosted_cache_write_tokens"`
	TaskCount                int32 `json:"task_count"`
}

// GetDashboardUsageDaily returns per-(date, model) token rows for the
// workspace, optionally scoped to a project. Backed by task_usage_hourly,
// sliced into calendar days under the viewer's tz.
func (h *Handler) GetDashboardUsageDaily(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	projectID, ok := parseProjectIDParam(w, r)
	if !ok {
		return
	}
	tz := h.resolveViewingTZ(r)
	since := parseSinceParamInTZ(r, 30, tz)

	resp, err := h.listDashboardUsageDaily(r.Context(), parseUUID(workspaceID), tz, since, projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list usage")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) listDashboardUsageDaily(
	ctx context.Context,
	workspaceID pgtype.UUID,
	tz string,
	since pgtype.Timestamptz,
	projectID pgtype.UUID,
) ([]DashboardUsageDailyResponse, error) {
	rows, err := h.Queries.ListDashboardUsageDaily(ctx, db.ListDashboardUsageDailyParams{
		WorkspaceID: workspaceID,
		Tz:          tz,
		Since:       since,
		ProjectID:   projectID,
	})
	if err != nil {
		return nil, err
	}
	resp := make([]DashboardUsageDailyResponse, len(rows))
	for i, row := range rows {
		resp[i] = DashboardUsageDailyResponse{
			Date:                     row.Date.Time.Format("2006-01-02"),
			Provider:                 row.Provider,
			Model:                    row.Model,
			InputTokens:              row.InputTokens,
			OutputTokens:             row.OutputTokens,
			CacheReadTokens:          row.CacheReadTokens,
			CacheWriteTokens:         row.CacheWriteTokens,
			CostUSDTicks:             row.CostUsdTicks,
			UncostedInputTokens:      row.UncostedInputTokens,
			UncostedOutputTokens:     row.UncostedOutputTokens,
			UncostedCacheReadTokens:  row.UncostedCacheReadTokens,
			UncostedCacheWriteTokens: row.UncostedCacheWriteTokens,
			TaskCount:                row.TaskCount,
		}
	}
	return resp, nil
}

// DashboardUsageByAgentResponse is one (agent, provider, model) row. provider
// rides along for the same cross-provider pricing disambiguation as the daily
// response; the client folds by agent_id and sums cost.
type DashboardUsageByAgentResponse struct {
	AgentID          string `json:"agent_id"`
	Provider         string `json:"provider"`
	Model            string `json:"model"`
	InputTokens      int64  `json:"input_tokens"`
	OutputTokens     int64  `json:"output_tokens"`
	CacheReadTokens  int64  `json:"cache_read_tokens"`
	CacheWriteTokens int64  `json:"cache_write_tokens"`
	// Cost split: `CostUSDTicks` is what the provider itself charged for the
	// rows behind this aggregate (1e-10 USD), and the `Uncosted*` token
	// counts are the tokens from rows the provider did NOT price. The client
	// reports authoritative + estimate(uncosted), so a window mixing both
	// kinds of row stays whole. See migration 213.
	CostUSDTicks             int64 `json:"cost_usd_ticks"`
	UncostedInputTokens      int64 `json:"uncosted_input_tokens"`
	UncostedOutputTokens     int64 `json:"uncosted_output_tokens"`
	UncostedCacheReadTokens  int64 `json:"uncosted_cache_read_tokens"`
	UncostedCacheWriteTokens int64 `json:"uncosted_cache_write_tokens"`
	TaskCount                int32 `json:"task_count"`
}

// GetDashboardUsageByAgent returns per-(agent, model) token aggregates
// for the workspace, optionally scoped to a project. Backed by
// task_usage_hourly with the viewer's tz applied to the `?days=` cutoff.
func (h *Handler) GetDashboardUsageByAgent(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	projectID, ok := parseProjectIDParam(w, r)
	if !ok {
		return
	}
	restricted, ok := h.dashboardRestrictedAgents(w, r, workspaceID, member.Role)
	if !ok {
		return
	}
	// "By agent" has no date grouping in the SQL — tz only determines
	// the cutoff boundary, not the bucket axis. Which is exactly why the
	// cutoff must be the EXACT N-day one: the client trims the surplus day
	// `parseSinceParamInTZ` hands back with `-(days-1)`, and a response
	// carrying no date cannot be trimmed that way. On the N+1 cutoff this
	// leaderboard covered one calendar day more than the Tokens/Cost KPI and
	// the chart directly above it, so at 1D a single agent's row could read
	// higher than the workspace total (MUL-5551).
	tz := h.resolveViewingTZ(r)
	since := parseExactSinceParamInTZ(r, 30, tz)

	resp, err := h.listDashboardUsageByAgent(r.Context(), parseUUID(workspaceID), since, projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list usage by agent")
		return
	}
	writeJSON(w, http.StatusOK, foldRestrictedUsageByAgent(resp, restricted))
}

// providerModelKey keeps the restricted bucket split by (provider, model) so
// the client can still price it from its per-model table — without that the
// bucket's cost is uncomputable and the leaderboard stops summing to the Cost
// KPI, which is the whole reason these rows are folded rather than dropped.
type providerModelKey struct{ provider, model string }

func foldRestrictedUsageByAgent(
	rows []DashboardUsageByAgentResponse,
	restricted map[string]struct{},
) []DashboardUsageByAgentResponse {
	return foldRestrictedAgents(
		rows,
		restricted,
		func(row DashboardUsageByAgentResponse) string { return row.AgentID },
		func(row DashboardUsageByAgentResponse) (DashboardUsageByAgentResponse, providerModelKey) {
			key := providerModelKey{provider: row.Provider, model: row.Model}
			row.AgentID = restrictedAgentsRowID
			return row, key
		},
		func(dst, src DashboardUsageByAgentResponse) DashboardUsageByAgentResponse {
			dst.InputTokens += src.InputTokens
			dst.OutputTokens += src.OutputTokens
			dst.CacheReadTokens += src.CacheReadTokens
			dst.CacheWriteTokens += src.CacheWriteTokens
			dst.CostUSDTicks += src.CostUSDTicks
			dst.UncostedInputTokens += src.UncostedInputTokens
			dst.UncostedOutputTokens += src.UncostedOutputTokens
			dst.UncostedCacheReadTokens += src.UncostedCacheReadTokens
			dst.UncostedCacheWriteTokens += src.UncostedCacheWriteTokens
			dst.TaskCount += src.TaskCount
			return dst
		},
	)
}

func (h *Handler) listDashboardUsageByAgent(
	ctx context.Context,
	workspaceID pgtype.UUID,
	since pgtype.Timestamptz,
	projectID pgtype.UUID,
) ([]DashboardUsageByAgentResponse, error) {
	rows, err := h.Queries.ListDashboardUsageByAgent(ctx, db.ListDashboardUsageByAgentParams{
		WorkspaceID: workspaceID,
		Since:       since,
		ProjectID:   projectID,
	})
	if err != nil {
		return nil, err
	}
	resp := make([]DashboardUsageByAgentResponse, len(rows))
	for i, row := range rows {
		resp[i] = DashboardUsageByAgentResponse{
			AgentID:                  uuidToString(row.AgentID),
			Provider:                 row.Provider,
			Model:                    row.Model,
			InputTokens:              row.InputTokens,
			OutputTokens:             row.OutputTokens,
			CacheReadTokens:          row.CacheReadTokens,
			CacheWriteTokens:         row.CacheWriteTokens,
			CostUSDTicks:             row.CostUsdTicks,
			UncostedInputTokens:      row.UncostedInputTokens,
			UncostedOutputTokens:     row.UncostedOutputTokens,
			UncostedCacheReadTokens:  row.UncostedCacheReadTokens,
			UncostedCacheWriteTokens: row.UncostedCacheWriteTokens,
			TaskCount:                row.TaskCount,
		}
	}
	return resp, nil
}

// DashboardAgentRunTimeResponse is one agent's total terminal-task run time
// over the window. Includes failed and cancelled tasks so the dashboard can
// surface how much execution time was spent on runs that didn't succeed.
// FailedCount and CancelledCount are disjoint subsets of TaskCount; the
// client derives the succeeded count as the remainder.
type DashboardAgentRunTimeResponse struct {
	AgentID        string `json:"agent_id"`
	TotalSeconds   int64  `json:"total_seconds"`
	TaskCount      int32  `json:"task_count"`
	FailedCount    int32  `json:"failed_count"`
	CancelledCount int32  `json:"cancelled_count"`
}

// GetDashboardAgentRunTime returns per-agent total task run time (seconds)
// and task counts for the workspace, optionally scoped to a project. Only
// terminal tasks (completed, failed, or cancelled) with both started_at and
// completed_at populated contribute, since queued/running tasks have no
// finite duration.
func (h *Handler) GetDashboardAgentRunTime(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	projectID, ok := parseProjectIDParam(w, r)
	if !ok {
		return
	}
	restricted, ok := h.dashboardRestrictedAgents(w, r, workspaceID, member.Role)
	if !ok {
		return
	}
	// Cutoff in the viewer's tz so the "last N days" window matches the
	// per-agent cost card (GetDashboardUsageByAgent). Exact N-day cutoff for
	// the same reason: these rows carry no date, so the client cannot trim
	// the extra calendar day `parseSinceParamInTZ` returns. This response
	// feeds BOTH the leaderboard's Time/Tasks columns and the Run time /
	// Tasks KPI tiles, so the N+1 cutoff put those two tiles on a wider
	// window than the Cost / Tokens tiles beside them (MUL-5551).
	tz := h.resolveViewingTZ(r)
	since := parseExactSinceParamInTZ(r, 30, tz)

	rows, err := h.Queries.ListDashboardAgentRunTime(r.Context(), db.ListDashboardAgentRunTimeParams{
		WorkspaceID: parseUUID(workspaceID),
		Since:       since,
		ProjectID:   projectID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agent runtime")
		return
	}

	resp := make([]DashboardAgentRunTimeResponse, len(rows))
	for i, row := range rows {
		resp[i] = DashboardAgentRunTimeResponse{
			AgentID:        uuidToString(row.AgentID),
			TotalSeconds:   row.TotalSeconds,
			TaskCount:      row.TaskCount,
			FailedCount:    row.FailedCount,
			CancelledCount: row.CancelledCount,
		}
	}
	writeJSON(w, http.StatusOK, foldRestrictedAgentRunTime(resp, restricted))
}

// The run-time row carries no dimension besides the agent, so every restricted
// row merges into a single bucket — hence the empty merge key.
func foldRestrictedAgentRunTime(
	rows []DashboardAgentRunTimeResponse,
	restricted map[string]struct{},
) []DashboardAgentRunTimeResponse {
	return foldRestrictedAgents(
		rows,
		restricted,
		func(row DashboardAgentRunTimeResponse) string { return row.AgentID },
		func(row DashboardAgentRunTimeResponse) (DashboardAgentRunTimeResponse, struct{}) {
			row.AgentID = restrictedAgentsRowID
			return row, struct{}{}
		},
		func(dst, src DashboardAgentRunTimeResponse) DashboardAgentRunTimeResponse {
			dst.TotalSeconds += src.TotalSeconds
			dst.TaskCount += src.TaskCount
			dst.FailedCount += src.FailedCount
			dst.CancelledCount += src.CancelledCount
			return dst
		},
	)
}

// DashboardRunTimeDailyResponse is one (date) bucket of terminal-task run
// time and counts. Powers the workspace dashboard's daily Time and Tasks
// charts — same toggle as Tokens / Cost, different metric. FailedCount and
// CancelledCount are disjoint subsets of TaskCount.
type DashboardRunTimeDailyResponse struct {
	Date           string `json:"date"`
	TotalSeconds   int64  `json:"total_seconds"`
	TaskCount      int32  `json:"task_count"`
	FailedCount    int32  `json:"failed_count"`
	CancelledCount int32  `json:"cancelled_count"`
}

// GetDashboardRunTimeDaily returns per-date total task run time and task
// counts for the workspace, optionally scoped to a project. Only terminal
// tasks (completed, failed, or cancelled) with both started_at and
// completed_at populated contribute. Bucketed by completed_at so the day
// boundaries line up with the per-agent run-time card.
func (h *Handler) GetDashboardRunTimeDaily(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	projectID, ok := parseProjectIDParam(w, r)
	if !ok {
		return
	}
	// Slice day buckets in the viewer's tz so the Time / Tasks charts cut
	// their calendar day identically to the Cost / Tokens charts.
	tz := h.resolveViewingTZ(r)
	since := parseSinceParamInTZ(r, 30, tz)

	rows, err := h.Queries.ListDashboardRunTimeDaily(r.Context(), db.ListDashboardRunTimeDailyParams{
		WorkspaceID: parseUUID(workspaceID),
		Tz:          tz,
		Since:       since,
		ProjectID:   projectID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list daily runtime")
		return
	}

	resp := make([]DashboardRunTimeDailyResponse, len(rows))
	for i, row := range rows {
		resp[i] = DashboardRunTimeDailyResponse{
			Date:           row.Date.Time.Format("2006-01-02"),
			TotalSeconds:   row.TotalSeconds,
			TaskCount:      row.TaskCount,
			FailedCount:    row.FailedCount,
			CancelledCount: row.CancelledCount,
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// ---------------------------------------------------------------------------
// Failure rollups
//
// Both endpoints return EVERY terminal task, not just the failed ones: the
// row whose FailureReason is "" carries that bucket's succeeded count. The
// client needs that denominator to render an error *rate*, and shipping it
// in the same payload keeps numerator and denominator on identical filters —
// deriving the denominator from the run-time endpoints instead would silently
// disagree, because those require started_at IS NOT NULL and a task that
// expired in the queue never started.
//
// FailureReason values are the canonical taxonomy from server/pkg/taskfailure
// (21 reasons), plus "unclassified" for failed rows with a NULL / empty
// column. The client folds them into a handful of display classes; the raw
// reason stays on the wire so that mapping can change without a backend
// deploy.
// ---------------------------------------------------------------------------

// DashboardFailureDailyResponse is one (date, failure_reason) bucket of
// terminal-task counts. FailureReason == "" is the succeeded bucket.
type DashboardFailureDailyResponse struct {
	Date          string `json:"date"`
	FailureReason string `json:"failure_reason"`
	TaskCount     int32  `json:"task_count"`
}

// GetDashboardFailuresDaily returns per-(date, failure_reason) terminal-task
// counts for the workspace, optionally scoped to a project. Powers the Usage
// page's Errors trend and errors-by-class breakdown.
func (h *Handler) GetDashboardFailuresDaily(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	projectID, ok := parseProjectIDParam(w, r)
	if !ok {
		return
	}
	// Same viewer-tz day boundary as every other daily series so the Errors
	// tab lines up with Cost / Tokens / Time / Tasks.
	tz := h.resolveViewingTZ(r)
	since := parseSinceParamInTZ(r, 30, tz)

	rows, err := h.Queries.ListDashboardFailuresDaily(r.Context(), db.ListDashboardFailuresDailyParams{
		WorkspaceID: parseUUID(workspaceID),
		Tz:          tz,
		Since:       since,
		ProjectID:   projectID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list daily failures")
		return
	}

	resp := make([]DashboardFailureDailyResponse, len(rows))
	for i, row := range rows {
		resp[i] = DashboardFailureDailyResponse{
			Date:          row.Date.Time.Format("2006-01-02"),
			FailureReason: row.FailureReason,
			TaskCount:     row.TaskCount,
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// DashboardFailureByAgentResponse is one (agent, failure_reason) bucket of
// terminal-task counts. FailureReason == "" is the succeeded bucket.
type DashboardFailureByAgentResponse struct {
	AgentID       string `json:"agent_id"`
	FailureReason string `json:"failure_reason"`
	TaskCount     int32  `json:"task_count"`
}

// GetDashboardFailuresByAgent returns per-(agent, failure_reason)
// terminal-task counts for the workspace, optionally scoped to a project.
// Powers the Usage page's "top offenders" list.
func (h *Handler) GetDashboardFailuresByAgent(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	projectID, ok := parseProjectIDParam(w, r)
	if !ok {
		return
	}
	restricted, ok := h.dashboardRestrictedAgents(w, r, workspaceID, member.Role)
	if !ok {
		return
	}
	// No date grouping in the SQL, so the client cannot trim this response the
	// way it trims the date-bucketed series. Close the window server-side to
	// exactly `days` calendar buckets — the same span the Errors chart renders
	// after its `-(days-1)` filter. With the default N+1 cutoff this list
	// covered one extra day, so at days=1 the card could report yesterday's
	// failures next to a chart showing none.
	tz := h.resolveViewingTZ(r)
	since := parseExactSinceParamInTZ(r, 30, tz)

	rows, err := h.Queries.ListDashboardFailuresByAgent(r.Context(), db.ListDashboardFailuresByAgentParams{
		WorkspaceID: parseUUID(workspaceID),
		Since:       since,
		ProjectID:   projectID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list failures by agent")
		return
	}

	resp := make([]DashboardFailureByAgentResponse, len(rows))
	for i, row := range rows {
		resp[i] = DashboardFailureByAgentResponse{
			AgentID:       uuidToString(row.AgentID),
			FailureReason: row.FailureReason,
			TaskCount:     row.TaskCount,
		}
	}
	writeJSON(w, http.StatusOK, foldRestrictedFailuresByAgent(resp, restricted))
}

// The restricted bucket keeps its failure_reason split: the client derives the
// bucket's failure rate and class mix from these raw rows exactly like any
// other agent's, and the succeeded rows (failure_reason == "") are the
// denominator that keeps the offender list reconciling with the workspace
// failure total above it.
func foldRestrictedFailuresByAgent(
	rows []DashboardFailureByAgentResponse,
	restricted map[string]struct{},
) []DashboardFailureByAgentResponse {
	return foldRestrictedAgents(
		rows,
		restricted,
		func(row DashboardFailureByAgentResponse) string { return row.AgentID },
		func(row DashboardFailureByAgentResponse) (DashboardFailureByAgentResponse, string) {
			reason := row.FailureReason
			row.AgentID = restrictedAgentsRowID
			return row, reason
		},
		func(dst, src DashboardFailureByAgentResponse) DashboardFailureByAgentResponse {
			dst.TaskCount += src.TaskCount
			return dst
		},
	)
}

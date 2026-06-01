// CEREBRO-PATCH(search-fts-fir-2595): FIR-2595 search trin 1.
//
// Net-new fork file. Replaces the LIKE-based SearchIssues with a Postgres
// FTS + pg_trgm pipeline, plus Slack-style inline filters (from:/assignee:/
// status:/project:/has:). Parsing + SQL-build live in the cerebro/search
// package; this file is the HTTP shell that resolves filter values to UUIDs
// and applies access control.
package handler

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	cerebrosearch "github.com/multica-ai/multica/server/internal/cerebro/search"
	"github.com/multica-ai/multica/server/internal/util"
)

// ActiveFilter is echoed back in the response so the UI can render chips and
// show what the backend actually applied (after resolving names to IDs).
// Match is "ok" when at least one row matched the value, "miss" when the
// filter resolved to zero IDs (so the user sees "no results" with a hint).
type ActiveFilter struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Match string `json:"match"`
}

// SearchIssuesResponseEnvelope mirrors the upstream issues+total payload and
// adds the parsed/resolved filter list so the frontend can render chips.
type SearchIssuesResponseEnvelope struct {
	Issues  []SearchIssueResponse `json:"issues"`
	Total   int64                 `json:"total"`
	Filters []ActiveFilter        `json:"filters,omitempty"`
}

// SearchIssuesCerebro is the FTS-powered replacement for h.SearchIssues. The
// upstream entry point delegates here via a CEREBRO-PATCH redirect.
func (h *Handler) SearchIssuesCerebro(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workspaceID := h.resolveWorkspaceID(r)
	q := r.URL.Query()

	rawQ := q.Get("q")
	if rawQ == "" {
		writeError(w, http.StatusBadRequest, "q parameter is required")
		return
	}

	limit := 20
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 50 {
		limit = 50
	}
	offset := 0
	if v := q.Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	includeClosed := q.Get("include_closed") == "true"

	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	parsed := cerebrosearch.Parse(rawQ)
	queryNum, hasNum := parseQueryNumber(parsed.Text)

	in := cerebrosearch.QueryInput{
		WorkspaceID:   wsUUID,
		Text:          parsed.Text,
		QueryNumber:   queryNum,
		HasNumber:     hasNum,
		IncludeClosed: includeClosed,
		Limit:         limit,
		Offset:        offset,
	}

	activeFilters := make([]ActiveFilter, 0, len(parsed.Filters))

	// --- status: ---
	if values := parsed.Values(cerebrosearch.FilterStatus); len(values) > 0 {
		// Many statuses may be supplied; expand each via StatusMap and dedupe.
		seen := map[string]bool{}
		for _, v := range values {
			expanded := cerebrosearch.StatusMap(v)
			match := "miss"
			if len(expanded) > 0 {
				match = "ok"
			}
			activeFilters = append(activeFilters, ActiveFilter{Key: "status", Value: v, Match: match})
			for _, s := range expanded {
				if !seen[s] {
					seen[s] = true
					in.StatusValues = append(in.StatusValues, s)
				}
			}
		}
	}

	// --- from: ---
	for _, v := range parsed.Values(cerebrosearch.FilterFrom) {
		users, agents := h.cerebroResolveActorName(ctx, wsUUID, v)
		match := "miss"
		if len(users) > 0 || len(agents) > 0 {
			match = "ok"
		}
		activeFilters = append(activeFilters, ActiveFilter{Key: "from", Value: v, Match: match})
		for _, u := range users {
			in.FromUserIDs = append(in.FromUserIDs, u)
		}
		for _, a := range agents {
			in.FromAgentIDs = append(in.FromAgentIDs, a)
		}
	}

	// --- assignee: ---
	for _, v := range parsed.Values(cerebrosearch.FilterAssignee) {
		lv := strings.ToLower(strings.TrimSpace(v))
		if lv == "none" || lv == "unassigned" || lv == "nobody" {
			in.AssigneeUnassigned = true
			activeFilters = append(activeFilters, ActiveFilter{Key: "assignee", Value: v, Match: "ok"})
			continue
		}
		if lv == "me" {
			if userID := requestUserID(r); userID != "" {
				if uid, err := util.ParseUUID(userID); err == nil {
					in.AssigneeUserIDs = append(in.AssigneeUserIDs, uid)
					activeFilters = append(activeFilters, ActiveFilter{Key: "assignee", Value: v, Match: "ok"})
					continue
				}
			}
		}
		users, agents := h.cerebroResolveActorName(ctx, wsUUID, v)
		match := "miss"
		if len(users) > 0 || len(agents) > 0 {
			match = "ok"
		}
		activeFilters = append(activeFilters, ActiveFilter{Key: "assignee", Value: v, Match: match})
		for _, u := range users {
			in.AssigneeUserIDs = append(in.AssigneeUserIDs, u)
		}
		for _, a := range agents {
			in.AssigneeAgentIDs = append(in.AssigneeAgentIDs, a)
		}
	}

	// --- project: ---
	for _, v := range parsed.Values(cerebrosearch.FilterProject) {
		projects := h.cerebroResolveProjectName(ctx, wsUUID, v)
		match := "miss"
		if len(projects) > 0 {
			match = "ok"
		}
		activeFilters = append(activeFilters, ActiveFilter{Key: "project", Value: v, Match: match})
		for _, p := range projects {
			in.ProjectIDs = append(in.ProjectIDs, p)
		}
	}

	// --- has: ---
	for _, v := range parsed.Values(cerebrosearch.FilterHas) {
		if strings.EqualFold(strings.TrimSpace(v), "attachment") {
			in.HasAttachment = true
			activeFilters = append(activeFilters, ActiveFilter{Key: "has", Value: "attachment", Match: "ok"})
		} else {
			// Unknown has:<x> — echo back so the UI can render a "miss" chip.
			activeFilters = append(activeFilters, ActiveFilter{Key: "has", Value: v, Match: "miss"})
		}
	}

	// A miss on any filter that COULD match (i.e. the user picked a real
	// filter name but the value resolved nowhere) collapses the result set
	// to empty. Detect and short-circuit before issuing SQL.
	if anyFilterMissNarrowed(parsed, activeFilters) {
		writeFilteredEmptyResponse(w, activeFilters)
		return
	}

	// CEREBRO-PATCH(search-hybrid-fir-2604): FIR-2604 hybrid (FTS + vector)
	// retrieval. Opt in when free text is present, the operator wired a
	// semantic provider, and the embedding store is reachable; otherwise
	// fall back to Build — same behaviour as Del 1.
	var query cerebrosearch.Query
	if in.Text != "" && h.SemanticSearch != nil {
		if literal, ok := h.SemanticSearch.QueryVectorLiteral(ctx, in.Text); ok {
			query = cerebrosearch.BuildHybrid(hybridInputFor(in, literal))
		}
	}
	if query.SQL == "" {
		query = cerebrosearch.Build(in)
	}
	rows, err := h.DB.Query(ctx, query.SQL, query.Args...)
	if err != nil {
		slog.Warn("cerebro search failed", "error", err, "workspace_id", workspaceID, "q", rawQ)
		writeError(w, http.StatusInternalServerError, "failed to search issues")
		return
	}
	defer rows.Close()

	var results []searchResult
	for rows.Next() {
		var sr searchResult
		if err := rows.Scan(
			&sr.issue.ID,
			&sr.issue.WorkspaceID,
			&sr.issue.Title,
			&sr.issue.Description,
			&sr.issue.Status,
			&sr.issue.Priority,
			&sr.issue.AssigneeType,
			&sr.issue.AssigneeID,
			&sr.issue.CreatorType,
			&sr.issue.CreatorID,
			&sr.issue.ParentIssueID,
			&sr.issue.AcceptanceCriteria,
			&sr.issue.ContextRefs,
			&sr.issue.Position,
			&sr.issue.StartDate,
			&sr.issue.DueDate,
			&sr.issue.CreatedAt,
			&sr.issue.UpdatedAt,
			&sr.issue.Number,
			&sr.issue.ProjectID,
			&sr.totalCount,
			&sr.matchSource,
			&sr.matchedCommentContent,
		); err != nil {
			slog.Warn("cerebro search scan failed", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to search issues")
			return
		}
		results = append(results, sr)
	}
	if err := rows.Err(); err != nil {
		slog.Warn("cerebro search rows error", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to search issues")
		return
	}

	// Access control: same rule as the upstream LIKE path. A user who can't
	// access an issue must never know it matched their search, so we drop
	// the row entirely rather than redact.
	if member, hasMember := ctxMember(ctx); hasMember && !isWorkspaceAdmin(member) {
		filtered := results[:0]
		for _, sr := range results {
			if h.canAccessIssue(ctx, member, sr.issue) {
				filtered = append(filtered, sr)
			}
		}
		results = filtered
	}

	var total int64
	if len(results) > 0 {
		total = results[0].totalCount
	}

	prefix := h.getIssuePrefix(ctx, wsUUID)
	resp := make([]SearchIssueResponse, len(results))
	for i, sr := range results {
		sir := SearchIssueResponse{
			IssueResponse: issueToResponse(sr.issue, prefix),
			MatchSource:   sr.matchSource,
		}
		if sr.matchedCommentContent != "" {
			snippet := extractSnippet(sr.matchedCommentContent, parsed.Text)
			sir.MatchedCommentSnippet = &snippet
			if sr.matchSource == "comment" {
				sir.MatchedSnippet = &snippet
			}
		}
		if sr.matchSource == "description" || descriptionContains(sr.issue.Description, parsed.Text, strings.Fields(parsed.Text)) {
			if sr.issue.Description.Valid && sr.issue.Description.String != "" {
				snippet := extractSnippet(sr.issue.Description.String, parsed.Text)
				sir.MatchedDescriptionSnippet = &snippet
			}
		}
		resp[i] = sir
	}

	w.Header().Set("X-Total-Count", strconv.FormatInt(total, 10))
	writeJSON(w, http.StatusOK, SearchIssuesResponseEnvelope{
		Issues:  resp,
		Total:   total,
		Filters: activeFilters,
	})
}

// anyFilterMissNarrowed returns true when at least one filter category was
// supplied with values that ALL resolved to "miss" (no member/agent/project
// matched). In that case the result set is guaranteed empty regardless of
// free text, so we short-circuit instead of issuing a no-op SQL query.
func anyFilterMissNarrowed(parsed cerebrosearch.Parsed, active []ActiveFilter) bool {
	for _, key := range []cerebrosearch.FilterKey{
		cerebrosearch.FilterFrom,
		cerebrosearch.FilterAssignee,
		cerebrosearch.FilterProject,
		cerebrosearch.FilterStatus,
		// CEREBRO-PATCH(search-has-miss-narrows): has:<invalid> must narrow to empty like other filter misses, not return all rows (FIR-2571)
		cerebrosearch.FilterHas,
	} {
		raw := parsed.Values(key)
		if len(raw) == 0 {
			continue
		}
		anyOK := false
		for _, af := range active {
			if af.Key == string(key) && af.Match == "ok" {
				anyOK = true
				break
			}
		}
		if !anyOK {
			return true
		}
	}
	return false
}

func writeFilteredEmptyResponse(w http.ResponseWriter, active []ActiveFilter) {
	w.Header().Set("X-Total-Count", "0")
	writeJSON(w, http.StatusOK, SearchIssuesResponseEnvelope{
		Issues:  []SearchIssueResponse{},
		Total:   0,
		Filters: active,
	})
}

// cerebroResolveActorName resolves a `from:` / `assignee:` value into the
// set of (member user_ids, agent ids) that match the name fragment. The
// match is case-insensitive prefix-or-contains so both `from:jesper` and
// `from:hvejsel` work against the same person.
//
// We resolve in Go instead of inlining the lookup as a subquery so a
// missing name produces a friendly "miss" chip rather than a query that
// returns zero rows with no explanation.
func (h *Handler) cerebroResolveActorName(ctx context.Context, wsUUID pgtype.UUID, name string) ([]pgtype.UUID, []pgtype.UUID) {
	v := strings.ToLower(strings.TrimSpace(name))
	if v == "" {
		return nil, nil
	}
	// pgx requires us to handle the LIKE pattern in Go.
	prefix := v + "%"
	contains := "%" + v + "%"

	var users []pgtype.UUID
	const memberSQL = `SELECT m.user_id
		FROM member m
		JOIN "user" u ON u.id = m.user_id
		WHERE m.workspace_id = $1
		  AND (lower(u.name) LIKE $2
		       OR lower(u.email) LIKE $2
		       OR lower(u.name) LIKE $3)
		LIMIT 25`
	if rows, err := h.DB.Query(ctx, memberSQL, wsUUID, prefix, contains); err == nil {
		defer rows.Close()
		for rows.Next() {
			var uid pgtype.UUID
			if err := rows.Scan(&uid); err == nil {
				users = append(users, uid)
			}
		}
	}

	var agents []pgtype.UUID
	const agentSQL = `SELECT id FROM agent
		WHERE workspace_id = $1
		  AND (lower(name) LIKE $2 OR lower(name) LIKE $3)
		LIMIT 25`
	if rows, err := h.DB.Query(ctx, agentSQL, wsUUID, prefix, contains); err == nil {
		defer rows.Close()
		for rows.Next() {
			var aid pgtype.UUID
			if err := rows.Scan(&aid); err == nil {
				agents = append(agents, aid)
			}
		}
	}
	return users, agents
}

// cerebroResolveProjectName matches `project:<value>` against project.title
// (prefix-or-contains, case-insensitive). Slug-style values (project:lager)
// are caught by the prefix branch; multi-word values (project:"my project")
// fall back to contains.
func (h *Handler) cerebroResolveProjectName(ctx context.Context, wsUUID pgtype.UUID, name string) []pgtype.UUID {
	v := strings.ToLower(strings.TrimSpace(name))
	if v == "" {
		return nil
	}
	prefix := v + "%"
	contains := "%" + v + "%"

	var out []pgtype.UUID
	const sql = `SELECT id FROM project
		WHERE workspace_id = $1
		  AND (lower(title) LIKE $2 OR lower(title) LIKE $3)
		LIMIT 25`
	if rows, err := h.DB.Query(ctx, sql, wsUUID, prefix, contains); err == nil {
		defer rows.Close()
		for rows.Next() {
			var pid pgtype.UUID
			if err := rows.Scan(&pid); err == nil {
				out = append(out, pid)
			}
		}
	}
	return out
}

// CEREBRO-PATCH(duplicate-check-handler): FIR-2504. Net-new fork file —
// powers the "Findes det her allerede?" panel on issue creation.
//
// The endpoint takes the draft title + description, runs the existing
// SearchIssues SQL helpers against the same workspace to find the top
// candidates (title + description + comment match), then asks Haiku via the
// Firtal AI Gateway to classify each candidate. The frontend shows the top 3
// actionable verdicts (duplicate / related) so the user can open the
// existing issue or create the new one as a sub-issue.
//
// The LLM step is best-effort: if the gateway is unreachable or the request
// is malformed the endpoint returns an empty match list rather than a 5xx,
// so a flaky gateway never blocks issue creation.
package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/duplicatecheck"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	// dupCheckCandidatePoolSize caps the candidate-finder result set fed to
	// Haiku. 10 is enough to cover the same-pipeline cluster the PRD calls
	// out (~16 hevo issues, but the search ranking surfaces the closest
	// first); going wider just inflates the prompt without improving
	// precision.
	dupCheckCandidatePoolSize = 10
	// dupCheckResponseLimit is the number of actionable verdicts (duplicate
	// or related) returned to the panel. The PRD scope is "top 3 matches".
	dupCheckResponseLimit = 3
	// dupCheckMinQueryRunes is the floor on the combined draft text length
	// before we even attempt a search. Below this the SQL returns too much
	// noise and the LLM call is wasted.
	dupCheckMinQueryRunes = 6
)

// dupCheckRequest is the body of POST /api/cerebro/issues/check-similar.
// ProjectID is optional — when present we add it as an extra ranking hint
// in the candidate finder (same-project matches surface first), but the
// search is workspace-wide so cross-project duplicates still appear.
type dupCheckRequest struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	ProjectID   string `json:"project_id,omitempty"`
}

// dupCheckMatch is one match card shown in the create-issue panel. Fields
// mirror IssueResponse where possible so the frontend can reuse status /
// priority / assignee renderers. The verdict + reason come from the LLM.
type dupCheckMatch struct {
	ID         string `json:"id"`
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
	Status     string `json:"status"`
	URL        string `json:"url"`
	Verdict    string `json:"verdict"`
	Reason     string `json:"reason"`
}

type dupCheckResponse struct {
	Matches []dupCheckMatch `json:"matches"`
}

// CheckSimilarIssues is the HTTP handler. The judge dependency is read off
// the Handler struct so tests can inject a fake (see field declaration in
// handler.go).
func (h *Handler) CheckSimilarIssues(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	var req dupCheckRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	title := strings.TrimSpace(req.Title)
	desc := strings.TrimSpace(req.Description)

	// Combine title + first chunk of description into a single search
	// phrase. Title alone is too thin; whole-description is too noisy. ~600
	// runes covers the meaningful first paragraph in practice.
	phrase := title
	if desc != "" {
		phrase = title + " " + truncateRunesForSearch(desc, 600)
	}
	phrase = strings.TrimSpace(phrase)
	if runeLen(phrase) < dupCheckMinQueryRunes {
		writeJSON(w, http.StatusOK, dupCheckResponse{Matches: nil})
		return
	}

	candidates := h.findDupCheckCandidates(ctx, wsUUID, phrase)
	if len(candidates) == 0 {
		writeJSON(w, http.StatusOK, dupCheckResponse{Matches: nil})
		return
	}

	// Defensive: only members who can actually see the candidate get to
	// have it ranked. Reuses the access predicate that powers SearchIssues
	// so a non-admin never sees a candidate that would otherwise be hidden.
	member, hasMember := ctxMember(ctx)
	if hasMember && !isWorkspaceAdmin(member) {
		filtered := candidates[:0]
		for _, c := range candidates {
			if h.canAccessIssue(ctx, member, c) {
				filtered = append(filtered, c)
			}
		}
		candidates = filtered
	}
	if len(candidates) == 0 {
		writeJSON(w, http.StatusOK, dupCheckResponse{Matches: nil})
		return
	}

	prefix := h.getIssuePrefix(ctx, wsUUID)
	candidateByID := make(map[string]db.Issue, len(candidates))
	llmCandidates := make([]duplicatecheck.Candidate, 0, len(candidates))
	for _, c := range candidates {
		id := uuidToString(c.ID)
		candidateByID[id] = c
		llmCandidates = append(llmCandidates, duplicatecheck.Candidate{
			ID:          id,
			Identifier:  formatIssueIdentifier(prefix, c.Number),
			Title:       c.Title,
			Description: truncateForLLM(c.Description, 240),
		})
	}

	judger := h.duplicateCheckJudger()
	verdicts := judger.Judge(ctx, duplicatecheck.Draft{Title: title, Description: desc}, llmCandidates)

	matches := make([]dupCheckMatch, 0, dupCheckResponseLimit)
	for _, v := range verdicts {
		if !v.Verdict.IsActionable() {
			continue
		}
		issue, ok := candidateByID[v.ID]
		if !ok {
			continue
		}
		matches = append(matches, dupCheckMatch{
			ID:         v.ID,
			Identifier: formatIssueIdentifier(prefix, issue.Number),
			Title:      issue.Title,
			Status:     issue.Status,
			URL:        "/issues/" + v.ID,
			Verdict:    string(v.Verdict),
			Reason:     v.Reason,
		})
		if len(matches) >= dupCheckResponseLimit {
			break
		}
	}

	// Adoption telemetry (FIR-2504): structured slog so we can compute "% of
	// drafts shown a match that result in open / attach" by grepping logs. No
	// DB persistence in v1 — the PRD-loft target (20%) is coarse enough that
	// log aggregation is enough. Combined with the dup_check.event entries
	// (see DupCheckEvent below) this gives the full funnel.
	memberID := ""
	if hasMember {
		memberID = uuidToString(member.ID)
	}
	slog.Info("dup_check.shown",
		"workspace_id", uuidToString(wsUUID),
		"member_id", memberID,
		"candidate_count", len(candidates),
		"match_count", len(matches),
		"draft_title_len", runeLen(title),
	)

	writeJSON(w, http.StatusOK, dupCheckResponse{Matches: matches})
}

// dupCheckEventRequest is the body of POST /api/cerebro/issues/check-similar/event.
// The panel posts one event per user action (opened existing / attached as
// sub-issue / dismissed). The endpoint is fire-and-forget: it logs and
// returns 204; the frontend ignores the response so a slow log write never
// blocks the create flow.
type dupCheckEventRequest struct {
	Action     string `json:"action"`
	MatchID    string `json:"match_id,omitempty"`
	Verdict    string `json:"verdict,omitempty"`
	MatchCount int    `json:"match_count,omitempty"`
}

// DupCheckEvent records the user's choice when a match panel was shown. Used
// to compute the PRD-loft adoption metric: of the drafts shown ≥1 match, what
// fraction chose "open existing" or "attach as sub-issue" over "create new".
func (h *Handler) DupCheckEvent(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	var req dupCheckEventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	action := strings.ToLower(strings.TrimSpace(req.Action))
	switch action {
	case "opened", "attached", "dismissed", "created_anyway":
		// accepted
	default:
		writeError(w, http.StatusBadRequest, "unknown action")
		return
	}
	memberID := ""
	if member, hasMember := ctxMember(r.Context()); hasMember {
		memberID = uuidToString(member.ID)
	}
	slog.Info("dup_check.event",
		"workspace_id", uuidToString(wsUUID),
		"member_id", memberID,
		"action", action,
		"match_id", req.MatchID,
		"verdict", req.Verdict,
		"match_count", req.MatchCount,
	)
	w.WriteHeader(http.StatusNoContent)
}

// findDupCheckCandidates runs the existing search SQL against the workspace
// with includeClosed=false and dupCheckCandidatePoolSize as the cap. We
// reuse buildSearchQuery so any future ranking improvements (e.g. switching
// to pg_bigm trigrams) immediately benefit the duplicate-check too.
func (h *Handler) findDupCheckCandidates(ctx context.Context, wsUUID pgtype.UUID, phrase string) []db.Issue {
	terms := splitSearchTerms(phrase)
	queryNum, hasNum := parseQueryNumber(phrase)
	sqlQuery, args := buildSearchQuery(phrase, terms, queryNum, hasNum, false)
	args[3] = wsUUID
	args[len(args)-2] = dupCheckCandidatePoolSize
	args[len(args)-1] = 0

	rows, err := h.DB.Query(ctx, sqlQuery, args...)
	if err != nil {
		slog.Warn("duplicate-check candidate query failed", "error", err)
		return nil
	}
	defer rows.Close()

	var out []db.Issue
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
			slog.Warn("duplicate-check candidate scan failed", "error", err)
			return nil
		}
		out = append(out, sr.issue)
	}
	if err := rows.Err(); err != nil {
		slog.Warn("duplicate-check candidate rows error", "error", err)
		return nil
	}
	return out
}

// duplicateCheckJudger returns the configured judge. If the Handler-level
// override hook is nil (default) we build a zero-Judger so it reads the
// gateway credentials off env each call — same pattern agent_avatar uses.
func (h *Handler) duplicateCheckJudger() *duplicatecheck.Judger {
	if h.DuplicateCheckJudger != nil {
		return h.DuplicateCheckJudger
	}
	return &duplicatecheck.Judger{}
}

// formatIssueIdentifier renders e.g. "MUL-123". Mirrors the prefix logic in
// issueToResponse — kept inline so the duplicate-check response doesn't pay
// for the full IssueResponse marshalling on the candidate path.
func formatIssueIdentifier(prefix string, number int32) string {
	if prefix == "" {
		return ""
	}
	return prefix + "-" + intToString(int64(number))
}

func intToString(n int64) string {
	// strconv would pull a new import only for this; the upstream package
	// already vendors strings extensively.
	var sb strings.Builder
	if n == 0 {
		return "0"
	}
	var digits [20]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	sb.Write(digits[i:])
	return sb.String()
}

// truncateRunesForSearch caps phrase length while keeping multi-byte chars
// intact. The search query lowercases internally, so we don't bother here.
func truncateRunesForSearch(s string, max int) string {
	rs := []rune(s)
	if len(rs) <= max {
		return s
	}
	return string(rs[:max])
}

// truncateForLLM converts pgtype.Text + caps length for the gateway prompt.
func truncateForLLM(t pgtype.Text, max int) string {
	if !t.Valid {
		return ""
	}
	rs := []rune(t.String)
	if len(rs) <= max {
		return t.String
	}
	return string(rs[:max]) + "…"
}

func runeLen(s string) int { return len([]rune(s)) }

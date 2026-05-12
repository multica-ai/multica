package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	teamgate "github.com/multica-ai/multica/server/internal/multica"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Strict status-change lockdown (2026-04-29).
//
// Status transitions on issues are centrally gated by
// `checkStatusFlipPolicy`. The contract is:
//
//   * Member callers (humans on the web/desktop UI) may only flip status
//     `backlog -> todo` and `todo -> planning`. All other flips return 403
//     and the human is told to leave a comment mentioning the agent.
//   * The BMAD sidecar (agent name `bmad-sidecar`) is the privileged actor —
//     it parses contract markers and routes cards on agents' behalf, so it
//     is fully exempt and may flip any status to any status.
//   * All other agents (Amelia, Murat, Quinn, Winston, Felix, the PR
//     Manager, plus any future or non-BMAD agents) are blocked from direct
//     status flips with NO exemptions. Every agent transition flows through
//     a marker comment that the sidecar parses (including the post-merge
//     no-op short-circuit, which now uses `<!-- post-merge-noop -->`
//     instead of the legacy `bmad-agent-dev → staged` direct-flip
//     exemption that was retired in this commit).
//   * `CreateIssue` always lands new cards in `backlog`, regardless of the
//     `status` field on the request. Movement out of `backlog` happens via
//     the rules above.
//
// This collapses the older marker-only allowlist (which only covered a
// subset of BMAD agents on `UpdateIssue`) into a single source of truth
// applied across `UpdateIssue`, `BatchUpdateIssues`, and `CreateIssue`.

// sidecarAgentName is the agent name reserved for the BMAD sidecar. The
// sidecar is the only actor permitted to perform unrestricted status flips
// on agents' behalf.
const sidecarAgentName = "bmad-sidecar"

// memberAllowedStatusTransitions maps an `from -> set of permitted to`
// statuses for member (human) callers. Anything not present here is denied.
var memberAllowedStatusTransitions = map[string]map[string]struct{}{
	"backlog": {"todo": {}},
	"todo":    {"planning": {}},
}

// memberMayFlipStatus reports whether a member caller is permitted to flip
// an issue from `prev` to `next`. A no-op flip (prev == next) is always
// allowed. Inputs are matched case-insensitively after trimming whitespace.
func memberMayFlipStatus(prev, next string) bool {
	prev = strings.ToLower(strings.TrimSpace(prev))
	next = strings.ToLower(strings.TrimSpace(next))
	if prev == next {
		return true
	}
	allowed, ok := memberAllowedStatusTransitions[prev]
	if !ok {
		return false
	}
	_, ok = allowed[next]
	return ok
}

// agentMayFlipStatus reports whether the given agent is permitted to flip
// an issue from `prev` to `next`. Only the BMAD sidecar may flip status
// on agents' behalf — all other agents are blocked, with no exemptions.
// A no-op flip (prev == next) is always allowed (callers occasionally
// emit unchanged-status PATCHes on unrelated field updates and we don't
// want those to 403).
func agentMayFlipStatus(agentName, prev, next string) bool {
	name := strings.ToLower(strings.TrimSpace(agentName))
	if name == sidecarAgentName {
		return true
	}
	if strings.ToLower(strings.TrimSpace(prev)) == strings.ToLower(strings.TrimSpace(next)) {
		return true
	}
	return false
}

// statusFlipForbiddenMessage builds the user-facing 403 error string. The
// `actorType` is either "member" or "agent"; for agents `actorName` carries
// the agent name, ignored for members.
func statusFlipForbiddenMessage(actorType, actorName, prev, next string) string {
	switch actorType {
	case "member":
		return fmt.Sprintf(
			"members may only flip status `backlog -> todo` and `todo -> planning`; "+
				"requested `%s -> %s` was rejected. To move this card any other way, "+
				"leave a comment mentioning the agent you want to handle it (for "+
				"example: \"@bmad-agent-architect please plan this\") and the BMAD "+
				"sidecar will route the card for you.",
			prev, next)
	case "agent":
		return fmt.Sprintf(
			"agent %q is forbidden from flipping status directly (`%s -> %s`). "+
				"Status transitions are owned by the BMAD sidecar, which parses "+
				"contract markers in your comments (`<!-- claim -->`, "+
				"`<!-- impl-plan -->`, `<!-- completion-note -->`, "+
				"`<!-- review-findings -->`, `<!-- fix-note -->`, "+
				"`<!-- plan-issue -->`, `<!-- pr-opened -->`, "+
				"`<!-- post-merge-noop -->`) and routes the card. Drop the "+
				"`multica issue status` call and post your marker comment instead.",
			actorName, prev, next)
	default:
		return fmt.Sprintf("status flip `%s -> %s` is not permitted", prev, next)
	}
}

// checkStatusFlipPolicy is the centralized gate called by every issue
// handler that can change `status`. It returns (forbidden=true, msg) when
// the request must be rejected; otherwise (false, ""). On forbidden it also
// emits a structured warn log so the rejection is observable in container
// logs.
func (h *Handler) checkStatusFlipPolicy(r *http.Request, userID, workspaceID, prevStatus, newStatus, issueID string) (bool, string) {
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if actorType == "member" {
		if memberMayFlipStatus(prevStatus, newStatus) {
			return false, ""
		}
		slog.Warn("member blocked from status flip",
			append(logger.RequestAttrs(r),
				"issue_id", issueID,
				"workspace_id", workspaceID,
				"prev_status", prevStatus,
				"requested_status", newStatus,
			)...)
		return true, statusFlipForbiddenMessage("member", "", prevStatus, newStatus)
	}
	// actorType == "agent"
	actorAgent, err := h.Queries.GetAgent(r.Context(), parseUUID(actorID))
	if err != nil {
		// Unknown agent — let the rest of the handler's validation handle it
		// rather than emit a misleading lockdown 403.
		return false, ""
	}
	if agentMayFlipStatus(actorAgent.Name, prevStatus, newStatus) {
		return false, ""
	}
	slog.Warn("agent blocked from status flip",
		append(logger.RequestAttrs(r),
			"agent_name", actorAgent.Name,
			"agent_id", actorID,
			"issue_id", issueID,
			"workspace_id", workspaceID,
			"prev_status", prevStatus,
			"requested_status", newStatus,
		)...)
	return true, statusFlipForbiddenMessage("agent", actorAgent.Name, prevStatus, newStatus)
}

// IssueResponse is the JSON response for an issue.
type IssueResponse struct {
	ID                      string                  `json:"id"`
	WorkspaceID             string                  `json:"workspace_id"`
	Number                  int32                   `json:"number"`
	Identifier              string                  `json:"identifier"`
	Title                   string                  `json:"title"`
	Description             *string                 `json:"description"`
	Status                  string                  `json:"status"`
	Priority                string                  `json:"priority"`
	AssigneeType            *string                 `json:"assignee_type"`
	AssigneeID              *string                 `json:"assignee_id"`
	CreatorType             string                  `json:"creator_type"`
	CreatorID               string                  `json:"creator_id"`
	ParentIssueID           *string                 `json:"parent_issue_id"`
	ProjectID               *string                 `json:"project_id"`
	EstimateMinutes         *int32                  `json:"estimate_minutes"`
	ComputedEstimateMinutes *int32                  `json:"computed_estimate_minutes,omitempty"`
	Position                float64                 `json:"position"`
	DueDate                 *string                 `json:"due_date"`
	CreatedAt               string                  `json:"created_at"`
	UpdatedAt               string                  `json:"updated_at"`
	PhaseState              json.RawMessage         `json:"phase_state,omitempty"`
	Reactions               []IssueReactionResponse `json:"reactions,omitempty"`
	Attachments             []AttachmentResponse    `json:"attachments,omitempty"`
	// CodeRabbit / GitHub PR linkage. Set by the GitHub webhook handler when
	// a PR is opened referencing this issue's identifier (e.g. "MUL-50").
	PrURL    *string              `json:"pr_url"`
	PrNumber *int32               `json:"pr_number"`
	PrRepo   *string              `json:"pr_repo"`
	Labels   []IssueLabelResponse `json:"labels"`
	Links    []IssueLinkResponse  `json:"links"`
}

// int4ToPtr converts a pgtype.Int4 to *int32 (nil when invalid).
func int4ToPtr(v pgtype.Int4) *int32 {
	if !v.Valid {
		return nil
	}
	return &v.Int32
}

func int32ToInt4(v *int32) pgtype.Int4 {
	if v == nil {
		return pgtype.Int4{Valid: false}
	}
	return pgtype.Int4{Int32: *v, Valid: true}
}

func int32Ptr(v int32) *int32 {
	return &v
}

func intPtrFromInt32Ptr(v *int32) *int {
	if v == nil {
		return nil
	}
	out := int(*v)
	return &out
}

func stringPtrFromText(v pgtype.Text) *string {
	if !v.Valid {
		return nil
	}
	return &v.String
}

func uuidStringPtr(v pgtype.UUID) *string {
	if !v.Valid {
		return nil
	}
	s := uuidToString(v)
	return &s
}

func timePtrFromTimestamp(v pgtype.Timestamptz) *time.Time {
	if !v.Valid {
		return nil
	}
	return &v.Time
}

func gatePatchFromUpdate(rawFields map[string]json.RawMessage, req UpdateIssueRequest, params db.UpdateIssueParams) teamgate.GatePatch {
	patch := teamgate.GatePatch{}
	if _, touchedType := rawFields["assignee_type"]; touchedType {
		patch.Assignee = &teamgate.GateAssignee{
			Type: stringPtrFromText(params.AssigneeType),
			ID:   uuidStringPtr(params.AssigneeID),
		}
	} else if _, touchedID := rawFields["assignee_id"]; touchedID {
		patch.Assignee = &teamgate.GateAssignee{
			Type: stringPtrFromText(params.AssigneeType),
			ID:   uuidStringPtr(params.AssigneeID),
		}
	}
	if _, ok := rawFields["estimate_minutes"]; ok {
		patch.EstimateMinutes = intPtrFromInt32Ptr(req.EstimateMinutes)
	}
	if _, ok := rawFields["due_date"]; ok {
		patch.DueDate = timePtrFromTimestamp(params.DueDate)
	}
	if req.Status != nil {
		patch.Status = req.Status
	}
	return patch
}

func writeGateDeny(w http.ResponseWriter, body []byte, batchIndex, batchSize *int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusConflict)
	if batchIndex == nil || batchSize == nil {
		if len(body) == 0 {
			_, _ = w.Write([]byte(`{"error":"issue update denied by Team App"}`))
			return
		}
		_, _ = w.Write(body)
		return
	}
	var payload map[string]any
	if len(body) == 0 || json.Unmarshal(body, &payload) != nil {
		payload = map[string]any{"error": "issue update denied by Team App"}
	}
	payload["batch_index"] = *batchIndex
	payload["batch_size"] = *batchSize
	_ = json.NewEncoder(w).Encode(payload)
}

func gateBypassedActivityParams(workspaceID, issueID pgtype.UUID, actorType, actorID string, result teamgate.GateResult) db.CreateActivityParams {
	details, _ := json.Marshal(map[string]any{
		"reason":      "team_app_gate_fail_open",
		"status_code": result.StatusCode,
		"error":       fmt.Sprint(result.Err),
	})
	return db.CreateActivityParams{
		WorkspaceID: workspaceID,
		IssueID:     issueID,
		ActorType:   pgtype.Text{String: actorType, Valid: actorType != ""},
		ActorID:     parseUUID(actorID),
		Action:      "gate_bypassed",
		Details:     details,
	}
}

func issueToResponse(i db.Issue, issuePrefix string) IssueResponse {
	identifier := issuePrefix + "-" + strconv.Itoa(int(i.Number))
	return IssueResponse{
		ID:              uuidToString(i.ID),
		WorkspaceID:     uuidToString(i.WorkspaceID),
		Number:          i.Number,
		Identifier:      identifier,
		Title:           i.Title,
		Description:     textToPtr(i.Description),
		Status:          i.Status,
		Priority:        i.Priority,
		AssigneeType:    textToPtr(i.AssigneeType),
		AssigneeID:      uuidToPtr(i.AssigneeID),
		CreatorType:     i.CreatorType,
		CreatorID:       uuidToString(i.CreatorID),
		ParentIssueID:   uuidToPtr(i.ParentIssueID),
		ProjectID:       uuidToPtr(i.ProjectID),
		EstimateMinutes: int4ToPtr(i.EstimateMinutes),
		Position:        i.Position,
		DueDate:         timestampToPtr(i.DueDate),
		CreatedAt:       timestampToString(i.CreatedAt),
		UpdatedAt:       timestampToString(i.UpdatedAt),
		PhaseState:      bytesToRawJSON(i.PhaseState),
		PrURL:           textToPtr(i.PrUrl),
		PrNumber:        int4ToPtr(i.PrNumber),
		PrRepo:          textToPtr(i.PrRepo),
		Labels:          []IssueLabelResponse{},
		Links:           []IssueLinkResponse{},
	}
}

// issueListRowToResponse converts a list-query row (no description) to an IssueResponse.
func issueListRowToResponse(i db.ListIssuesRow, issuePrefix string) IssueResponse {
	identifier := issuePrefix + "-" + strconv.Itoa(int(i.Number))
	return IssueResponse{
		ID:              uuidToString(i.ID),
		WorkspaceID:     uuidToString(i.WorkspaceID),
		Number:          i.Number,
		Identifier:      identifier,
		Title:           i.Title,
		Description:     textToPtr(i.Description),
		Status:          i.Status,
		Priority:        i.Priority,
		AssigneeType:    textToPtr(i.AssigneeType),
		AssigneeID:      uuidToPtr(i.AssigneeID),
		CreatorType:     i.CreatorType,
		CreatorID:       uuidToString(i.CreatorID),
		ParentIssueID:   uuidToPtr(i.ParentIssueID),
		ProjectID:       uuidToPtr(i.ProjectID),
		EstimateMinutes: int4ToPtr(i.EstimateMinutes),
		Position:        i.Position,
		DueDate:         timestampToPtr(i.DueDate),
		CreatedAt:       timestampToString(i.CreatedAt),
		UpdatedAt:       timestampToString(i.UpdatedAt),
		Labels:          []IssueLabelResponse{},
		Links:           []IssueLinkResponse{},
	}
}

func openIssueRowToResponse(i db.ListOpenIssuesRow, issuePrefix string) IssueResponse {
	identifier := issuePrefix + "-" + strconv.Itoa(int(i.Number))
	return IssueResponse{
		ID:              uuidToString(i.ID),
		WorkspaceID:     uuidToString(i.WorkspaceID),
		Number:          i.Number,
		Identifier:      identifier,
		Title:           i.Title,
		Description:     textToPtr(i.Description),
		Status:          i.Status,
		Priority:        i.Priority,
		AssigneeType:    textToPtr(i.AssigneeType),
		AssigneeID:      uuidToPtr(i.AssigneeID),
		CreatorType:     i.CreatorType,
		CreatorID:       uuidToString(i.CreatorID),
		ParentIssueID:   uuidToPtr(i.ParentIssueID),
		ProjectID:       uuidToPtr(i.ProjectID),
		EstimateMinutes: int4ToPtr(i.EstimateMinutes),
		Position:        i.Position,
		DueDate:         timestampToPtr(i.DueDate),
		CreatedAt:       timestampToString(i.CreatedAt),
		UpdatedAt:       timestampToString(i.UpdatedAt),
		Labels:          []IssueLabelResponse{},
		Links:           []IssueLinkResponse{},
	}
}

// SearchIssueResponse extends IssueResponse with search metadata.
type SearchIssueResponse struct {
	IssueResponse
	MatchSource    string  `json:"match_source"`
	MatchedSnippet *string `json:"matched_snippet,omitempty"`
}

// extractSnippet extracts a snippet of text around the first occurrence of query.
// Returns up to ~120 runes centered on the match. Uses rune-based slicing to
// avoid splitting multi-byte UTF-8 characters (important for CJK content).
func extractSnippet(content, query string) string {
	runes := []rune(content)
	lowerRunes := []rune(strings.ToLower(content))
	queryRunes := []rune(strings.ToLower(query))

	idx := -1
	if len(queryRunes) > 0 && len(lowerRunes) >= len(queryRunes) {
		for i := 0; i <= len(lowerRunes)-len(queryRunes); i++ {
			match := true
			for j := range queryRunes {
				if lowerRunes[i+j] != queryRunes[j] {
					match = false
					break
				}
			}
			if match {
				idx = i
				break
			}
		}
	}

	if idx < 0 {
		if len(runes) > 120 {
			return string(runes[:120]) + "..."
		}
		return content
	}
	start := idx - 40
	if start < 0 {
		start = 0
	}
	end := idx + len(queryRunes) + 80
	if end > len(runes) {
		end = len(runes)
	}
	snippet := string(runes[start:end])
	if start > 0 {
		snippet = "..." + snippet
	}
	if end < len(runes) {
		snippet = snippet + "..."
	}
	return snippet
}

// escapeLike escapes LIKE special characters (%, _, \) in user input.
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

// splitSearchTerms splits a query into individual search terms, filtering empty strings.
func splitSearchTerms(q string) []string {
	fields := strings.FieldsFunc(q, func(r rune) bool {
		return unicode.IsSpace(r)
	})
	terms := make([]string, 0, len(fields))
	for _, f := range fields {
		if f != "" {
			terms = append(terms, f)
		}
	}
	return terms
}

// identifierNumberRe matches patterns like "MUL-123" or "ABC-45".
var identifierNumberRe = regexp.MustCompile(`(?i)^[a-z]+-(\d+)$`)

// parseQueryNumber extracts an issue number from the query if it looks like
// an identifier (e.g. "MUL-123") or a bare number (e.g. "123").
func parseQueryNumber(q string) (int, bool) {
	q = strings.TrimSpace(q)
	// Check for identifier pattern like "MUL-123"
	if m := identifierNumberRe.FindStringSubmatch(q); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil && n > 0 {
			return n, true
		}
	}
	// Check for bare number
	if n, err := strconv.Atoi(q); err == nil && n > 0 {
		return n, true
	}
	return 0, false
}

// searchResult holds a raw row from the dynamic search query.
type searchResult struct {
	issue                 db.Issue
	totalCount            int64
	matchSource           string
	matchedCommentContent string
}

// buildSearchQuery builds a dynamic SQL query for issue search.
// It uses LOWER(column) LIKE for case-insensitive matching compatible with pg_bigm 1.2 GIN indexes.
// Search patterns are lowercased in Go to avoid redundant LOWER() on the pattern side in SQL.
func buildSearchQuery(phrase string, terms []string, queryNum int, hasNum bool, includeClosed bool) (string, []any) {
	// Lowercase in Go so SQL only needs LOWER() on the column side.
	phrase = strings.ToLower(phrase)
	for i, t := range terms {
		terms[i] = strings.ToLower(t)
	}

	// Parameter index tracker
	argIdx := 1
	args := []any{}
	nextArg := func(val any) string {
		args = append(args, val)
		s := fmt.Sprintf("$%d", argIdx)
		argIdx++
		return s
	}

	escapedPhrase := escapeLike(phrase)
	phraseParam := nextArg(escapedPhrase) // $1
	phraseContains := "'%' || " + phraseParam + " || '%'"
	phraseStartsWith := phraseParam + " || '%'"

	wsParam := nextArg(nil) // $2 — workspace_id, will be filled by caller position

	// Build per-term LIKE conditions only for multi-word search.
	// For single-word queries, the phrase parameter already covers the term.
	var termParams []string
	if len(terms) > 1 {
		for _, t := range terms {
			et := escapeLike(t)
			termParams = append(termParams, nextArg(et))
		}
	}

	// --- WHERE clause ---
	var whereParts []string

	// Full phrase match: title, description, or comment
	phraseMatch := fmt.Sprintf(
		"(LOWER(i.title) LIKE %s OR LOWER(COALESCE(i.description, '')) LIKE %s OR EXISTS (SELECT 1 FROM comment c WHERE c.issue_id = i.id AND LOWER(c.content) LIKE %s))",
		phraseContains, phraseContains, phraseContains,
	)
	whereParts = append(whereParts, phraseMatch)

	// Multi-word AND match (each term must appear somewhere)
	if len(termParams) > 1 {
		var termConditions []string
		for _, tp := range termParams {
			tc := "'%' || " + tp + " || '%'"
			termConditions = append(termConditions, fmt.Sprintf(
				"(LOWER(i.title) LIKE %s OR LOWER(COALESCE(i.description, '')) LIKE %s OR EXISTS (SELECT 1 FROM comment c WHERE c.issue_id = i.id AND LOWER(c.content) LIKE %s))",
				tc, tc, tc,
			))
		}
		whereParts = append(whereParts, "("+strings.Join(termConditions, " AND ")+")")
	}

	// Number match
	numParam := ""
	if hasNum {
		numParam = nextArg(queryNum)
		whereParts = append(whereParts, fmt.Sprintf("i.number = %s", numParam))
	}

	whereClause := "(" + strings.Join(whereParts, " OR ") + ")"

	if !includeClosed {
		whereClause += " AND i.status NOT IN ('done', 'cancelled')"
	}

	// --- ORDER BY clause ---
	// Build ranking CASE with fine-grained tiers.
	var rankCases []string

	// Tier 0: Identifier exact match
	if hasNum {
		rankCases = append(rankCases, fmt.Sprintf("WHEN i.number = %s THEN 0", numParam))
	}

	// Tier 1: Exact title match
	rankCases = append(rankCases, fmt.Sprintf("WHEN LOWER(i.title) = %s THEN 1", phraseParam))

	// Tier 2: Title starts with phrase
	rankCases = append(rankCases, fmt.Sprintf("WHEN LOWER(i.title) LIKE %s THEN 2", phraseStartsWith))

	// Tier 3: Title contains phrase
	rankCases = append(rankCases, fmt.Sprintf("WHEN LOWER(i.title) LIKE %s THEN 3", phraseContains))

	// Tier 4: Title matches all words (multi-word only)
	if len(termParams) > 1 {
		var titleTerms []string
		for _, tp := range termParams {
			titleTerms = append(titleTerms, fmt.Sprintf("LOWER(i.title) LIKE '%s' || %s || '%s'", "%", tp, "%"))
		}
		rankCases = append(rankCases, fmt.Sprintf("WHEN (%s) THEN 4", strings.Join(titleTerms, " AND ")))
	}

	// Tier 5: Description contains phrase
	rankCases = append(rankCases, fmt.Sprintf("WHEN LOWER(COALESCE(i.description, '')) LIKE %s THEN 5", phraseContains))

	// Tier 6: Description matches all words (multi-word only)
	if len(termParams) > 1 {
		var descTerms []string
		for _, tp := range termParams {
			descTerms = append(descTerms, fmt.Sprintf("LOWER(COALESCE(i.description, '')) LIKE '%s' || %s || '%s'", "%", tp, "%"))
		}
		rankCases = append(rankCases, fmt.Sprintf("WHEN (%s) THEN 6", strings.Join(descTerms, " AND ")))
	}

	rankExpr := "CASE " + strings.Join(rankCases, " ") + " ELSE 7 END"

	// Status priority: active issues first
	statusRank := `CASE i.status
		WHEN 'in_progress' THEN 0
		WHEN 'todo' THEN 1
		WHEN 'blocked' THEN 2
		WHEN 'backlog' THEN 3
		WHEN 'done' THEN 4
		WHEN 'cancelled' THEN 5
		ELSE 6
	END`

	// --- match_source expression ---
	matchSourceExpr := fmt.Sprintf(`CASE
		WHEN LOWER(i.title) LIKE %s THEN 'title'
		WHEN LOWER(COALESCE(i.description, '')) LIKE %s THEN 'description'
		ELSE 'comment'
	END`, phraseContains, phraseContains)

	// For multi-word: also check if all terms match in title/description
	if len(termParams) > 1 {
		var titleTerms []string
		var descTerms []string
		for _, tp := range termParams {
			titleTerms = append(titleTerms, fmt.Sprintf("LOWER(i.title) LIKE '%s' || %s || '%s'", "%", tp, "%"))
			descTerms = append(descTerms, fmt.Sprintf("LOWER(COALESCE(i.description, '')) LIKE '%s' || %s || '%s'", "%", tp, "%"))
		}
		matchSourceExpr = fmt.Sprintf(`CASE
			WHEN LOWER(i.title) LIKE %s THEN 'title'
			WHEN (%s) THEN 'title'
			WHEN LOWER(COALESCE(i.description, '')) LIKE %s THEN 'description'
			WHEN (%s) THEN 'description'
			ELSE 'comment'
		END`,
			phraseContains, strings.Join(titleTerms, " AND "),
			phraseContains, strings.Join(descTerms, " AND "),
		)
	}

	// --- matched_comment_content subquery ---
	// Find the most recent matching comment for comment-source matches.
	commentSubquery := fmt.Sprintf(`CASE
		WHEN LOWER(i.title) LIKE %s THEN ''
		WHEN LOWER(COALESCE(i.description, '')) LIKE %s THEN ''
		ELSE COALESCE(
			(SELECT c.content FROM comment c
			 WHERE c.issue_id = i.id AND LOWER(c.content) LIKE %s
			 ORDER BY c.created_at DESC LIMIT 1),
			''
		)
	END`, phraseContains, phraseContains, phraseContains)

	// For multi-word, also find comment matching individual terms
	if len(termParams) > 1 {
		var titleTerms []string
		var descTerms []string
		var commentTerms []string
		for _, tp := range termParams {
			titleTerms = append(titleTerms, fmt.Sprintf("LOWER(i.title) LIKE '%s' || %s || '%s'", "%", tp, "%"))
			descTerms = append(descTerms, fmt.Sprintf("LOWER(COALESCE(i.description, '')) LIKE '%s' || %s || '%s'", "%", tp, "%"))
			commentTerms = append(commentTerms, fmt.Sprintf("LOWER(c.content) LIKE '%s' || %s || '%s'", "%", tp, "%"))
		}
		commentSubquery = fmt.Sprintf(`CASE
			WHEN LOWER(i.title) LIKE %s THEN ''
			WHEN (%s) THEN ''
			WHEN LOWER(COALESCE(i.description, '')) LIKE %s THEN ''
			WHEN (%s) THEN ''
			ELSE COALESCE(
				(SELECT c.content FROM comment c
				 WHERE c.issue_id = i.id AND (LOWER(c.content) LIKE %s OR (%s))
				 ORDER BY c.created_at DESC LIMIT 1),
				''
			)
		END`,
			phraseContains, strings.Join(titleTerms, " AND "),
			phraseContains, strings.Join(descTerms, " AND "),
			phraseContains, strings.Join(commentTerms, " AND "),
		)
	}

	limitParam := nextArg(nil)  // placeholder
	offsetParam := nextArg(nil) // placeholder

	query := fmt.Sprintf(`SELECT i.id, i.workspace_id, i.title, i.description, i.status, i.priority,
		i.assignee_type, i.assignee_id, i.creator_type, i.creator_id,
		i.parent_issue_id, i.acceptance_criteria, i.context_refs, i.position,
		i.due_date, i.created_at, i.updated_at, i.number, i.project_id,
		COUNT(*) OVER() AS total_count,
		%s AS match_source,
		%s AS matched_comment_content
	FROM issue i
	WHERE i.workspace_id = %s AND %s
	ORDER BY %s, %s, i.updated_at DESC
	LIMIT %s OFFSET %s`,
		matchSourceExpr,
		commentSubquery,
		wsParam,
		whereClause,
		rankExpr,
		statusRank,
		limitParam,
		offsetParam,
	)

	return query, args
}

func (h *Handler) SearchIssues(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workspaceID := h.resolveWorkspaceID(r)

	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, http.StatusBadRequest, "q parameter is required")
		return
	}

	limit := 20
	offset := 0
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			limit = v
		}
	}
	if limit > 50 {
		limit = 50
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil && v >= 0 {
			offset = v
		}
	}

	includeClosed := r.URL.Query().Get("include_closed") == "true"

	wsUUID := parseUUID(workspaceID)
	terms := splitSearchTerms(q)
	queryNum, hasNum := parseQueryNumber(q)

	sqlQuery, args := buildSearchQuery(q, terms, queryNum, hasNum, includeClosed)
	// Fill placeholder args: $2 = workspace_id, last two = limit, offset
	args[1] = wsUUID
	args[len(args)-2] = limit
	args[len(args)-1] = offset

	rows, err := h.DB.Query(ctx, sqlQuery, args...)
	if err != nil {
		slog.Warn("search issues failed", "error", err, "workspace_id", workspaceID, "query", q)
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
			&sr.issue.DueDate,
			&sr.issue.CreatedAt,
			&sr.issue.UpdatedAt,
			&sr.issue.Number,
			&sr.issue.ProjectID,
			&sr.totalCount,
			&sr.matchSource,
			&sr.matchedCommentContent,
		); err != nil {
			slog.Warn("search issues scan failed", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to search issues")
			return
		}
		results = append(results, sr)
	}
	if err := rows.Err(); err != nil {
		slog.Warn("search issues rows error", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to search issues")
		return
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
		if sr.matchSource == "comment" && sr.matchedCommentContent != "" {
			snippet := extractSnippet(sr.matchedCommentContent, q)
			sir.MatchedSnippet = &snippet
		}
		resp[i] = sir
	}

	// Bulk-enrich with labels. Unwrap into []IssueResponse so we can reuse the helper.
	if len(resp) > 0 {
		base := make([]IssueResponse, len(resp))
		for i, sir := range resp {
			base[i] = sir.IssueResponse
		}
		h.enrichIssuesWithLabels(ctx, base)
		h.enrichIssuesWithLinks(ctx, base)
		for i := range resp {
			resp[i].IssueResponse = base[i]
		}
	}

	w.Header().Set("X-Total-Count", strconv.FormatInt(total, 10))
	writeJSON(w, http.StatusOK, map[string]any{
		"issues": resp,
		"total":  total,
	})
}

func (h *Handler) ListIssues(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	workspaceID := h.resolveWorkspaceID(r)
	wsUUID := parseUUID(workspaceID)

	// Parse optional filter params
	var priorityFilter pgtype.Text
	if p := r.URL.Query().Get("priority"); p != "" {
		priorityFilter = pgtype.Text{String: p, Valid: true}
	}
	var assigneeFilter pgtype.UUID
	if a := r.URL.Query().Get("assignee_id"); a != "" {
		assigneeFilter = parseUUID(a)
	}
	var assigneeIdsFilter []pgtype.UUID
	if ids := r.URL.Query().Get("assignee_ids"); ids != "" {
		for _, raw := range strings.Split(ids, ",") {
			if s := strings.TrimSpace(raw); s != "" {
				assigneeIdsFilter = append(assigneeIdsFilter, parseUUID(s))
			}
		}
	}
	var creatorFilter pgtype.UUID
	if c := r.URL.Query().Get("creator_id"); c != "" {
		creatorFilter = parseUUID(c)
	}
	var projectFilter pgtype.UUID
	if p := r.URL.Query().Get("project_id"); p != "" {
		projectFilter = parseUUID(p)
	}

	// open_only=true returns all non-done/cancelled issues (no limit).
	if r.URL.Query().Get("open_only") == "true" {
		issues, err := h.Queries.ListOpenIssues(ctx, db.ListOpenIssuesParams{
			WorkspaceID: wsUUID,
			Priority:    priorityFilter,
			AssigneeID:  assigneeFilter,
			AssigneeIds: assigneeIdsFilter,
			CreatorID:   creatorFilter,
			ProjectID:   projectFilter,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list issues")
			return
		}

		prefix := h.getIssuePrefix(ctx, wsUUID)
		resp := make([]IssueResponse, len(issues))
		for i, issue := range issues {
			resp[i] = openIssueRowToResponse(issue, prefix)
		}
		h.enrichIssuesWithLabels(ctx, resp)
		h.enrichIssuesWithLinks(ctx, resp)

		writeJSON(w, http.StatusOK, map[string]any{
			"issues": resp,
			"total":  len(resp),
		})
		return
	}

	limit := 100
	offset := 0
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil {
			limit = v
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil {
			offset = v
		}
	}

	var statusFilter pgtype.Text
	if s := r.URL.Query().Get("status"); s != "" {
		statusFilter = pgtype.Text{String: s, Valid: true}
	}

	issues, err := h.Queries.ListIssues(ctx, db.ListIssuesParams{
		WorkspaceID: wsUUID,
		Limit:       int32(limit),
		Offset:      int32(offset),
		Status:      statusFilter,
		Priority:    priorityFilter,
		AssigneeID:  assigneeFilter,
		AssigneeIds: assigneeIdsFilter,
		CreatorID:   creatorFilter,
		ProjectID:   projectFilter,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list issues")
		return
	}

	// Get the true total count for pagination awareness.
	total, err := h.Queries.CountIssues(ctx, db.CountIssuesParams{
		WorkspaceID: wsUUID,
		Status:      statusFilter,
		Priority:    priorityFilter,
		AssigneeID:  assigneeFilter,
		AssigneeIds: assigneeIdsFilter,
		CreatorID:   creatorFilter,
		ProjectID:   projectFilter,
	})
	if err != nil {
		total = int64(len(issues))
	}

	prefix := h.getIssuePrefix(ctx, wsUUID)
	resp := make([]IssueResponse, len(issues))
	for i, issue := range issues {
		resp[i] = issueListRowToResponse(issue, prefix)
	}
	h.enrichIssuesWithLabels(ctx, resp)
	h.enrichIssuesWithLinks(ctx, resp)

	writeJSON(w, http.StatusOK, map[string]any{
		"issues": resp,
		"total":  total,
	})
}

func (h *Handler) GetIssue(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, id)
	if !ok {
		return
	}
	prefix := h.getIssuePrefix(r.Context(), issue.WorkspaceID)
	resp := issueToResponse(issue, prefix)
	if computed, err := h.Queries.GetIssueComputedEstimate(r.Context(), issue.ID); err == nil {
		resp.ComputedEstimateMinutes = int32Ptr(computed)
	} else {
		slog.Warn("get computed issue estimate failed", append(logger.RequestAttrs(r), "error", err, "issue_id", id)...)
	}

	// Fetch issue reactions.
	reactions, err := h.Queries.ListIssueReactions(r.Context(), issue.ID)
	if err == nil && len(reactions) > 0 {
		resp.Reactions = make([]IssueReactionResponse, len(reactions))
		for i, rx := range reactions {
			resp.Reactions[i] = issueReactionToResponse(rx)
		}
	}

	// Fetch issue-level attachments.
	attachments, err := h.Queries.ListAttachmentsByIssue(r.Context(), db.ListAttachmentsByIssueParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err == nil && len(attachments) > 0 {
		resp.Attachments = make([]AttachmentResponse, len(attachments))
		for i, a := range attachments {
			resp.Attachments[i] = h.attachmentToResponse(a)
		}
	}

	h.enrichIssueWithLabels(r.Context(), &resp)
	h.enrichIssueWithLinks(r.Context(), &resp)

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) ListChildIssues(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, id)
	if !ok {
		return
	}
	children, err := h.Queries.ListChildIssues(r.Context(), issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list child issues")
		return
	}
	prefix := h.getIssuePrefix(r.Context(), issue.WorkspaceID)
	resp := make([]IssueResponse, len(children))
	for i, child := range children {
		resp[i] = issueToResponse(child, prefix)
	}
	h.enrichIssuesWithLabels(r.Context(), resp)
	h.enrichIssuesWithLinks(r.Context(), resp)
	writeJSON(w, http.StatusOK, map[string]any{
		"issues": resp,
	})
}

func (h *Handler) ChildIssueProgress(w http.ResponseWriter, r *http.Request) {
	wsID := h.resolveWorkspaceID(r)
	wsUUID := parseUUID(wsID)

	rows, err := h.Queries.ChildIssueProgress(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get child issue progress")
		return
	}

	type progressEntry struct {
		ParentIssueID string `json:"parent_issue_id"`
		Total         int64  `json:"total"`
		Done          int64  `json:"done"`
	}
	resp := make([]progressEntry, len(rows))
	for i, row := range rows {
		resp[i] = progressEntry{
			ParentIssueID: uuidToString(row.ParentIssueID),
			Total:         row.Total,
			Done:          row.Done,
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"progress": resp,
	})
}

type CreateIssueRequest struct {
	Title           string   `json:"title"`
	Description     *string  `json:"description"`
	Status          string   `json:"status"`
	Priority        string   `json:"priority"`
	AssigneeType    *string  `json:"assignee_type"`
	AssigneeID      *string  `json:"assignee_id"`
	ParentIssueID   *string  `json:"parent_issue_id"`
	ProjectID       *string  `json:"project_id"`
	EstimateMinutes *int32   `json:"estimate_minutes"`
	DueDate         *string  `json:"due_date"`
	AttachmentIDs   []string `json:"attachment_ids,omitempty"`
}

func (h *Handler) CreateIssue(w http.ResponseWriter, r *http.Request) {
	var req CreateIssueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}

	workspaceID := h.resolveWorkspaceID(r)

	// Get creator from context (set by auth middleware)
	creatorID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	// Strict status-change lockdown (2026-04-29). Anything created lands
	// in `backlog` regardless of the request's `status` field. Movement
	// out of `backlog` happens via the rules in `checkStatusFlipPolicy`
	// (members: backlog -> todo; sidecar: anything; specific agent
	// exemptions). Log when an explicit non-backlog status is overridden
	// so callers can spot stale clients.
	if req.Status != "" && strings.ToLower(strings.TrimSpace(req.Status)) != "backlog" {
		slog.Info("create issue: forcing status to backlog (lockdown)",
			append(logger.RequestAttrs(r),
				"workspace_id", workspaceID,
				"requested_status", req.Status,
			)...)
	}
	status := "backlog"
	priority := req.Priority
	if priority == "" {
		priority = "none"
	}

	var assigneeType pgtype.Text
	var assigneeID pgtype.UUID
	if req.AssigneeType != nil {
		assigneeType = pgtype.Text{String: *req.AssigneeType, Valid: true}
	}
	if req.AssigneeID != nil {
		assigneeID = parseUUID(*req.AssigneeID)
		// parseUUID silently returns an invalid pgtype.UUID for malformed input.
		// Reject explicitly so the validator below cannot mistake "type unset
		// + id unparseable" for "no assignee" and accept the request.
		if !assigneeID.Valid {
			writeError(w, http.StatusBadRequest, "assignee_id is not a valid UUID")
			return
		}
	}

	if status, msg := h.validateAssigneePair(r.Context(), r, workspaceID, assigneeType, assigneeID); status != 0 {
		writeError(w, status, msg)
		return
	}

	var parentIssueID pgtype.UUID
	var projectID pgtype.UUID
	if req.ProjectID != nil {
		projectID = parseUUID(*req.ProjectID)
	}
	if req.ParentIssueID != nil {
		parentIssueID = parseUUID(*req.ParentIssueID)
		// Validate parent exists in the same workspace.
		parent, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
			ID:          parentIssueID,
			WorkspaceID: parseUUID(workspaceID),
		})
		if err != nil || !parent.ID.Valid {
			writeError(w, http.StatusBadRequest, "parent issue not found in this workspace")
			return
		}
		if req.ProjectID == nil {
			projectID = parent.ProjectID
		}
	}

	var dueDate pgtype.Timestamptz
	if req.DueDate != nil && *req.DueDate != "" {
		t, err := time.Parse(time.RFC3339, *req.DueDate)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid due_date format, expected RFC3339")
			return
		}
		dueDate = pgtype.Timestamptz{Time: t, Valid: true}
	}

	// Use a transaction to atomically increment the workspace issue counter
	// and create the issue with the assigned number.
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create issue")
		return
	}
	defer tx.Rollback(r.Context())

	qtx := h.Queries.WithTx(tx)
	issueNumber, err := qtx.IncrementIssueCounter(r.Context(), parseUUID(workspaceID))
	if err != nil {
		slog.Warn("increment issue counter failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to create issue")
		return
	}

	// Determine creator identity: agent (via X-Agent-ID header) or member.
	creatorType, actualCreatorID := h.resolveActor(r, creatorID, workspaceID)

	issue, err := qtx.CreateIssue(r.Context(), db.CreateIssueParams{
		WorkspaceID:     parseUUID(workspaceID),
		Title:           req.Title,
		Description:     ptrToText(req.Description),
		Status:          status,
		Priority:        priority,
		AssigneeType:    assigneeType,
		AssigneeID:      assigneeID,
		CreatorType:     creatorType,
		CreatorID:       parseUUID(actualCreatorID),
		ParentIssueID:   parentIssueID,
		Position:        0,
		DueDate:         dueDate,
		Number:          issueNumber,
		ProjectID:       projectID,
		EstimateMinutes: int32ToInt4(req.EstimateMinutes),
	})
	if err != nil {
		slog.Warn("create issue failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to create issue: "+err.Error())
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create issue")
		return
	}

	// Link any pre-uploaded attachments to this issue.
	if len(req.AttachmentIDs) > 0 {
		h.linkAttachmentsByIssueIDs(r.Context(), issue.ID, issue.WorkspaceID, req.AttachmentIDs)
	}

	prefix := h.getIssuePrefix(r.Context(), issue.WorkspaceID)
	resp := issueToResponse(issue, prefix)

	// Fetch linked attachments so they appear in the response.
	if len(req.AttachmentIDs) > 0 {
		attachments, err := h.Queries.ListAttachmentsByIssue(r.Context(), db.ListAttachmentsByIssueParams{
			IssueID:     issue.ID,
			WorkspaceID: issue.WorkspaceID,
		})
		if err == nil && len(attachments) > 0 {
			resp.Attachments = make([]AttachmentResponse, len(attachments))
			for i, a := range attachments {
				resp.Attachments[i] = h.attachmentToResponse(a)
			}
		}
	}

	slog.Info("issue created", append(logger.RequestAttrs(r), "issue_id", uuidToString(issue.ID), "title", issue.Title, "status", issue.Status, "workspace_id", workspaceID)...)
	h.publish(protocol.EventIssueCreated, workspaceID, creatorType, actualCreatorID, map[string]any{"issue": resp})

	// Enqueue agent task when an agent-assigned issue is created.
	if issue.AssigneeType.Valid && issue.AssigneeID.Valid {
		if h.shouldEnqueueAgentTask(r.Context(), issue) {
			h.TaskService.EnqueueTaskForIssue(r.Context(), issue)
		}
	}

	h.enrichIssueWithLabels(r.Context(), &resp)
	h.enrichIssueWithLinks(r.Context(), &resp)
	writeJSON(w, http.StatusCreated, resp)
}

type UpdateIssueRequest struct {
	Title           *string         `json:"title"`
	Description     *string         `json:"description"`
	Status          *string         `json:"status"`
	Priority        *string         `json:"priority"`
	AssigneeType    *string         `json:"assignee_type"`
	AssigneeID      *string         `json:"assignee_id"`
	Position        *float64        `json:"position"`
	DueDate         *string         `json:"due_date"`
	ParentIssueID   *string         `json:"parent_issue_id"`
	ProjectID       *string         `json:"project_id"`
	EstimateMinutes *int32          `json:"estimate_minutes"`
	PhaseState      json.RawMessage `json:"phase_state"`
}

func (h *Handler) UpdateIssue(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	prevIssue, ok := h.loadIssueForUser(w, r, id)
	if !ok {
		return
	}
	userID := requestUserID(r)
	workspaceID := uuidToString(prevIssue.WorkspaceID)

	// Read body as raw bytes so we can detect which fields were explicitly sent.
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	var req UpdateIssueRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Track which fields were explicitly present in JSON (even if null)
	var rawFields map[string]json.RawMessage
	json.Unmarshal(bodyBytes, &rawFields)

	// Pre-fill nullable fields (bare sqlc.narg) with current values
	params := db.UpdateIssueParams{
		ID:            prevIssue.ID,
		AssigneeType:  prevIssue.AssigneeType,
		AssigneeID:    prevIssue.AssigneeID,
		DueDate:       prevIssue.DueDate,
		ParentIssueID: prevIssue.ParentIssueID,
		ProjectID:     prevIssue.ProjectID,
	}

	// COALESCE fields — only set when explicitly provided
	if req.Title != nil {
		params.Title = pgtype.Text{String: *req.Title, Valid: true}
	}
	if req.Description != nil {
		params.Description = pgtype.Text{String: *req.Description, Valid: true}
	}
	if req.Status != nil {
		params.Status = pgtype.Text{String: *req.Status, Valid: true}
	}
	if req.Priority != nil {
		params.Priority = pgtype.Text{String: *req.Priority, Valid: true}
	}
	if req.Position != nil {
		params.Position = pgtype.Float8{Float64: *req.Position, Valid: true}
	}
	// Nullable fields — only override when explicitly present in JSON
	if _, ok := rawFields["assignee_type"]; ok {
		if req.AssigneeType != nil {
			params.AssigneeType = pgtype.Text{String: *req.AssigneeType, Valid: true}
		} else {
			params.AssigneeType = pgtype.Text{Valid: false} // explicit null = unassign
		}
	}
	if _, ok := rawFields["assignee_id"]; ok {
		if req.AssigneeID != nil {
			params.AssigneeID = parseUUID(*req.AssigneeID)
			if !params.AssigneeID.Valid {
				writeError(w, http.StatusBadRequest, "assignee_id is not a valid UUID")
				return
			}
		} else {
			params.AssigneeID = pgtype.UUID{Valid: false} // explicit null = unassign
		}
	}
	if _, ok := rawFields["due_date"]; ok {
		if req.DueDate != nil && *req.DueDate != "" {
			t, err := time.Parse(time.RFC3339, *req.DueDate)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid due_date format, expected RFC3339")
				return
			}
			params.DueDate = pgtype.Timestamptz{Time: t, Valid: true}
		} else {
			params.DueDate = pgtype.Timestamptz{Valid: false} // explicit null = clear date
		}
	}
	if _, ok := rawFields["parent_issue_id"]; ok {
		if req.ParentIssueID != nil {
			newParentID := parseUUID(*req.ParentIssueID)
			// Cannot set self as parent.
			if uuidToString(newParentID) == id {
				writeError(w, http.StatusBadRequest, "an issue cannot be its own parent")
				return
			}
			// Validate parent exists in the same workspace.
			if _, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
				ID:          newParentID,
				WorkspaceID: prevIssue.WorkspaceID,
			}); err != nil {
				writeError(w, http.StatusBadRequest, "parent issue not found in this workspace")
				return
			}
			// Cycle detection: walk up from the new parent to ensure we don't reach this issue.
			cursor := newParentID
			for depth := 0; depth < 10; depth++ {
				ancestor, err := h.Queries.GetIssue(r.Context(), cursor)
				if err != nil || !ancestor.ParentIssueID.Valid {
					break
				}
				if uuidToString(ancestor.ParentIssueID) == id {
					writeError(w, http.StatusBadRequest, "circular parent relationship detected")
					return
				}
				cursor = ancestor.ParentIssueID
			}
			params.ParentIssueID = newParentID
		} else {
			params.ParentIssueID = pgtype.UUID{Valid: false} // explicit null = remove parent
		}
	}
	if _, ok := rawFields["project_id"]; ok {
		if req.ProjectID != nil {
			params.ProjectID = parseUUID(*req.ProjectID)
		} else {
			params.ProjectID = pgtype.UUID{Valid: false}
		}
	}
	if _, ok := rawFields["estimate_minutes"]; ok {
		params.EstimateMinutesPresent = true
		params.EstimateMinutes = int32ToInt4(req.EstimateMinutes)
	}

	// phase_state: set when a JSON object is provided. Explicit JSON null is
	// a no-op (use a fresh object to overwrite) — the sqlc COALESCE pattern
	// preserves the existing column on NULL input. Clearing requires a direct
	// DB update, which the sidecar should never need.
	if _, ok := rawFields["phase_state"]; ok {
		if len(req.PhaseState) > 0 && string(req.PhaseState) != "null" {
			var tmp map[string]interface{}
			if err := json.Unmarshal(req.PhaseState, &tmp); err != nil {
				writeError(w, http.StatusBadRequest, "phase_state must be a JSON object")
				return
			}
			params.PhaseState = []byte(req.PhaseState)
		}
	}

	// Validate the resulting (assignee_type, assignee_id) pair when the caller
	// touches either field. Existing data on the issue is left alone if the
	// caller is not changing it.
	_, touchedType := rawFields["assignee_type"]
	_, touchedID := rawFields["assignee_id"]
	if touchedType || touchedID {
		if status, msg := h.validateAssigneePair(r.Context(), r, workspaceID, params.AssigneeType, params.AssigneeID); status != 0 {
			writeError(w, status, msg)
			return
		}
	}

	// Strict status-change lockdown (2026-04-29). Members may only flip
	// `backlog -> todo` and `todo -> planning`; the BMAD sidecar is
	// fully exempt; all other agents are blocked except for Amelia's
	// `staged` short-circuit. See `checkStatusFlipPolicy` at the top of
	// this file for full contract. This single call replaces the older
	// marker-only allowlist that only covered a subset of agents.
	if req.Status != nil && prevIssue.Status != *req.Status {
		if forbidden, msg := h.checkStatusFlipPolicy(r, userID, workspaceID, prevIssue.Status, *req.Status, id); forbidden {
			writeError(w, http.StatusForbidden, msg)
			return
		}
	}

	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	gatePatch := gatePatchFromUpdate(rawFields, req, params)
	gateResult := teamgate.GateResult{Outcome: teamgate.GateOutcomeAllow}
	if h.GateClient != nil && h.GateClient.Enabled() && gatePatch.IsTimeRelevant() {
		gateResult = h.GateClient.CallGate(r.Context(), teamgate.GateRequestBody{
			WorkspaceID: workspaceID,
			IssueID:     id,
			ActorUserID: userID,
			Force:       false,
			Patch:       gatePatch,
		})
		if gateResult.Outcome == teamgate.GateOutcomeDeny {
			writeGateDeny(w, gateResult.Body, nil, nil)
			return
		}
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update issue")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	issue, err := qtx.UpdateIssue(r.Context(), params)
	if err != nil {
		slog.Warn("update issue failed", append(logger.RequestAttrs(r), "error", err, "issue_id", id, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to update issue: "+err.Error())
		return
	}
	if gateResult.Outcome == teamgate.GateOutcomeFailOpen {
		slog.Warn("team-app gate bypassed for issue update", append(logger.RequestAttrs(r), "issue_id", id, "workspace_id", workspaceID, "status_code", gateResult.StatusCode, "error", gateResult.Err)...)
		if _, err := qtx.CreateActivity(r.Context(), gateBypassedActivityParams(issue.WorkspaceID, issue.ID, actorType, actorID, gateResult)); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update issue")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update issue")
		return
	}

	prefix := h.getIssuePrefix(r.Context(), issue.WorkspaceID)
	resp := issueToResponse(issue, prefix)
	slog.Info("issue updated", append(logger.RequestAttrs(r), "issue_id", id, "workspace_id", workspaceID)...)

	assigneeChanged := (req.AssigneeType != nil || req.AssigneeID != nil) &&
		(prevIssue.AssigneeType.String != issue.AssigneeType.String || uuidToString(prevIssue.AssigneeID) != uuidToString(issue.AssigneeID))
	statusChanged := req.Status != nil && prevIssue.Status != issue.Status
	priorityChanged := req.Priority != nil && prevIssue.Priority != issue.Priority
	descriptionChanged := req.Description != nil && textToPtr(prevIssue.Description) != resp.Description
	titleChanged := req.Title != nil && prevIssue.Title != issue.Title
	prevDueDate := timestampToPtr(prevIssue.DueDate)
	dueDateChanged := prevDueDate != resp.DueDate && (prevDueDate == nil) != (resp.DueDate == nil) ||
		(prevDueDate != nil && resp.DueDate != nil && *prevDueDate != *resp.DueDate)

	h.publish(protocol.EventIssueUpdated, workspaceID, actorType, actorID, map[string]any{
		"issue":               resp,
		"assignee_changed":    assigneeChanged,
		"status_changed":      statusChanged,
		"priority_changed":    priorityChanged,
		"due_date_changed":    dueDateChanged,
		"description_changed": descriptionChanged,
		"title_changed":       titleChanged,
		"prev_title":          prevIssue.Title,
		"prev_assignee_type":  textToPtr(prevIssue.AssigneeType),
		"prev_assignee_id":    uuidToPtr(prevIssue.AssigneeID),
		"prev_status":         prevIssue.Status,
		"prev_priority":       prevIssue.Priority,
		"prev_due_date":       prevDueDate,
		"prev_description":    textToPtr(prevIssue.Description),
		"creator_type":        prevIssue.CreatorType,
		"creator_id":          uuidToString(prevIssue.CreatorID),
	})

	// Reconcile task queue when assignee changes.
	if assigneeChanged {
		h.TaskService.CancelTasksForIssue(r.Context(), issue.ID)

		if h.shouldEnqueueAgentTask(r.Context(), issue) {
			h.TaskService.EnqueueTaskForIssue(r.Context(), issue)
		}
	}

	// Trigger the assigned agent when a member moves an issue out of backlog.
	// Backlog acts as a parking lot — moving to an active status signals the
	// issue is ready for work.
	if statusChanged && !assigneeChanged && actorType == "member" &&
		prevIssue.Status == "backlog" && issue.Status != "done" && issue.Status != "cancelled" {
		if h.isAgentAssigneeReady(r.Context(), issue) {
			h.TaskService.EnqueueTaskForIssue(r.Context(), issue)
		}
	}

	// Cancel active tasks when the issue is cancelled by a user.
	// This is distinct from agent-managed status transitions — cancellation
	// is a user-initiated terminal action that should stop execution.
	if statusChanged && issue.Status == "cancelled" {
		h.TaskService.CancelTasksForIssue(r.Context(), issue.ID)
	}

	h.enrichIssueWithLabels(r.Context(), &resp)
	h.enrichIssueWithLinks(r.Context(), &resp)
	writeJSON(w, http.StatusOK, resp)
}

// validateAssigneePair verifies the (assignee_type, assignee_id) pair refers
// to an existing entity in the workspace. For agent assignees it also enforces
// visibility (private agents are only assignable by their owner or by
// workspace admins/owners) and rejects archived agents.
//
// Returns (statusCode, errorMessage). statusCode == 0 means the pair is valid;
// callers should treat any non-zero status as a rejection and surface it back
// to the client.
func (h *Handler) validateAssigneePair(ctx context.Context, r *http.Request, workspaceID string, assigneeType pgtype.Text, assigneeID pgtype.UUID) (int, string) {
	// Both unset → unassigned issue, valid.
	if !assigneeType.Valid && !assigneeID.Valid {
		return 0, ""
	}
	// Exactly one of type/id provided → callers must always pair them.
	if assigneeType.Valid != assigneeID.Valid {
		return http.StatusBadRequest, "assignee_type and assignee_id must be provided together"
	}
	switch assigneeType.String {
	case "member":
		if _, err := h.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
			UserID:      assigneeID,
			WorkspaceID: parseUUID(workspaceID),
		}); err != nil {
			return http.StatusBadRequest, "assignee_id does not refer to a member of this workspace"
		}
		return 0, ""
	case "agent":
		agent, err := h.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{
			ID:          assigneeID,
			WorkspaceID: parseUUID(workspaceID),
		})
		if err != nil {
			return http.StatusBadRequest, "assignee_id does not refer to an agent of this workspace"
		}
		if agent.ArchivedAt.Valid {
			return http.StatusBadRequest, "cannot assign to archived agent"
		}
		if agent.Visibility == "private" {
			userID := requestUserID(r)
			if uuidToString(agent.OwnerID) != userID {
				member, err := h.getWorkspaceMember(ctx, userID, workspaceID)
				if err != nil || !roleAllowed(member.Role, "owner", "admin") {
					return http.StatusForbidden, "cannot assign to private agent"
				}
			}
		}
		return 0, ""
	default:
		return http.StatusBadRequest, "assignee_type must be 'member' or 'agent'"
	}
}

// shouldEnqueueAgentTask returns true when an issue creation or assignment
// should trigger the assigned agent. Backlog issues are skipped — backlog
// acts as a parking lot where issues can be pre-assigned without immediately
// triggering execution. Moving out of backlog is handled separately in
// UpdateIssue.
func (h *Handler) shouldEnqueueAgentTask(ctx context.Context, issue db.Issue) bool {
	if issue.Status == "backlog" {
		return false
	}
	return h.isAgentAssigneeReady(ctx, issue)
}

// shouldEnqueueOnComment returns true if a member comment on this issue should
// trigger the assigned agent. Fires for any status — comments are
// conversational and can happen at any stage, including after completion
// (e.g. follow-up questions on a done issue).
func (h *Handler) shouldEnqueueOnComment(ctx context.Context, issue db.Issue) bool {
	if !h.isAgentAssigneeReady(ctx, issue) {
		return false
	}
	// Coalescing queue: allow enqueue when a task is running (so the agent
	// picks up new comments on the next cycle) but skip if this agent already
	// has a pending task (natural dedup for rapid-fire comments).
	hasPending, err := h.Queries.HasPendingTaskForIssueAndAgent(ctx, db.HasPendingTaskForIssueAndAgentParams{
		IssueID: issue.ID,
		AgentID: issue.AssigneeID,
	})
	if err != nil || hasPending {
		return false
	}
	return true
}

// isAgentAssigneeReady checks if an issue is assigned to an active agent
// with a valid runtime.
func (h *Handler) isAgentAssigneeReady(ctx context.Context, issue db.Issue) bool {
	if !issue.AssigneeType.Valid || issue.AssigneeType.String != "agent" || !issue.AssigneeID.Valid {
		return false
	}

	agent, err := h.Queries.GetAgent(ctx, issue.AssigneeID)
	if err != nil || !agent.RuntimeID.Valid || agent.ArchivedAt.Valid {
		return false
	}

	return true
}

func (h *Handler) DeleteIssue(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, id)
	if !ok {
		return
	}

	h.TaskService.CancelTasksForIssue(r.Context(), issue.ID)
	// Fail any linked autopilot runs before delete (ON DELETE SET NULL clears issue_id).
	h.Queries.FailAutopilotRunsByIssue(r.Context(), issue.ID)

	// Collect all attachment URLs (issue-level + comment-level) before CASCADE delete.
	attachmentURLs, _ := h.Queries.ListAttachmentURLsByIssueOrComments(r.Context(), issue.ID)

	err := h.Queries.DeleteIssue(r.Context(), issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete issue")
		return
	}

	h.deleteS3Objects(r.Context(), attachmentURLs)
	userID := requestUserID(r)
	actorType, actorID := h.resolveActor(r, userID, uuidToString(issue.WorkspaceID))
	h.publish(protocol.EventIssueDeleted, uuidToString(issue.WorkspaceID), actorType, actorID, map[string]any{"issue_id": id})
	slog.Info("issue deleted", append(logger.RequestAttrs(r), "issue_id", id, "workspace_id", uuidToString(issue.WorkspaceID))...)
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Batch operations
// ---------------------------------------------------------------------------

type BatchUpdateIssuesRequest struct {
	IssueIDs []string           `json:"issue_ids"`
	Updates  UpdateIssueRequest `json:"updates"`
}

func (h *Handler) BatchUpdateIssues(w http.ResponseWriter, r *http.Request) {
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	var req BatchUpdateIssuesRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if len(req.IssueIDs) == 0 {
		writeError(w, http.StatusBadRequest, "issue_ids is required")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	// Detect which fields in "updates" were explicitly set (including null).
	var rawTop map[string]json.RawMessage
	json.Unmarshal(bodyBytes, &rawTop)
	var rawUpdates map[string]json.RawMessage
	if raw, exists := rawTop["updates"]; exists {
		json.Unmarshal(raw, &rawUpdates)
	}

	workspaceID := h.resolveWorkspaceID(r)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	type batchIssuePlan struct {
		RequestIndex int
		IssueID      string
		PrevIssue    db.Issue
		Params       db.UpdateIssueParams
		GateResult   teamgate.GateResult
	}
	plans := make([]batchIssuePlan, 0, len(req.IssueIDs))
	for idx, issueID := range req.IssueIDs {
		prevIssue, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
			ID:          parseUUID(issueID),
			WorkspaceID: parseUUID(workspaceID),
		})
		if err != nil {
			continue
		}

		params := db.UpdateIssueParams{
			ID:            prevIssue.ID,
			AssigneeType:  prevIssue.AssigneeType,
			AssigneeID:    prevIssue.AssigneeID,
			DueDate:       prevIssue.DueDate,
			ParentIssueID: prevIssue.ParentIssueID,
			ProjectID:     prevIssue.ProjectID,
		}

		if req.Updates.Title != nil {
			params.Title = pgtype.Text{String: *req.Updates.Title, Valid: true}
		}
		if req.Updates.Description != nil {
			params.Description = pgtype.Text{String: *req.Updates.Description, Valid: true}
		}
		if req.Updates.Status != nil {
			// Strict status-change lockdown (2026-04-29). Apply the same gate
			// as UpdateIssue so kanban drag-drop (which routes through
			// BatchUpdateIssues) cannot bypass member/agent transition rules.
			// On forbidden, skip this issue silently — the WS broadcast will
			// not fire and the frontend's optimistic update will revert.
			if *req.Updates.Status != prevIssue.Status {
				if forbidden, _ := h.checkStatusFlipPolicy(r, userID, workspaceID, prevIssue.Status, *req.Updates.Status, issueID); forbidden {
					continue
				}
			}
			params.Status = pgtype.Text{String: *req.Updates.Status, Valid: true}
		}
		if req.Updates.Priority != nil {
			params.Priority = pgtype.Text{String: *req.Updates.Priority, Valid: true}
		}
		if req.Updates.Position != nil {
			params.Position = pgtype.Float8{Float64: *req.Updates.Position, Valid: true}
		}
		if _, ok := rawUpdates["assignee_type"]; ok {
			if req.Updates.AssigneeType != nil {
				params.AssigneeType = pgtype.Text{String: *req.Updates.AssigneeType, Valid: true}
			} else {
				params.AssigneeType = pgtype.Text{Valid: false}
			}
		}
		if _, ok := rawUpdates["assignee_id"]; ok {
			if req.Updates.AssigneeID != nil {
				params.AssigneeID = parseUUID(*req.Updates.AssigneeID)
				if !params.AssigneeID.Valid {
					continue
				}
			} else {
				params.AssigneeID = pgtype.UUID{Valid: false}
			}
		}
		if _, ok := rawUpdates["due_date"]; ok {
			if req.Updates.DueDate != nil && *req.Updates.DueDate != "" {
				t, err := time.Parse(time.RFC3339, *req.Updates.DueDate)
				if err != nil {
					continue
				}
				params.DueDate = pgtype.Timestamptz{Time: t, Valid: true}
			} else {
				params.DueDate = pgtype.Timestamptz{Valid: false}
			}
		}

		if _, ok := rawUpdates["parent_issue_id"]; ok {
			if req.Updates.ParentIssueID != nil {
				newParentID := parseUUID(*req.Updates.ParentIssueID)
				// Cannot set self as parent.
				if uuidToString(newParentID) == issueID {
					continue
				}
				// Validate parent exists in the same workspace.
				if _, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
					ID:          newParentID,
					WorkspaceID: prevIssue.WorkspaceID,
				}); err != nil {
					continue
				}
				// Cycle detection: walk up from the new parent to ensure we don't reach this issue.
				cycleDetected := false
				cursor := newParentID
				for depth := 0; depth < 10; depth++ {
					ancestor, err := h.Queries.GetIssue(r.Context(), cursor)
					if err != nil || !ancestor.ParentIssueID.Valid {
						break
					}
					if uuidToString(ancestor.ParentIssueID) == issueID {
						cycleDetected = true
						break
					}
					cursor = ancestor.ParentIssueID
				}
				if cycleDetected {
					continue
				}
				params.ParentIssueID = newParentID
			} else {
				params.ParentIssueID = pgtype.UUID{Valid: false}
			}
		}
		if _, ok := rawUpdates["project_id"]; ok {
			if req.Updates.ProjectID != nil {
				params.ProjectID = parseUUID(*req.Updates.ProjectID)
			} else {
				params.ProjectID = pgtype.UUID{Valid: false}
			}
		}
		if _, ok := rawUpdates["estimate_minutes"]; ok {
			params.EstimateMinutesPresent = true
			params.EstimateMinutes = int32ToInt4(req.Updates.EstimateMinutes)
		}

		// Validate the resulting assignee pair when this batch update touches
		// either assignee field. Skip the issue silently on failure.
		_, batchTouchedType := rawUpdates["assignee_type"]
		_, batchTouchedID := rawUpdates["assignee_id"]
		if batchTouchedType || batchTouchedID {
			if status, _ := h.validateAssigneePair(r.Context(), r, workspaceID, params.AssigneeType, params.AssigneeID); status != 0 {
				continue
			}
		}

		plans = append(plans, batchIssuePlan{
			RequestIndex: idx,
			IssueID:      issueID,
			PrevIssue:    prevIssue,
			Params:       params,
			GateResult:   teamgate.GateResult{Outcome: teamgate.GateOutcomeAllow},
		})
	}

	batchSize := len(req.IssueIDs)
	for i := range plans {
		gatePatch := gatePatchFromUpdate(rawUpdates, req.Updates, plans[i].Params)
		if h.GateClient == nil || !h.GateClient.Enabled() || !gatePatch.IsTimeRelevant() {
			continue
		}
		result := h.GateClient.CallGate(r.Context(), teamgate.GateRequestBody{
			WorkspaceID: workspaceID,
			IssueID:     plans[i].IssueID,
			ActorUserID: userID,
			Force:       false,
			Patch:       gatePatch,
		})
		plans[i].GateResult = result
		if result.Outcome == teamgate.GateOutcomeDeny {
			batchIndex := plans[i].RequestIndex
			writeGateDeny(w, result.Body, &batchIndex, &batchSize)
			return
		}
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update issues")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	type batchUpdatedIssue struct {
		Plan  batchIssuePlan
		Issue db.Issue
		Resp  IssueResponse
	}
	updatedIssues := make([]batchUpdatedIssue, 0, len(plans))
	for _, plan := range plans {
		issue, err := qtx.UpdateIssue(r.Context(), plan.Params)
		if err != nil {
			slog.Warn("batch update issue failed", "issue_id", plan.IssueID, "error", err)
			continue
		}
		if plan.GateResult.Outcome == teamgate.GateOutcomeFailOpen {
			slog.Warn("team-app gate bypassed for batch issue update", "issue_id", plan.IssueID, "workspace_id", workspaceID, "status_code", plan.GateResult.StatusCode, "error", plan.GateResult.Err)
			if _, err := qtx.CreateActivity(r.Context(), gateBypassedActivityParams(issue.WorkspaceID, issue.ID, actorType, actorID, plan.GateResult)); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to update issues")
				return
			}
		}

		prefix := h.getIssuePrefix(r.Context(), issue.WorkspaceID)
		resp := issueToResponse(issue, prefix)
		updatedIssues = append(updatedIssues, batchUpdatedIssue{Plan: plan, Issue: issue, Resp: resp})
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update issues")
		return
	}

	updated := 0
	for _, item := range updatedIssues {
		prevIssue := item.Plan.PrevIssue
		issue := item.Issue
		_, batchTouchedType := rawUpdates["assignee_type"]
		_, batchTouchedID := rawUpdates["assignee_id"]
		assigneeChanged := (batchTouchedType || batchTouchedID) &&
			(prevIssue.AssigneeType.String != issue.AssigneeType.String || uuidToString(prevIssue.AssigneeID) != uuidToString(issue.AssigneeID))
		statusChanged := req.Updates.Status != nil && prevIssue.Status != issue.Status
		priorityChanged := req.Updates.Priority != nil && prevIssue.Priority != issue.Priority

		h.publish(protocol.EventIssueUpdated, workspaceID, actorType, actorID, map[string]any{
			"issue":            item.Resp,
			"assignee_changed": assigneeChanged,
			"status_changed":   statusChanged,
			"priority_changed": priorityChanged,
		})

		if assigneeChanged {
			h.TaskService.CancelTasksForIssue(r.Context(), issue.ID)
			if h.shouldEnqueueAgentTask(r.Context(), issue) {
				h.TaskService.EnqueueTaskForIssue(r.Context(), issue)
			}
		}

		// Trigger agent when moving out of backlog (batch).
		if statusChanged && !assigneeChanged && actorType == "member" &&
			prevIssue.Status == "backlog" && issue.Status != "done" && issue.Status != "cancelled" {
			if h.isAgentAssigneeReady(r.Context(), issue) {
				h.TaskService.EnqueueTaskForIssue(r.Context(), issue)
			}
		}

		// Cancel active tasks when the issue is cancelled by a user.
		if statusChanged && issue.Status == "cancelled" {
			h.TaskService.CancelTasksForIssue(r.Context(), issue.ID)
		}

		updated++
	}

	slog.Info("batch update issues", append(logger.RequestAttrs(r), "count", updated)...)
	writeJSON(w, http.StatusOK, map[string]any{"updated": updated})
}

type BatchDeleteIssuesRequest struct {
	IssueIDs []string `json:"issue_ids"`
}

func (h *Handler) BatchDeleteIssues(w http.ResponseWriter, r *http.Request) {
	var req BatchDeleteIssuesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if len(req.IssueIDs) == 0 {
		writeError(w, http.StatusBadRequest, "issue_ids is required")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	workspaceID := h.resolveWorkspaceID(r)
	deleted := 0
	for _, issueID := range req.IssueIDs {
		issue, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
			ID:          parseUUID(issueID),
			WorkspaceID: parseUUID(workspaceID),
		})
		if err != nil {
			continue
		}

		h.TaskService.CancelTasksForIssue(r.Context(), issue.ID)
		h.Queries.FailAutopilotRunsByIssue(r.Context(), issue.ID)

		// Collect attachment URLs before CASCADE delete to clean up S3 objects.
		attachmentURLs, _ := h.Queries.ListAttachmentURLsByIssueOrComments(r.Context(), issue.ID)

		if err := h.Queries.DeleteIssue(r.Context(), issue.ID); err != nil {
			slog.Warn("batch delete issue failed", "issue_id", issueID, "error", err)
			continue
		}

		h.deleteS3Objects(r.Context(), attachmentURLs)

		actorType, actorID := h.resolveActor(r, userID, workspaceID)
		h.publish(protocol.EventIssueDeleted, workspaceID, actorType, actorID, map[string]any{"issue_id": issueID})
		deleted++
	}

	slog.Info("batch delete issues", append(logger.RequestAttrs(r), "count", deleted)...)
	writeJSON(w, http.StatusOK, map[string]any{"deleted": deleted})
}

// bytesToRawJSON converts raw jsonb bytes from the DB to json.RawMessage for
// pass-through into IssueResponse without re-serializing. Returns nil when
// the column is SQL-NULL, producing JSON `null` (elided by omitempty on empty).
func bytesToRawJSON(b []byte) json.RawMessage {
	if len(b) == 0 {
		return nil
	}
	return json.RawMessage(b)
}

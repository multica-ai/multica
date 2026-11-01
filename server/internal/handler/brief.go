package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Limits for a brief. The boss reads these in two minutes.
const (
	briefMaxTitleLen    = 200
	briefMaxBodyLen     = 20000
	briefMaxProposalIDs = 20
	briefDefaultLimit   = 30
	briefMaxLimit       = 50
)

// briefHeadings are the H2 sections every brief carries, in this order.
var briefHeadings = []string{
	"Headline",
	"Needs your decision",
	"Progress",
	"Blockers and risks",
	"Watch",
	"FYI",
}

// briefKindWindow is how far back a brief's period reaches when the publisher
// does not say.
var briefKindWindow = map[string]time.Duration{
	"daily":  24 * time.Hour,
	"weekly": 7 * 24 * time.Hour,
	"ad_hoc": 24 * time.Hour,
}

type BriefResponse struct {
	ID          string   `json:"id"`
	WorkspaceID string   `json:"workspace_id"`
	Kind        string   `json:"kind"`
	PeriodStart string   `json:"period_start"`
	PeriodEnd   string   `json:"period_end"`
	Title       string   `json:"title"`
	Body        string   `json:"body"`
	ProposalIDs []string `json:"proposal_ids"`
	// CreatedByType is "agent" (the Secretary) or "member" (an admin
	// publishing by hand); CreatedByID is that agent's or member's user id.
	CreatedByType string `json:"created_by_type"`
	CreatedByID   string `json:"created_by_id"`
	CreatedAt     string `json:"created_at"`
}

func briefToResponse(b db.Brief) BriefResponse {
	ids := make([]string, len(b.ProposalIds))
	for i, id := range b.ProposalIds {
		ids[i] = uuidToString(id)
	}
	resp := BriefResponse{
		ID:          uuidToString(b.ID),
		WorkspaceID: uuidToString(b.WorkspaceID),
		Kind:        b.Kind,
		PeriodStart: timestampToString(b.PeriodStart),
		PeriodEnd:   timestampToString(b.PeriodEnd),
		Title:       b.Title,
		Body:        b.Body,
		ProposalIDs: ids,
		CreatedAt:   timestampToString(b.CreatedAt),
	}
	if b.CreatedByAgentID.Valid {
		resp.CreatedByType, resp.CreatedByID = "agent", uuidToString(b.CreatedByAgentID)
	} else {
		resp.CreatedByType, resp.CreatedByID = "member", uuidToString(b.CreatedByMemberID)
	}
	return resp
}

// validateBriefBody checks the body has every required H2 heading, once each
// and in order, and returns a message written for the publishing agent, or "".
// Headings inside fenced code blocks do not count.
func validateBriefBody(body string) string {
	var found []string
	inFence := false
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence || !strings.HasPrefix(line, "## ") {
			continue
		}
		found = append(found, strings.TrimSpace(strings.TrimRight(strings.TrimSpace(line[3:]), "#")))
	}

	required := make([]string, len(briefHeadings))
	for i, h := range briefHeadings {
		required[i] = "## " + h
	}
	layout := "a brief needs these H2 headings, in this order: " + strings.Join(required, ", ")

	next := 0 // index into briefHeadings of the next heading to be matched in order
	seen := map[string]bool{}
	for _, heading := range found {
		for i, want := range briefHeadings {
			if strings.EqualFold(heading, want) {
				if seen[want] {
					return fmt.Sprintf("body has the heading %q twice; %s", "## "+want, layout)
				}
				seen[want] = true
				if i < next {
					return fmt.Sprintf("the heading %q is out of order; %s", "## "+want, layout)
				}
				next = i + 1
			}
		}
	}
	var missing []string
	for _, want := range briefHeadings {
		if !seen[want] {
			missing = append(missing, "## "+want)
		}
	}
	if len(missing) > 0 {
		return fmt.Sprintf("body is missing the required heading(s) %s; %s", strings.Join(missing, ", "), layout)
	}
	return ""
}

type CreateBriefRequest struct {
	Kind        string   `json:"kind"`
	Title       string   `json:"title"`
	Body        string   `json:"body"`
	ProposalIDs []string `json:"proposal_ids"`
	PeriodStart *string  `json:"period_start"`
	PeriodEnd   *string  `json:"period_end"`
}

// mayPublishBrief reports whether the actor may publish: the workspace's
// secretary agent, or an owner or admin member. It writes the refusal.
func (h *Handler) mayPublishBrief(w http.ResponseWriter, r *http.Request, workspaceID, userID, actorType, actorID string) bool {
	const refusal = "only the workspace's secretary agent or an admin member may publish a brief"
	if actorType == "agent" {
		wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
		if !ok {
			return false
		}
		ws, err := h.Queries.GetWorkspace(r.Context(), wsUUID)
		if err != nil || !ws.SecretaryAgentID.Valid || uuidToString(ws.SecretaryAgentID) != actorID {
			writeError(w, http.StatusForbidden, refusal)
			return false
		}
		return true
	}
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return false
	}
	if !roleAllowed(member.Role, "owner", "admin") {
		writeError(w, http.StatusForbidden, refusal)
		return false
	}
	return true
}

// CreateBrief publishes a brief and announces it to the workspace owners.
//
//	POST /api/briefs {kind, title, body, proposal_ids?, period_start?, period_end?}
func (h *Handler) CreateBrief(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	var req CreateBriefRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if !h.mayPublishBrief(w, r, workspaceID, userID, actorType, actorID) {
		return
	}

	params := db.CreateBriefParams{ID: dbid.NewV7(), WorkspaceID: wsUUID}
	actorUUID, ok := parseUUIDOrBadRequest(w, actorID, "actor id")
	if !ok {
		return
	}
	if actorType == "agent" {
		params.CreatedByAgentID = actorUUID
	} else {
		params.CreatedByMemberID = actorUUID
	}

	switch req.Kind {
	case "daily", "weekly", "ad_hoc":
		params.Kind = req.Kind
	default:
		writeError(w, http.StatusBadRequest, "kind must be one of daily, weekly, ad_hoc")
		return
	}
	params.Title = strings.TrimSpace(sanitizeNullBytes(req.Title))
	if n := utf8.RuneCountInString(params.Title); n == 0 || n > briefMaxTitleLen {
		writeError(w, http.StatusBadRequest, "title is required and must be at most "+strconv.Itoa(briefMaxTitleLen)+" characters")
		return
	}
	params.Body = strings.TrimSpace(sanitizeNullBytes(req.Body))
	if n := utf8.RuneCountInString(params.Body); n > briefMaxBodyLen {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("body is %d characters; a brief is at most %d. Shorten it: a quiet day is a short brief, and the rest belongs in links", n, briefMaxBodyLen))
		return
	}
	if params.Body == "" {
		writeError(w, http.StatusBadRequest, "body is required; "+validateBriefBody(""))
		return
	}
	if msg := validateBriefBody(params.Body); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}

	now := time.Now()
	end, start := now, now.Add(-briefKindWindow[params.Kind])
	if req.PeriodEnd != nil {
		t, err := time.Parse(time.RFC3339, *req.PeriodEnd)
		if err != nil {
			writeError(w, http.StatusBadRequest, "period_end must be an RFC 3339 timestamp")
			return
		}
		end = t
		start = t.Add(-briefKindWindow[params.Kind])
	}
	if req.PeriodStart != nil {
		t, err := time.Parse(time.RFC3339, *req.PeriodStart)
		if err != nil {
			writeError(w, http.StatusBadRequest, "period_start must be an RFC 3339 timestamp")
			return
		}
		start = t
	}
	if !start.Before(end) {
		writeError(w, http.StatusBadRequest, "period_start must be before period_end")
		return
	}
	params.PeriodStart = pgtype.Timestamptz{Time: start, Valid: true}
	params.PeriodEnd = pgtype.Timestamptz{Time: end, Valid: true}

	proposalIDs, msg := h.resolveBriefProposalIDs(r, wsUUID, req.ProposalIDs)
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	params.ProposalIds = proposalIDs

	brief, err := h.Queries.CreateBrief(r.Context(), params)
	if err != nil {
		slog.Warn("create brief failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to create brief")
		return
	}

	resp := briefToResponse(brief)
	h.publish(protocol.EventBriefCreated, workspaceID, actorType, actorID, map[string]any{"brief": resp})
	slog.Info("brief published", append(logger.RequestAttrs(r), "brief_id", resp.ID, "kind", resp.Kind)...)
	writeJSON(w, http.StatusCreated, resp)
}

// resolveBriefProposalIDs parses the proposal ids a brief raises, drops
// duplicates and checks they all exist in the workspace. The message is
// written for the publishing agent.
func (h *Handler) resolveBriefProposalIDs(r *http.Request, workspaceID pgtype.UUID, raw []string) ([]pgtype.UUID, string) {
	ids := make([]pgtype.UUID, 0, len(raw))
	seen := map[string]bool{}
	for _, s := range raw {
		s = strings.TrimSpace(s)
		id, err := util.ParseUUID(s)
		if err != nil {
			return nil, fmt.Sprintf("proposal_ids: %q is not a proposal id", s)
		}
		if key := uuidToString(id); !seen[key] {
			seen[key] = true
			ids = append(ids, id)
		}
	}
	if len(ids) > briefMaxProposalIDs {
		return nil, "proposal_ids may list at most " + strconv.Itoa(briefMaxProposalIDs) + " proposals"
	}
	if len(ids) == 0 {
		return ids, ""
	}
	found, err := h.Queries.ListProposalIDsInWorkspace(r.Context(), db.ListProposalIDsInWorkspaceParams{WorkspaceID: workspaceID, Ids: ids})
	if err != nil {
		return nil, "failed to check proposal_ids"
	}
	exists := map[string]bool{}
	for _, id := range found {
		exists[uuidToString(id)] = true
	}
	for _, id := range ids {
		if !exists[uuidToString(id)] {
			return nil, fmt.Sprintf("proposal_ids: no proposal %s in this workspace", uuidToString(id))
		}
	}
	return ids, ""
}

// ListBriefs returns the workspace's briefs, newest first.
//
//	GET /api/briefs?kind=daily|weekly|ad_hoc&limit=30
func (h *Handler) ListBriefs(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	params := db.ListBriefsParams{WorkspaceID: wsUUID, RowLimit: briefDefaultLimit}
	if kind := r.URL.Query().Get("kind"); kind != "" {
		switch kind {
		case "daily", "weekly", "ad_hoc":
			params.Kind = pgtype.Text{String: kind, Valid: true}
		default:
			writeError(w, http.StatusBadRequest, "kind must be one of daily, weekly, ad_hoc")
			return
		}
	}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > briefMaxLimit {
			writeError(w, http.StatusBadRequest, "limit must be between 1 and "+strconv.Itoa(briefMaxLimit))
			return
		}
		params.RowLimit = int32(n)
	}
	rows, err := h.Queries.ListBriefs(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list briefs")
		return
	}
	resp := make([]BriefResponse, len(rows))
	for i, row := range rows {
		resp[i] = briefToResponse(row)
	}
	writeJSON(w, http.StatusOK, resp)
}

// GetBrief returns one brief.
func (h *Handler) GetBrief(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "brief id")
	if !ok {
		return
	}
	brief, err := h.Queries.GetBriefInWorkspace(r.Context(), db.GetBriefInWorkspaceParams{ID: id, WorkspaceID: wsUUID})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "brief not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load brief")
		return
	}
	writeJSON(w, http.StatusOK, briefToResponse(brief))
}

// Package references owns the cerebro-only "issue reference" feature: a
// generic table that attaches typed external/internal pointers to an issue
// (Stripe customers, GitHub PRs, Linear issues, internal entities). The
// table shape is intentionally generic — `(object, type, ref_id)` describes
// what is being pointed at, `metadata` carries object-specific fields — so
// new reference kinds can be added without a migration per kind.
//
// Backend fundament for JEH-837 / JEH-808. Frontend (the
// `cerebro-references` extension) lands in a sibling sub-issue.
package references

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// WS event types broadcast on the workspace channel. The cerebro prefix
// keeps the namespace separate from upstream `comment:created`-style events
// so frontends can subscribe without ambiguity. See JEH-837 for the exact
// strings.
const (
	EventReferenceCreated = "cerebro.issue_reference.created"
	EventReferenceUpdated = "cerebro.issue_reference.updated"
	EventReferenceDeleted = "cerebro.issue_reference.deleted"
)

// Handler holds the cerebro reference HTTP endpoints. Carries both query
// packages because reference rows live in cerebro but the issue access
// check (workspace scoping) reads from the upstream issue table.
type Handler struct {
	Cerebro  *cerebrodb.Queries
	Upstream *db.Queries
	Bus      *events.Bus
}

// New constructs the handler. The router wires the dependencies in.
func New(cerebro *cerebrodb.Queries, upstream *db.Queries, bus *events.Bus) *Handler {
	return &Handler{Cerebro: cerebro, Upstream: upstream, Bus: bus}
}

// ReferenceResponse is the JSON shape returned to clients and embedded in
// WS payloads. Keeps the API stable even if the underlying column types
// change (e.g. nullable text vs string).
type ReferenceResponse struct {
	ID            string          `json:"id"`
	IssueID       string          `json:"issue_id"`
	Object        string          `json:"object"`
	Type          *string         `json:"type"`
	RefID         string          `json:"ref_id"`
	Label         *string         `json:"label"`
	URL           *string         `json:"url"`
	Metadata      json.RawMessage `json:"metadata"`
	CreatedByType string          `json:"created_by_type"`
	CreatedByID   string          `json:"created_by_id"`
	CreatedAt     string          `json:"created_at"`
	UpdatedAt     string          `json:"updated_at"`
}

func toResponse(r cerebrodb.CerebroIssueReference) ReferenceResponse {
	meta := json.RawMessage(r.Metadata)
	if len(meta) == 0 {
		meta = json.RawMessage("{}")
	}
	return ReferenceResponse{
		ID:            util.UUIDToString(r.ID),
		IssueID:       util.UUIDToString(r.IssueID),
		Object:        r.Object,
		Type:          util.TextToPtr(r.Type),
		RefID:         r.RefID,
		Label:         util.TextToPtr(r.Label),
		URL:           util.TextToPtr(r.Url),
		Metadata:      meta,
		CreatedByType: r.CreatedByType,
		CreatedByID:   util.UUIDToString(r.CreatedByID),
		CreatedAt:     util.TimestampToString(r.CreatedAt),
		UpdatedAt:     util.TimestampToString(r.UpdatedAt),
	}
}

// createRequest is the POST body for /api/issues/{id}/references. `object`
// and `ref_id` are required; everything else is optional. `metadata` is
// raw JSON so callers can attach payload-specific fields.
type createRequest struct {
	Object   string          `json:"object"`
	Type     *string         `json:"type"`
	RefID    string          `json:"ref_id"`
	Label    *string         `json:"label"`
	URL      *string         `json:"url"`
	Metadata json.RawMessage `json:"metadata"`
}

// updateRequest is the PATCH body for /api/cerebro/references/{refId}. Only
// the mutable fields (label, url, metadata) are accepted — re-pointing a
// reference at a different external object is a delete-then-insert, not an
// update, so we keep the API narrow.
type updateRequest struct {
	Label    *string         `json:"label"`
	URL      *string         `json:"url"`
	Metadata json.RawMessage `json:"metadata"`
}

// ListByIssue handles GET /api/issues/{id}/references. Returns every
// reference attached to the issue, ordered by creation time so callers
// see the oldest pin first.
func (h *Handler) ListByIssue(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssue(w, r)
	if !ok {
		return
	}

	rows, err := h.Cerebro.ListReferencesByIssue(r.Context(), issue.ID)
	if err != nil {
		slog.Error("references list failed", "issue_id", util.UUIDToString(issue.ID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load references")
		return
	}

	out := make([]ReferenceResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, toResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"references": out})
}

// Create handles POST /api/issues/{id}/references. UPSERT semantics on
// (issue_id, object, type, ref_id): re-posting the same identifier
// refreshes label/url/metadata and the updated_at timestamp without
// rewriting the original creator. Returns 201 on first insert, 200 on
// update — distinguished by whether created_at == updated_at after the
// query.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssue(w, r)
	if !ok {
		return
	}

	var req createRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Object = strings.TrimSpace(req.Object)
	req.RefID = strings.TrimSpace(req.RefID)
	if req.Object == "" {
		writeError(w, http.StatusBadRequest, "object is required")
		return
	}
	if req.RefID == "" {
		writeError(w, http.StatusBadRequest, "ref_id is required")
		return
	}
	metadata := normalizeMetadata(req.Metadata)
	if metadata == nil {
		writeError(w, http.StatusBadRequest, "metadata must be a JSON object")
		return
	}

	creatorType, creatorID, ok := h.requireCreator(w, r)
	if !ok {
		return
	}

	row, err := h.Cerebro.InsertReference(r.Context(), cerebrodb.InsertReferenceParams{
		IssueID:       issue.ID,
		Object:        req.Object,
		Type:          util.PtrToText(req.Type),
		RefID:         req.RefID,
		Label:         util.PtrToText(req.Label),
		Url:           util.PtrToText(req.URL),
		Metadata:      metadata,
		CreatedByType: creatorType,
		CreatedByID:   creatorID,
	})
	if err != nil {
		slog.Error("reference insert failed", "issue_id", util.UUIDToString(issue.ID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create reference")
		return
	}

	resp := toResponse(row)
	wasUpsert := !row.CreatedAt.Time.Equal(row.UpdatedAt.Time)
	eventType := EventReferenceCreated
	if wasUpsert {
		eventType = EventReferenceUpdated
	}
	h.publish(eventType, util.UUIDToString(issue.WorkspaceID), creatorType, util.UUIDToString(creatorID), resp)

	status := http.StatusCreated
	if wasUpsert {
		status = http.StatusOK
	}
	writeJSON(w, status, resp)
}

// Update handles PATCH /api/cerebro/references/{refId}. Only the mutable
// fields (label, url, metadata) are touched; re-pointing the reference at
// a different external object requires a delete + insert.
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	ref, ok := h.loadReference(w, r)
	if !ok {
		return
	}

	var req updateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Only overwrite fields the client explicitly sent. PATCH semantics —
	// an absent JSON key keeps the existing value. We detect "absent" via
	// nil for *string and the helper for json.RawMessage.
	label := ref.Label
	if req.Label != nil {
		label = util.PtrToText(req.Label)
	}
	url := ref.Url
	if req.URL != nil {
		url = util.PtrToText(req.URL)
	}
	metadata := ref.Metadata
	if len(req.Metadata) > 0 {
		normalized := normalizeMetadata(req.Metadata)
		if normalized == nil {
			writeError(w, http.StatusBadRequest, "metadata must be a JSON object")
			return
		}
		metadata = normalized
	}

	row, err := h.Cerebro.UpdateReference(r.Context(), cerebrodb.UpdateReferenceParams{
		ID:       ref.ID,
		Label:    label,
		Url:      url,
		Metadata: metadata,
	})
	if err != nil {
		slog.Error("reference update failed", "reference_id", util.UUIDToString(ref.ID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update reference")
		return
	}

	resp := toResponse(row)
	actorType, actorID := h.actorFromRequest(r)
	h.publish(EventReferenceUpdated, h.workspaceForIssue(r.Context(), ref.IssueID), actorType, actorID, resp)
	writeJSON(w, http.StatusOK, resp)
}

// Delete handles DELETE /api/cerebro/references/{refId}. Returns 204 on
// success; the WS event carries the deleted row's payload so subscribers
// can remove the row from their cache without a re-fetch.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	ref, ok := h.loadReference(w, r)
	if !ok {
		return
	}

	row, err := h.Cerebro.DeleteReference(r.Context(), ref.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "reference not found")
			return
		}
		slog.Error("reference delete failed", "reference_id", util.UUIDToString(ref.ID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete reference")
		return
	}

	resp := toResponse(row)
	actorType, actorID := h.actorFromRequest(r)
	h.publish(EventReferenceDeleted, h.workspaceForIssue(r.Context(), ref.IssueID), actorType, actorID, resp)
	w.WriteHeader(http.StatusNoContent)
}

// ListByObject handles GET /api/cerebro/references?object=&ref_id=. Reverse
// lookup: "which issues are linked to this Stripe customer / GitHub PR?".
// Workspace-scoped — only returns rows whose issue is in the caller's
// workspace.
func (h *Handler) ListByObject(w http.ResponseWriter, r *http.Request) {
	wsID, ok := h.requireWorkspace(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	object := strings.TrimSpace(q.Get("object"))
	refID := strings.TrimSpace(q.Get("ref_id"))
	if object == "" {
		writeError(w, http.StatusBadRequest, "object query param is required")
		return
	}
	if refID == "" {
		writeError(w, http.StatusBadRequest, "ref_id query param is required")
		return
	}

	rows, err := h.Cerebro.ListIssuesByReference(r.Context(), cerebrodb.ListIssuesByReferenceParams{
		Object: object,
		RefID:  refID,
	})
	if err != nil {
		slog.Error("references reverse-lookup failed", "object", object, "ref_id", refID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load references")
		return
	}

	out := make([]ReferenceResponse, 0, len(rows))
	for _, row := range rows {
		issue, err := h.Upstream.GetIssue(r.Context(), row.IssueID)
		if err != nil {
			continue
		}
		if util.UUIDToString(issue.WorkspaceID) != wsID {
			continue
		}
		out = append(out, toResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"references": out})
}

// loadIssue resolves and authorizes the {id} URL param. The issue must
// exist in the caller's workspace; per-project / per-channel access is
// gated by the route's middleware (RequireWorkspaceMember + the cerebro
// references frontend tying access to issues the user can already see).
func (h *Handler) loadIssue(w http.ResponseWriter, r *http.Request) (db.Issue, bool) {
	wsID, ok := h.requireWorkspace(w, r)
	if !ok {
		return db.Issue{}, false
	}
	wsUUID, err := util.ParseUUID(wsID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return db.Issue{}, false
	}
	idParam := chi.URLParam(r, "id")
	issueUUID, err := util.ParseUUID(idParam)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid issue id")
		return db.Issue{}, false
	}
	issue, err := h.Upstream.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
		ID:          issueUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "issue not found")
		return db.Issue{}, false
	}
	return issue, true
}

// loadReference resolves and authorizes the {refId} URL param. The
// reference's issue must be in the caller's workspace, otherwise we 404
// to avoid leaking existence across workspaces.
func (h *Handler) loadReference(w http.ResponseWriter, r *http.Request) (cerebrodb.CerebroIssueReference, bool) {
	wsID, ok := h.requireWorkspace(w, r)
	if !ok {
		return cerebrodb.CerebroIssueReference{}, false
	}
	wsUUID, err := util.ParseUUID(wsID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return cerebrodb.CerebroIssueReference{}, false
	}
	refUUID, err := util.ParseUUID(chi.URLParam(r, "refId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid reference id")
		return cerebrodb.CerebroIssueReference{}, false
	}
	ref, err := h.Cerebro.GetReference(r.Context(), refUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "reference not found")
		return cerebrodb.CerebroIssueReference{}, false
	}
	issue, err := h.Upstream.GetIssue(r.Context(), ref.IssueID)
	if err != nil {
		writeError(w, http.StatusNotFound, "reference not found")
		return cerebrodb.CerebroIssueReference{}, false
	}
	if util.UUIDToString(issue.WorkspaceID) != util.UUIDToString(wsUUID) {
		// Cross-workspace access — collapse to 404 so callers can't probe
		// reference IDs outside their own workspace.
		writeError(w, http.StatusNotFound, "reference not found")
		return cerebrodb.CerebroIssueReference{}, false
	}
	return ref, true
}

// requireWorkspace pulls the workspace UUID from middleware context. The
// route is mounted under RequireWorkspaceMember so this is always set; we
// keep the explicit guard so a misrouted handler returns a clean 400.
func (h *Handler) requireWorkspace(w http.ResponseWriter, r *http.Request) (string, bool) {
	wsID := middleware.WorkspaceIDFromContext(r.Context())
	if wsID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return "", false
	}
	return wsID, true
}

// requireCreator returns the actor (member or agent) writing the row. We
// store both type and ID so the audit trail stays intact regardless of
// whether a human or a task-scoped agent posted the reference.
func (h *Handler) requireCreator(w http.ResponseWriter, r *http.Request) (string, pgtype.UUID, bool) {
	if scope := middleware.AuthScopeFromContext(r.Context()); scope == middleware.ScopeTask {
		ts := middleware.TaskScopeFromContext(r.Context())
		agentUUID, err := util.ParseUUID(ts.AgentID)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid agent id on task token")
			return "", pgtype.UUID{}, false
		}
		return "agent", agentUUID, true
	}
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return "", pgtype.UUID{}, false
	}
	userUUID, err := util.ParseUUID(userID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return "", pgtype.UUID{}, false
	}
	return "member", userUUID, true
}

// actorFromRequest is the best-effort version of requireCreator used on
// update/delete WS broadcasts. Falls back to "system" if neither path
// produces an actor — a publish without an actor is preferable to a 500
// after the DB write already succeeded.
func (h *Handler) actorFromRequest(r *http.Request) (string, string) {
	if scope := middleware.AuthScopeFromContext(r.Context()); scope == middleware.ScopeTask {
		ts := middleware.TaskScopeFromContext(r.Context())
		return "agent", ts.AgentID
	}
	if userID := r.Header.Get("X-User-ID"); userID != "" {
		return "member", userID
	}
	return "system", ""
}

// workspaceForIssue resolves the workspace UUID for a given issue. Used by
// the update/delete WS publishers — the URL only carries the reference id
// so we can't read the workspace from the path.
func (h *Handler) workspaceForIssue(ctx context.Context, issueID pgtype.UUID) string {
	issue, err := h.Upstream.GetIssue(ctx, issueID)
	if err != nil {
		return ""
	}
	return util.UUIDToString(issue.WorkspaceID)
}

func (h *Handler) publish(eventType, workspaceID, actorType, actorID string, payload any) {
	if h.Bus == nil || workspaceID == "" {
		return
	}
	h.Bus.Publish(events.Event{
		Type:        eventType,
		WorkspaceID: workspaceID,
		ActorType:   actorType,
		ActorID:     actorID,
		Payload:     payload,
	})
}

// normalizeMetadata coerces the request body's metadata field into the
// JSONB shape the database expects. Empty / null becomes "{}"; non-object
// JSON (arrays, scalars) is rejected with nil so the caller can return
// 400. JSON-malformed input is also rejected.
func normalizeMetadata(raw json.RawMessage) []byte {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return []byte("{}")
	}
	if !strings.HasPrefix(trimmed, "{") {
		return nil
	}
	var probe map[string]any
	if err := json.Unmarshal([]byte(trimmed), &probe); err != nil {
		return nil
	}
	return []byte(trimmed)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

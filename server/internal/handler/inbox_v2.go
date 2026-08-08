package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service/inboxv2"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// The v2 inbox surface.
//
// One row per (person, source) instead of one row per event. The client no
// longer folds anything: what it renders, what it counts and what it marks read
// are the same entity, which is the entire point — three clients each folding
// by their own rules is how the unread count, the archived view and the jump
// target came to disagree.
//
// v1 stays mounted and fully functional beside this. Mobile lives there
// indefinitely, and the frontend gate can be turned off at any moment without a
// data migration.

const (
	// inboxV2PageSize bounds one page. Keyset, not offset, so a group arriving
	// mid-scroll cannot shift the page boundary and duplicate or skip a row.
	inboxV2PageSize = 50
	// inboxV2MaxPageSize is the ceiling a client may ask for.
	inboxV2MaxPageSize = 200
)

// InboxGroupResponse is one inbox row as the product has always meant it.
type InboxGroupResponse struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	RecipientID string `json:"recipient_id"`

	SourceKind string `json:"source_kind"`
	SourceID   string `json:"source_id"`

	Unread   bool `json:"unread"`
	Archived bool `json:"archived"`

	// Seq and StateVersion travel back on a read so the server can tell an
	// intentional action from a stale one. Without StateVersion, "the user
	// marked this unread and a background read raced it" and "the user read it"
	// are the same request.
	Seq          int64 `json:"seq"`
	StateVersion int64 `json:"state_version"`

	SurfacedAt string `json:"surfaced_at"`

	// The representative event, flattened. Clients render this and never see
	// the event table.
	EventID     string          `json:"event_id"`
	Type        string          `json:"type"`
	Severity    string          `json:"severity"`
	Title       string          `json:"title"`
	Body        *string         `json:"body"`
	ActorType   *string         `json:"actor_type"`
	ActorID     *string         `json:"actor_id"`
	Details     json.RawMessage `json:"details"`
	IssueID     *string         `json:"issue_id"`
	IssueStatus *string         `json:"issue_status"`
	CreatedAt   string          `json:"created_at"`

	// The jump target, resolved server-side. The old client borrowed the
	// target from whatever row it happened to be rendering, which is why
	// clicking a group could land on the wrong comment.
	TargetKind *string `json:"target_kind"`
	TargetID   *string `json:"target_id"`
}

func inboxGroupToResponse(g db.ListInboxGroupsForRecipientRow) InboxGroupResponse {
	return InboxGroupResponse{
		ID:           uuidToString(g.ID),
		WorkspaceID:  uuidToString(g.WorkspaceID),
		RecipientID:  uuidToString(g.RecipientID),
		SourceKind:   g.SourceKind,
		SourceID:     uuidToString(g.SourceID),
		Unread:       g.Unread.Bool,
		Archived:     g.ArchivedAt.Valid,
		Seq:          g.LatestSeq,
		StateVersion: g.StateVersion,
		SurfacedAt:   timestampToString(g.SurfacedAt),
		EventID:      uuidToString(g.LatestEventID),
		Type:         g.EventType,
		Severity:     g.EventSeverity,
		Title:        g.EventTitle,
		Body:         textToPtr(g.EventBody),
		ActorType:    textToPtr(g.EventActorType),
		ActorID:      uuidToPtr(g.EventActorID),
		Details:      json.RawMessage(g.EventDetails),
		IssueID:      uuidToPtr(g.EventIssueID),
		IssueStatus:  textToPtr(g.IssueStatus),
		CreatedAt:    timestampToString(g.EventCreatedAt),
		TargetKind:   textToPtr(g.EventTargetKind),
		TargetID:     uuidToPtr(g.EventTargetID),
	}
}

// inboxV2Page is the list envelope. An object rather than a bare array because
// the cursor and the readiness flag have to travel with the page, and because
// an array response can never gain a field without breaking every parser.
type inboxV2Page struct {
	Items      []InboxGroupResponse `json:"items"`
	NextCursor *string              `json:"next_cursor"`
	// Ready=false means this user's history has not been folded into groups yet
	// and the client should use v1 for now. It is not an error: v1 is completely
	// correct, just less pleasant.
	Ready bool `json:"ready"`
}

// ensureInboxGroups runs the lazy migration barrier for this user.
//
// Every v2 read passes through it. User-level rather than workspace-level
// because the cross-workspace summary reads every workspace at once, and a
// per-workspace barrier would let that endpoint report from a half-migrated
// world.
func (h *Handler) ensureInboxGroups(r *http.Request, userID pgtype.UUID) bool {
	ready, err := h.inboxService().EnsureGroups(r.Context(), userID, time.Now())
	if err != nil {
		slog.Warn("inbox v2: lazy migration failed, falling back to v1",
			"user_id", uuidToString(userID), "error", err)
		return false
	}
	return ready
}

func inboxV2Cursor(r *http.Request) (pgtype.Timestamptz, pgtype.UUID) {
	at := r.URL.Query().Get("cursor_at")
	id := r.URL.Query().Get("cursor_id")
	if at == "" || id == "" {
		return pgtype.Timestamptz{}, pgtype.UUID{}
	}
	t, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return pgtype.Timestamptz{}, pgtype.UUID{}
	}
	parsed, err := util.ParseUUID(id)
	if err != nil {
		return pgtype.Timestamptz{}, pgtype.UUID{}
	}
	return pgtype.Timestamptz{Time: t, Valid: true}, parsed
}

func inboxV2PageSizeParam(r *http.Request) int32 {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return inboxV2PageSize
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return inboxV2PageSize
	}
	if n > inboxV2MaxPageSize {
		return inboxV2MaxPageSize
	}
	return int32(n)
}

func inboxV2NextCursor(items []InboxGroupResponse, pageSize int32) *string {
	if len(items) < int(pageSize) || len(items) == 0 {
		return nil
	}
	last := items[len(items)-1]
	cursor := last.SurfacedAt + "|" + last.ID
	return &cursor
}

// ListInboxV2 is GET /api/v2/inbox.
func (h *Handler) ListInboxV2(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, ctxWorkspaceID(r.Context()), "workspace id")
	if !ok {
		return
	}
	user := parseUUID(userID)
	if !h.ensureInboxGroups(r, user) {
		writeJSON(w, http.StatusOK, inboxV2Page{Items: []InboxGroupResponse{}, Ready: false})
		return
	}

	cursorAt, cursorID := inboxV2Cursor(r)
	pageSize := inboxV2PageSizeParam(r)
	rows, err := h.Queries.ListInboxGroupsForRecipient(r.Context(), db.ListInboxGroupsForRecipientParams{
		WorkspaceID:     wsUUID,
		RecipientID:     user,
		Now:             pgtype.Timestamptz{Time: time.Now(), Valid: true},
		AfterSurfacedAt: cursorAt,
		AfterID:         cursorID,
		PageSize:        pageSize,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list inbox")
		return
	}

	items := make([]InboxGroupResponse, len(rows))
	for i, row := range rows {
		items[i] = inboxGroupToResponse(row)
	}
	writeJSON(w, http.StatusOK, inboxV2Page{
		Items:      items,
		NextCursor: inboxV2NextCursor(items, pageSize),
		Ready:      true,
	})
}

// ListArchivedInboxV2 is GET /api/v2/inbox/archived.
func (h *Handler) ListArchivedInboxV2(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, ctxWorkspaceID(r.Context()), "workspace id")
	if !ok {
		return
	}
	user := parseUUID(userID)
	if !h.ensureInboxGroups(r, user) {
		writeJSON(w, http.StatusOK, inboxV2Page{Items: []InboxGroupResponse{}, Ready: false})
		return
	}

	cursorAt, cursorID := inboxV2Cursor(r)
	pageSize := inboxV2PageSizeParam(r)
	rows, err := h.Queries.ListArchivedInboxGroupsForRecipient(r.Context(), db.ListArchivedInboxGroupsForRecipientParams{
		WorkspaceID:     wsUUID,
		RecipientID:     user,
		AfterSurfacedAt: cursorAt,
		AfterID:         cursorID,
		PageSize:        pageSize,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list archived inbox")
		return
	}
	items := make([]InboxGroupResponse, len(rows))
	for i, row := range rows {
		items[i] = inboxGroupToResponse(db.ListInboxGroupsForRecipientRow(row))
	}
	writeJSON(w, http.StatusOK, inboxV2Page{
		Items:      items,
		NextCursor: inboxV2NextCursor(items, pageSize),
		Ready:      true,
	})
}

// CountUnreadInboxV2 is GET /api/v2/inbox/unread-count.
func (h *Handler) CountUnreadInboxV2(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, ctxWorkspaceID(r.Context()), "workspace id")
	if !ok {
		return
	}
	user := parseUUID(userID)
	if !h.ensureInboxGroups(r, user) {
		writeJSON(w, http.StatusOK, map[string]any{"count": 0, "ready": false})
		return
	}
	count, err := h.Queries.CountUnreadInboxGroups(r.Context(), db.CountUnreadInboxGroupsParams{
		WorkspaceID: wsUUID, RecipientID: user,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count unread inbox")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": count, "ready": true})
}

// UnreadInboxSummaryV2 is GET /api/v2/inbox/unread-summary.
func (h *Handler) UnreadInboxSummaryV2(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	user := parseUUID(userID)
	if !h.ensureInboxGroups(r, user) {
		writeJSON(w, http.StatusOK, map[string]any{
			"workspaces": []InboxWorkspaceUnreadResponse{}, "ready": false,
		})
		return
	}
	rows, err := h.Queries.CountUnreadInboxGroupsByWorkspace(r.Context(), user)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to summarize unread inbox")
		return
	}
	resp := make([]InboxWorkspaceUnreadResponse, len(rows))
	for i, row := range rows {
		resp[i] = InboxWorkspaceUnreadResponse{
			WorkspaceID: uuidToString(row.WorkspaceID),
			Count:       row.Count,
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"workspaces": resp, "ready": true})
}

// inboxV2ReadRequest carries what the client believed when it acted.
type inboxV2ReadRequest struct {
	ObservedSeq          *int64 `json:"observed_seq"`
	ObservedStateVersion *int64 `json:"observed_state_version"`
}

// MarkInboxGroupReadV2 is POST /api/v2/inbox/{id}/read.
//
// The observation is what makes this safe against the races v1 could not
// express. A read reports the sequence the user actually saw, so it can never
// mark a NEWER event read; and it reports the state version, so a read issued
// before the user's own "mark unread" — and arriving after it — does not undo
// their decision.
func (h *Handler) MarkInboxGroupReadV2(w http.ResponseWriter, r *http.Request) {
	h.inboxV2GroupWrite(w, r, "failed to mark read", protocol.EventInboxRead,
		func(svc *inboxv2.Service, group db.InboxGroup, body inboxV2ReadRequest) (db.InboxGroup, error) {
			observedSeq := group.LatestSeq
			if body.ObservedSeq != nil {
				observedSeq = *body.ObservedSeq
			}
			observedVersion := group.StateVersion
			if body.ObservedStateVersion != nil {
				observedVersion = *body.ObservedStateVersion
			}
			return svc.MarkGroupRead(r.Context(), group, observedSeq, observedVersion, time.Now())
		})
}

// MarkInboxGroupUnreadV2 is POST /api/v2/inbox/{id}/unread.
func (h *Handler) MarkInboxGroupUnreadV2(w http.ResponseWriter, r *http.Request) {
	h.inboxV2GroupWrite(w, r, "failed to mark unread", protocol.EventInboxUnread,
		func(svc *inboxv2.Service, group db.InboxGroup, _ inboxV2ReadRequest) (db.InboxGroup, error) {
			return svc.MarkGroupUnread(r.Context(), group, time.Now())
		})
}

// ArchiveInboxGroupV2 is POST /api/v2/inbox/{id}/archive.
func (h *Handler) ArchiveInboxGroupV2(w http.ResponseWriter, r *http.Request) {
	h.inboxV2GroupWrite(w, r, "failed to archive", protocol.EventInboxArchived,
		func(svc *inboxv2.Service, group db.InboxGroup, _ inboxV2ReadRequest) (db.InboxGroup, error) {
			return svc.ArchiveGroup(r.Context(), group, time.Now())
		})
}

// UnarchiveInboxGroupV2 is POST /api/v2/inbox/{id}/unarchive.
func (h *Handler) UnarchiveInboxGroupV2(w http.ResponseWriter, r *http.Request) {
	h.inboxV2GroupWrite(w, r, "failed to unarchive", protocol.EventInboxUnarchived,
		func(svc *inboxv2.Service, group db.InboxGroup, _ inboxV2ReadRequest) (db.InboxGroup, error) {
			return svc.UnarchiveGroup(r.Context(), group, time.Now())
		})
}

func (h *Handler) inboxV2GroupWrite(
	w http.ResponseWriter,
	r *http.Request,
	failure string,
	event string,
	apply func(*inboxv2.Service, db.InboxGroup, inboxV2ReadRequest) (db.InboxGroup, error),
) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, ctxWorkspaceID(r.Context()), "workspace id")
	if !ok {
		return
	}
	groupID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "group id")
	if !ok {
		return
	}
	user := parseUUID(userID)

	// Ownership comes from the row, never from the path: a UUID in a URL is not
	// proof that the caller owns what it names.
	group, err := h.Queries.GetInboxGroupForRecipient(r.Context(), db.GetInboxGroupForRecipientParams{
		ID: groupID, WorkspaceID: wsUUID, RecipientID: user,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "inbox item not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, failure)
		return
	}

	var body inboxV2ReadRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}

	updated, err := apply(h.inboxService(), group, body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, failure)
		return
	}

	h.publish(event, uuidToString(wsUUID), "member", userID, map[string]any{
		"group_id":      uuidToString(updated.ID),
		"recipient_id":  userID,
		"state_version": updated.StateVersion,
	})

	row, err := h.Queries.GetInboxGroupWithEvent(r.Context(), db.GetInboxGroupWithEventParams{
		ID: updated.ID, WorkspaceID: wsUUID, RecipientID: user,
	})
	if err != nil {
		// The write landed; only the render-back failed. Report the state we know.
		writeJSON(w, http.StatusOK, map[string]any{
			"id": uuidToString(updated.ID), "state_version": updated.StateVersion,
		})
		return
	}
	writeJSON(w, http.StatusOK, inboxGroupToResponse(db.ListInboxGroupsForRecipientRow(row)))
}

// inboxV2Batch handles the four bulk endpoints.
func (h *Handler) inboxV2Batch(w http.ResponseWriter, r *http.Request, op inboxv2.BatchOp, event string) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, ctxWorkspaceID(r.Context()), "workspace id")
	if !ok {
		return
	}
	user := parseUUID(userID)
	if !h.ensureInboxGroups(r, user) {
		writeError(w, http.StatusServiceUnavailable, "inbox is still being prepared")
		return
	}

	groups, err := h.inboxService().ApplyBatch(r.Context(), wsUUID, user, op, time.Now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update inbox")
		return
	}
	h.publish(event, uuidToString(wsUUID), "member", userID, map[string]any{
		"recipient_id": userID,
		"count":        int64(len(groups)),
	})
	writeJSON(w, http.StatusOK, map[string]any{"count": len(groups)})
}

func (h *Handler) MarkAllInboxReadV2(w http.ResponseWriter, r *http.Request) {
	h.inboxV2Batch(w, r, inboxv2.BatchMarkAllRead, protocol.EventInboxBatchRead)
}

func (h *Handler) ArchiveAllInboxV2(w http.ResponseWriter, r *http.Request) {
	h.inboxV2Batch(w, r, inboxv2.BatchArchiveAll, protocol.EventInboxBatchArchived)
}

func (h *Handler) ArchiveAllReadInboxV2(w http.ResponseWriter, r *http.Request) {
	h.inboxV2Batch(w, r, inboxv2.BatchArchiveRead, protocol.EventInboxBatchArchived)
}

func (h *Handler) ArchiveCompletedInboxV2(w http.ResponseWriter, r *http.Request) {
	h.inboxV2Batch(w, r, inboxv2.BatchArchiveComplete, protocol.EventInboxBatchArchived)
}

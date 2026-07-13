// Package note holds the cerebro-only HTTP handlers for the Notes feature
// (TECH-3421). A Note is a lightweight, private-by-default note built on top of
// the upstream artifact (Document) machinery: it is an artifact with
// kind='note' plus a cerebro_note row that carries owner + visibility + pin
// state. The handler creates the artifact via the upstream Queries and the
// note-specific state via the cerebro Queries, so notes reuse the artifact
// editor, storage, folders and search while staying fork-clean (the upstream
// artifact table is never modified).
package note

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// EventNoteMentioned fires when a note save introduces one or more new person
// (member) mentions. The notification listener (cerebro_note_mentions.go) turns
// each mentioned member into a routed "mentioned" inbox notification, reusing
// the same engine + settings as comment mentions. Payload keys: "note_id",
// "note_title", "member_ids" ([]string of the newly-mentioned user IDs).
const EventNoteMentioned = "cerebro:note_mentioned"

// validVisibility gates the visibility values a client may set. Mirrors the
// CHECK constraint in migration 9073_cerebro_note.
var validVisibility = map[string]bool{
	"private":   true,
	"shared":    true,
	"workspace": true,
}

const (
	defaultListLimit = 50
	maxListLimit     = 200
	recentLimit      = 5
)

// Handler exposes the cerebro-only Notes endpoints. It carries the upstream
// sqlc Queries (to create/read/delete the underlying artifact) and the cerebro
// Queries (for note owner/visibility/pin/share state).
type Handler struct {
	Upstream *db.Queries
	Cerebro  *cerebrodb.Queries
	Bus      *events.Bus
	// Tasks dispatches the "send comments to agent" flow (FIR-1621) to the
	// coupled issue or chat. Optional: nil disables the send endpoint with a 503
	// (e.g. in unit tests that don't exercise dispatch).
	Tasks TaskDispatcher
	// Gate is the agent-mention trigger gate (FIR-1621). Optional: nil skips the
	// permission check (tests); in production it is wired to the same gate the
	// issue comment path uses.
	Gate MentionGate
	// Pool is the raw pgx pool the FTS note/document search uses (FIR-2022).
	// Search runs raw SQL inside a tx so it can SET LOCAL the pg_trgm word-
	// similarity threshold; sqlc cannot express the dynamic text-vs-kind SQL.
	// Optional: nil makes GET /api/notes/search return 503 (e.g. in unit tests).
	Pool *pgxpool.Pool
	// Issues creates an issue from a single note comment (FIR-3102). Optional:
	// nil makes POST /{id}/comments/{commentId}/create-issue return 503 (e.g. in
	// unit tests that do not exercise issue creation).
	Issues IssueCreator
}

// New constructs the handler. The router wires both query packages, the event
// bus (used to fan person-mentions out to the notification listener), and the
// task dispatcher (FIR-1621 send-to-agent).
func New(upstream *db.Queries, cerebro *cerebrodb.Queries, bus *events.Bus, tasks TaskDispatcher) *Handler {
	return &Handler{Upstream: upstream, Cerebro: cerebro, Bus: bus, Tasks: tasks}
}

// notifyNoteMentions handles person (@member) mentions introduced by a note
// save. It diffs the new body against the old (oldBody is "" on create) and,
// for every member mention that is newly added and is not the author:
//   - shares the note with them so the notification is openable — a private
//     note is bumped to 'shared' the first time it gains a tagged person, and
//   - publishes EventNoteMentioned so the notification listener creates a
//     routed "mentioned" inbox item, reusing the comment-mention engine.
//
// commentID/excerpt carry the comment context when the mention came from a note
// comment (empty for a note-body mention): commentID is the thread-root id the
// inbox item deep-links to, and excerpt is a trimmed preview shown as the inbox
// message body so the notification reads with context (FIR-2589).
//
// Everything here is best-effort: a note save must never fail because a share
// or notification could not be created.
func (h *Handler) notifyNoteMentions(ctx context.Context, wsID, artifactID, ownerID pgtype.UUID, title, oldBody, newBody, visibility, commentID, excerpt string) {
	added := newMemberMentions(oldBody, newBody, uuidStr(ownerID))
	if len(added) == 0 {
		return
	}

	// FIR-2595: who gets notified depends on whether the note lives in a folder.
	// A foldered note is governed by the folder's Collections grants (Stage 1),
	// so silently sharing the note with a tagged person no longer opens it — the
	// folder still gates them and they'd get "note not found" (the FIR-2589
	// symptom). So for a foldered note we do NOT auto-share: we notify only
	// members who can already open the note. Granting a tagged person who lacks
	// access is an explicit choice made via the mention-access endpoint (the
	// "give access?" prompt), which grants folder access before the mention is
	// saved — so by the time we run here, they can see it. A note with no folder
	// has nothing to gate, so the original share + private→shared path applies.
	art, artErr := h.Upstream.GetArtifact(ctx, db.GetArtifactParams{ID: artifactID, WorkspaceID: wsID})
	foldered := artErr == nil && art.FolderID.Valid

	var notify []string
	if foldered {
		for _, uid := range added {
			mu, err := util.ParseUUID(uid)
			if err != nil {
				continue
			}
			ok, err := h.Cerebro.CanUserSeeNote(ctx, cerebrodb.CanUserSeeNoteParams{ArtifactID: artifactID, OwnerID: mu})
			if err == nil && ok {
				notify = append(notify, uid)
			}
		}
	} else {
		for _, uid := range added {
			mu, err := util.ParseUUID(uid)
			if err != nil {
				continue
			}
			_ = h.Cerebro.AddNoteShare(ctx, cerebrodb.AddNoteShareParams{ArtifactID: artifactID, UserID: mu})
		}
		// Shares only grant access when visibility is 'shared' (or 'workspace'),
		// so a private note becomes shared the moment it gains a tagged person.
		if visibility == "private" {
			_ = h.Cerebro.SetNoteVisibility(ctx, cerebrodb.SetNoteVisibilityParams{ArtifactID: artifactID, Visibility: "shared"})
		}
		notify = added
	}

	if len(notify) == 0 {
		return
	}
	if h.Bus == nil {
		return
	}
	payload := map[string]any{
		"note_id":    uuidStr(artifactID),
		"note_title": title,
		"member_ids": notify,
	}
	// A comment mention carries the comment it was tagged in, so the inbox item
	// can deep-link straight to that comment and show its text as context.
	if commentID != "" {
		payload["comment_id"] = commentID
		payload["comment_excerpt"] = excerpt
	}
	h.Bus.Publish(events.Event{
		Type:        EventNoteMentioned,
		WorkspaceID: uuidStr(wsID),
		ActorType:   "member",
		ActorID:     uuidStr(ownerID),
		Payload:     payload,
	})
}

// newMemberMentions returns the user IDs mentioned as members in newBody that
// were not already mentioned in oldBody, excluding excludeID (the author).
// Order is stable and duplicates are removed, so re-saving a note never
// re-notifies someone already tagged.
func newMemberMentions(oldBody, newBody, excludeID string) []string {
	old := map[string]bool{}
	for _, m := range util.ParseMentions(oldBody) {
		if m.Type == "member" {
			old[m.ID] = true
		}
	}
	seen := map[string]bool{}
	var out []string
	for _, m := range util.ParseMentions(newBody) {
		if m.Type != "member" || m.ID == excludeID || old[m.ID] || seen[m.ID] {
			continue
		}
		seen[m.ID] = true
		out = append(out, m.ID)
	}
	return out
}

// Routes mounts the Notes endpoints under /api/notes.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/", h.ListNotes)
	r.Post("/", h.CreateNote)
	r.Get("/recent", h.ListRecentNotes)
	// FIR-2022 — full-text search over documents (all artifact kinds) + their
	// comments. Static path, registered before /{id} so chi never treats
	// "search" as a note id. See search.go.
	r.Get("/search", h.SearchNotes)
	// FIR-1621 — reverse note↔object coupling: list notes that reference a given
	// object (e.g. an issue), so the issue page shows its coupled notes.
	r.Get("/by-reference", h.ListNotesForReference)
	r.Get("/{id}", h.GetNote)
	r.Put("/{id}", h.UpdateNote)
	r.Delete("/{id}", h.DeleteNote)
	r.Put("/{id}/visibility", h.SetVisibility)
	r.Put("/{id}/pin", h.SetPin)
	// FIR-2810 — per-note "stamp the writer's member code on every line" toggle.
	r.Put("/{id}/author-codes", h.SetAuthorCodes)
	r.Get("/{id}/shares", h.ListShares)
	// FIR-2595 — the "give access?" prompt: check which tagged members can't open
	// the note, and grant them access when the author confirms. See mention_access.go.
	r.Get("/{id}/mention-access", h.MentionAccessCheck)
	r.Post("/{id}/mention-access", h.GrantMentionAccess)
	// References on a note (TECH-3421): mirror of the issue-reference feature,
	// keyed by the note's artifact id. See references.go.
	r.Get("/{id}/references", h.ListReferences)
	r.Post("/{id}/references", h.CreateReference)
	r.Delete("/{id}/references/{refId}", h.DeleteReference)

	// Wave 3 / G1 — comments + suggestions on a note. See comments.go.
	r.Get("/{id}/comments", h.ListComments)
	r.Post("/{id}/comments", h.CreateComment)
	r.Put("/{id}/comments/{commentId}", h.UpdateComment)
	r.Delete("/{id}/comments/{commentId}", h.DeleteComment)
	r.Post("/{id}/comments/{commentId}/resolve", h.ResolveComment)
	r.Post("/{id}/comments/{commentId}/suggestion", h.DecideSuggestion)
	// FIR-3102 — create a standalone issue from one comment (see create_issue.go).
	r.Post("/{id}/comments/{commentId}/create-issue", h.CreateIssueFromComment)
	// FIR-1621 — dispatch selected/all unsent comments to the coupled destination.
	r.Post("/{id}/comments/send", h.SendComments)

	// Wave 3 / G2 — version history. See versions.go.
	r.Get("/{id}/versions", h.ListVersions)
	r.Post("/{id}/versions", h.SaveVersion)
	r.Post("/{id}/versions/{versionId}/restore", h.RestoreVersion)

	// Wave 3 / G3 — interim edit lock. See lock.go.
	r.Get("/{id}/lock", h.GetLock)
	r.Post("/{id}/lock", h.AcquireLock)
	r.Delete("/{id}/lock", h.ReleaseLock)

	// FIR-1317 Plan A — AI-assisted conflict merge. See merge.go.
	r.Post("/{id}/merge", h.MergeNote)
}

// --- request / response shapes ---

type createNoteRequest struct {
	Title      string  `json:"title"`
	Body       string  `json:"body"`
	FolderID   *string `json:"folder_id,omitempty"`
	Visibility string  `json:"visibility,omitempty"`
	// FIR-1852: when a note is created in an issue/project context, set the same
	// issue_id/project_id columns a document (artifact) uses, so the note is
	// unified with documents — it appears in the issue's document list and the
	// editor renders "on FIR-XXX". Optional; absent means a workspace-scoped
	// note exactly as before.
	IssueID   *string `json:"issue_id,omitempty"`
	ProjectID *string `json:"project_id,omitempty"`
}

type updateNoteRequest struct {
	Title *string `json:"title,omitempty"`
	Body  *string `json:"body,omitempty"`
	// BaseUpdatedAt is the note's updated_at when the client last fetched it.
	// If provided and it doesn't match the server's current updated_at, the
	// update is rejected with 409 and the server's current body is returned
	// so the client can offer an AI-assisted merge (FIR-1317 Plan A).
	BaseUpdatedAt *string `json:"base_updated_at,omitempty"`
}

// noteConflictResponse is the 409 body returned when a concurrent save is detected.
type noteConflictResponse struct {
	Conflict    bool   `json:"conflict"`
	ServerBody  string `json:"server_body"`
	ServerTitle string `json:"server_title"`
}

type setVisibilityRequest struct {
	Visibility    string   `json:"visibility"`
	SharedUserIDs []string `json:"shared_user_ids,omitempty"`
}

type setPinRequest struct {
	Pinned bool `json:"pinned"`
}

// FIR-2810: body for PUT /{id}/author-codes.
type setAuthorCodesRequest struct {
	AuthorCodes bool `json:"author_codes"`
}

// NoteResponse is the wire shape for a note: the artifact essentials plus the
// note-specific state. It is intentionally lighter than the full artifact
// response — the Notes list and editor only need these fields.
type NoteResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	FolderID    *string `json:"folder_id"`
	Title       string  `json:"title"`
	Body        string  `json:"body"`
	OwnerID     string  `json:"owner_id"`
	Visibility  string  `json:"visibility"`
	Pinned      bool    `json:"pinned"`
	// FIR-1852: the unified issue/project scope a note shares with documents.
	// Populated on the single-note reads (create/get) where the artifact row is
	// loaded; nil on lightweight list rows that never render the meta header.
	IssueID   *string `json:"issue_id"`
	ProjectID *string `json:"project_id"`
	// FIR-2145: populated on list reads; 0 on single-note detail reads.
	CommentCount int64  `json:"comment_count"`
	// FIR-2595: whether THIS caller may edit and save the note. Driven by folder
	// access (owner, or an 'editor'/'full_access' grant on the note's folder).
	// Populated on the single-note reads (create/get/update) that feed the
	// editor; the editor uses it to render read-only for viewers instead of
	// letting them type into a field that silently fails to save. Absent on the
	// lightweight list rows (the editor always refetches the single note).
	CanEdit   bool   `json:"can_edit"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	// FIR-2810: per-note toggle — when true the editor stamps the writer's
	// member code (e.g. "JEH") on every line they write. Populated on the
	// single-note reads; false on lightweight list rows.
	AuthorCodes bool `json:"author_codes"`
	// FIR-2810: per-line attribution (who created / last edited each body
	// line), aligned index-for-index with the body's lines. Populated on the
	// single-note reads that feed the editor; omitted on list rows.
	LineAttrs []LineAttr `json:"line_attrs,omitempty"`
}

// --- handlers ---

// CreateNote creates an empty-or-filled note: an artifact (kind='note') plus a
// cerebro_note row owned by the caller, private by default. No title is
// required — when absent the note is stored titleless and the client derives
// the title from the first line.
func (h *Handler) CreateNote(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsID := middleware.WorkspaceIDFromContext(r.Context())
	if wsID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	var req createNoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	visibility := req.Visibility
	if visibility == "" {
		visibility = "private"
	}
	if !validVisibility[visibility] {
		writeError(w, http.StatusBadRequest, "invalid visibility; expected private|shared|workspace")
		return
	}

	wsUUID, err := util.ParseUUID(wsID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return
	}
	ownerUUID, err := util.ParseUUID(userID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}

	var folderID pgtype.UUID
	if req.FolderID != nil && *req.FolderID != "" {
		folder, err := h.Upstream.GetArtifactFolder(r.Context(), db.GetArtifactFolderParams{
			ID:          mustUUID(*req.FolderID),
			WorkspaceID: wsUUID,
		})
		if err != nil {
			writeError(w, http.StatusNotFound, "folder not found")
			return
		}
		folderID = folder.ID
	}

	// FIR-1852: resolve optional issue/project scope so a note carries the same
	// issue_id/project_id a document does. Both are validated against the
	// caller's workspace; an unknown id is a 404 rather than a silent drop.
	var issueID, projectID pgtype.UUID
	if req.IssueID != nil && *req.IssueID != "" {
		parsed, err := util.ParseUUID(*req.IssueID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid issue id")
			return
		}
		issue, err := h.Upstream.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
			ID:          parsed,
			WorkspaceID: wsUUID,
		})
		if err != nil {
			writeError(w, http.StatusNotFound, "issue not found")
			return
		}
		issueID = issue.ID
	}
	if req.ProjectID != nil && *req.ProjectID != "" {
		parsed, err := util.ParseUUID(*req.ProjectID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid project id")
			return
		}
		project, err := h.Upstream.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
			ID:          parsed,
			WorkspaceID: wsUUID,
		})
		if err != nil {
			writeError(w, http.StatusNotFound, "project not found")
			return
		}
		projectID = project.ID
	}

	id, _ := uuid.NewV7()
	artifact, err := h.Upstream.CreateArtifact(r.Context(), db.CreateArtifactParams{
		ID:          pgtype.UUID{Bytes: id, Valid: true},
		WorkspaceID: wsUUID,
		IssueID:     issueID,
		ProjectID:   projectID,
		FolderID:    folderID,
		Kind:        "note",
		Format:      "md",
		Title:       req.Title,
		Body:        req.Body,
		Metadata:    []byte("{}"),
		AuthorType:  "member",
		AuthorID:    ownerUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create note")
		return
	}

	noteRow, err := h.Cerebro.UpsertNote(r.Context(), cerebrodb.UpsertNoteParams{
		ArtifactID: artifact.ID,
		OwnerID:    ownerUUID,
		Visibility: visibility,
		Pinned:     false,
	})
	if err != nil {
		// Best-effort cleanup so we don't leave an artifact with no note row.
		_ = h.Upstream.DeleteArtifact(r.Context(), db.DeleteArtifactParams{ID: artifact.ID, WorkspaceID: wsUUID})
		writeError(w, http.StatusInternalServerError, "failed to create note")
		return
	}

	// Tagging a person in the note body notifies them (and shares the note).
	h.notifyNoteMentions(r.Context(), wsUUID, artifact.ID, ownerUUID, artifact.Title, "", artifact.Body, noteRow.Visibility, "", "")

	// FIR-2810: seed per-line attribution — every initial line is the creator's.
	lineAttrs := h.advanceAndSaveLineAttrs(r.Context(), artifact.ID, "", artifact.Body, userID)

	writeJSON(w, http.StatusCreated, NoteResponse{
		ID:          uuidStr(artifact.ID),
		WorkspaceID: uuidStr(artifact.WorkspaceID),
		FolderID:    uuidPtr(artifact.FolderID),
		Title:       artifact.Title,
		Body:        artifact.Body,
		OwnerID:     uuidStr(noteRow.OwnerID),
		Visibility:  noteRow.Visibility,
		Pinned:      noteRow.Pinned,
		CanEdit:     true, // the creator owns the note
		IssueID:     uuidPtr(artifact.IssueID),
		ProjectID:   uuidPtr(artifact.ProjectID),
		CreatedAt:   tsStr(artifact.CreatedAt),
		UpdatedAt:   tsStr(artifact.UpdatedAt),
		AuthorCodes: false, // new notes start with the toggle off
		LineAttrs:   lineAttrs,
	})
}

// ListNotes returns the caller's visible notes in a workspace, pinned-first
// then most-recent. An optional ?q= filters by title/body (private notes only
// ever match for their owner).
func (h *Handler) ListNotes(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ownerUUID, ok := h.scope(w, r, userID)
	if !ok {
		return
	}
	limit, offset := pageParams(r)

	q := r.URL.Query().Get("q")
	var rows []NoteResponse
	if q != "" {
		res, err := h.Cerebro.SearchNotesForUser(r.Context(), cerebrodb.SearchNotesForUserParams{
			WorkspaceID: wsUUID,
			OwnerID:     ownerUUID,
			Q:           q,
			Limit:       limit,
			Offset:      offset,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to search notes")
			return
		}
		rows = make([]NoteResponse, 0, len(res))
		for _, n := range res {
			rows = append(rows, searchRowToResponse(n))
		}
	} else {
		res, err := h.Cerebro.ListNotesForUser(r.Context(), cerebrodb.ListNotesForUserParams{
			WorkspaceID: wsUUID,
			OwnerID:     ownerUUID,
			Limit:       limit,
			Offset:      offset,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list notes")
			return
		}
		rows = make([]NoteResponse, 0, len(res))
		for _, n := range res {
			rows = append(rows, listRowToResponse(n))
		}
	}
	writeJSON(w, http.StatusOK, rows)
}

// ListRecentNotes feeds the Notes box in the dynamic inbox: a small pinned-first
// feed of the caller's visible notes.
func (h *Handler) ListRecentNotes(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ownerUUID, ok := h.scope(w, r, userID)
	if !ok {
		return
	}
	limit := int32(recentLimit)
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 20 {
			limit = int32(n)
		}
	}
	res, err := h.Cerebro.ListRecentNotesForUser(r.Context(), cerebrodb.ListRecentNotesForUserParams{
		WorkspaceID: wsUUID,
		OwnerID:     ownerUUID,
		Limit:       limit,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list recent notes")
		return
	}
	rows := make([]NoteResponse, 0, len(res))
	for _, n := range res {
		rows = append(rows, recentRowToResponse(n))
	}
	writeJSON(w, http.StatusOK, rows)
}

// GetNote returns a single note when the caller is allowed to see it.
func (h *Handler) GetNote(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ownerUUID, ok := h.scope(w, r, userID)
	if !ok {
		return
	}
	noteID, ok := pathUUID(w, r)
	if !ok {
		return
	}
	allowed, err := h.Cerebro.CanUserSeeNote(r.Context(), cerebrodb.CanUserSeeNoteParams{
		ArtifactID: noteID,
		OwnerID:    ownerUUID,
	})
	if err != nil || !allowed {
		writeError(w, http.StatusNotFound, "note not found")
		return
	}
	artifact, err := h.Upstream.GetArtifact(r.Context(), db.GetArtifactParams{ID: noteID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "note not found")
		return
	}
	noteRow, err := h.Cerebro.GetNote(r.Context(), noteID)
	if err != nil {
		writeError(w, http.StatusNotFound, "note not found")
		return
	}
	// FIR-2595: tell the editor whether this caller may edit and save, so a
	// viewer gets a read-only editor instead of a field that silently fails on
	// save. Owner or an 'editor'/'full_access' folder grant → true.
	canEdit, err := h.Cerebro.CanUserEditNote(r.Context(), cerebrodb.CanUserEditNoteParams{
		ArtifactID: noteID,
		OwnerID:    ownerUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load note")
		return
	}
	// FIR-2810: attach per-line attribution, healing it first when the body
	// was changed outside the note handler (agent via the artifact API, the
	// note-types sweeper, …) so the array always aligns with the body.
	lineAttrs := h.ensureLineAttrs(r.Context(), artifact.ID, artifact.Body)
	writeJSON(w, http.StatusOK, NoteResponse{
		ID:          uuidStr(artifact.ID),
		WorkspaceID: uuidStr(artifact.WorkspaceID),
		FolderID:    uuidPtr(artifact.FolderID),
		Title:       artifact.Title,
		Body:        artifact.Body,
		OwnerID:     uuidStr(noteRow.OwnerID),
		Visibility:  noteRow.Visibility,
		Pinned:      noteRow.Pinned,
		CanEdit:     canEdit,
		IssueID:     uuidPtr(artifact.IssueID),
		ProjectID:   uuidPtr(artifact.ProjectID),
		CreatedAt:   tsStr(artifact.CreatedAt),
		UpdatedAt:   tsStr(artifact.UpdatedAt),
		AuthorCodes: noteRow.AuthorCodes,
		LineAttrs:   lineAttrs,
	})
}

// UpdateNote edits a note's title and/or body. The owner may always edit; a
// user with an 'editor' / 'full_access' Collections grant on the note's folder
// (or an ancestor) may edit and save too (FIR-2595). The body/title live on the
// underlying artifact, so this loads the artifact, merges the provided fields,
// and writes it back via the upstream update.
func (h *Handler) UpdateNote(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ownerUUID, ok := h.scope(w, r, userID)
	if !ok {
		return
	}
	noteID, ok := pathUUID(w, r)
	if !ok {
		return
	}
	if !h.requireCanEdit(w, r, noteID, ownerUUID) {
		return
	}
	var req updateNoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	artifact, err := h.Upstream.GetArtifact(r.Context(), db.GetArtifactParams{ID: noteID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "note not found")
		return
	}

	// FIR-2810 follow-up: a save that changes nothing can never conflict.
	// The editor's blur + debounce autosaves (and the author-code stamper)
	// can re-send an identical body while an earlier save's response is in
	// flight; answering 409 on those surfaced a bogus "two people edited this
	// note" merge dialog to a user typing alone. Answer with the current
	// state instead so the client re-syncs its base timestamp.
	if (req.Body == nil || *req.Body == artifact.Body) &&
		(req.Title == nil || *req.Title == artifact.Title) {
		noteRow, err := h.Cerebro.GetNote(r.Context(), noteID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update note")
			return
		}
		writeJSON(w, http.StatusOK, NoteResponse{
			ID:          uuidStr(artifact.ID),
			WorkspaceID: uuidStr(artifact.WorkspaceID),
			FolderID:    uuidPtr(artifact.FolderID),
			Title:       artifact.Title,
			Body:        artifact.Body,
			OwnerID:     uuidStr(noteRow.OwnerID),
			Visibility:  noteRow.Visibility,
			Pinned:      noteRow.Pinned,
			CanEdit:     true, // caller just passed requireCanEdit
			IssueID:     uuidPtr(artifact.IssueID),
			ProjectID:   uuidPtr(artifact.ProjectID),
			CreatedAt:   tsStr(artifact.CreatedAt),
			UpdatedAt:   tsStr(artifact.UpdatedAt),
			AuthorCodes: noteRow.AuthorCodes,
			LineAttrs:   h.ensureLineAttrs(r.Context(), noteID, artifact.Body),
		})
		return
	}

	// FIR-1317 Plan A — optimistic concurrency: if the client sent the
	// updated_at it last saw and it doesn't match, someone else saved in the
	// meantime. Return 409 with the server's current content so the frontend
	// can offer an AI-assisted merge dialog before retrying.
	if req.BaseUpdatedAt != nil && *req.BaseUpdatedAt != "" {
		serverTS := tsStr(artifact.UpdatedAt)
		if *req.BaseUpdatedAt != serverTS {
			// FIR-2810 review (Jesper, 2026-07-11): the author-code stamper
			// can fire on a stale base (blur in an old tab while a save is
			// settling). When the incoming body is exactly the current body
			// plus system-inserted stamps, nothing can be lost — accept it
			// instead of opening the merge dialog. Anything else, including
			// the user's own edits from elsewhere, still conflicts.
			stampOnly := req.Body != nil &&
				(req.Title == nil || *req.Title == artifact.Title) &&
				bodyOnlyAddsAuthorStamps(artifact.Body, *req.Body)
			if !stampOnly {
				writeJSON(w, http.StatusConflict, noteConflictResponse{
					Conflict:    true,
					ServerBody:  artifact.Body,
					ServerTitle: artifact.Title,
				})
				return
			}
		}
	}

	title := artifact.Title
	if req.Title != nil {
		title = *req.Title
	}
	body := artifact.Body
	if req.Body != nil {
		body = *req.Body
	}
	updated, err := h.Upstream.UpdateArtifact(r.Context(), db.UpdateArtifactParams{
		ID:            noteID,
		WorkspaceID:   wsUUID,
		Title:         title,
		Body:          body,
		Metadata:      artifact.Metadata,
		FileUrl:       artifact.FileUrl,
		FileSizeBytes: artifact.FileSizeBytes,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update note")
		return
	}
	noteRow, err := h.Cerebro.GetNote(r.Context(), noteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update note")
		return
	}

	// Notify only people newly tagged by this edit (diff vs. the old body).
	h.notifyNoteMentions(r.Context(), wsUUID, noteID, ownerUUID, updated.Title, artifact.Body, updated.Body, noteRow.Visibility, "", "")

	// FIR-2810: advance per-line attribution — kept lines keep their author,
	// changed/added lines are credited to this caller.
	var lineAttrs []LineAttr
	if updated.Body != artifact.Body {
		lineAttrs = h.advanceAndSaveLineAttrs(r.Context(), noteID, artifact.Body, updated.Body, userID)
	} else {
		lineAttrs = h.ensureLineAttrs(r.Context(), noteID, updated.Body)
	}

	// Wave 3 / G2 — snapshot a version for the history. Coalesced: a burst of
	// edits by the same author becomes one entry (see snapshotVersion). Only
	// snapshot when something actually changed.
	if updated.Title != artifact.Title || updated.Body != artifact.Body {
		h.snapshotVersion(r.Context(), noteID, ownerUUID, "member", updated.Title, updated.Body, "edit", "", true)
	}

	writeJSON(w, http.StatusOK, NoteResponse{
		ID:          uuidStr(updated.ID),
		WorkspaceID: uuidStr(updated.WorkspaceID),
		FolderID:    uuidPtr(updated.FolderID),
		Title:       updated.Title,
		Body:        updated.Body,
		OwnerID:     uuidStr(noteRow.OwnerID),
		Visibility:  noteRow.Visibility,
		Pinned:      noteRow.Pinned,
		CanEdit:     true, // caller just passed requireCanEdit
		IssueID:     uuidPtr(updated.IssueID),
		ProjectID:   uuidPtr(updated.ProjectID),
		CreatedAt:   tsStr(updated.CreatedAt),
		UpdatedAt:   tsStr(updated.UpdatedAt),
		AuthorCodes: noteRow.AuthorCodes,
		LineAttrs:   lineAttrs,
	})
}

// SetVisibility changes who can see a note. Only the owner may change it. When
// visibility is 'shared' the share list is replaced with shared_user_ids.
func (h *Handler) SetVisibility(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	_, ownerUUID, ok := h.scope(w, r, userID)
	if !ok {
		return
	}
	noteID, ok := pathUUID(w, r)
	if !ok {
		return
	}
	if !h.requireOwner(w, r, noteID, ownerUUID) {
		return
	}

	var req setVisibilityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !validVisibility[req.Visibility] {
		writeError(w, http.StatusBadRequest, "invalid visibility; expected private|shared|workspace")
		return
	}
	if err := h.Cerebro.SetNoteVisibility(r.Context(), cerebrodb.SetNoteVisibilityParams{
		ArtifactID: noteID,
		Visibility: req.Visibility,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update visibility")
		return
	}
	// Replace shares: clear, then re-add for 'shared'.
	if err := h.Cerebro.ReplaceNoteShares(r.Context(), noteID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update shares")
		return
	}
	if req.Visibility == "shared" {
		for _, uid := range req.SharedUserIDs {
			u, err := util.ParseUUID(uid)
			if err != nil {
				continue
			}
			_ = h.Cerebro.AddNoteShare(r.Context(), cerebrodb.AddNoteShareParams{ArtifactID: noteID, UserID: u})
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"visibility": req.Visibility})
}

// SetPin pins or unpins a note for the fast list. Only the owner may pin.
func (h *Handler) SetPin(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	_, ownerUUID, ok := h.scope(w, r, userID)
	if !ok {
		return
	}
	noteID, ok := pathUUID(w, r)
	if !ok {
		return
	}
	if !h.requireOwner(w, r, noteID, ownerUUID) {
		return
	}
	var req setPinRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.Cerebro.SetNotePinned(r.Context(), cerebrodb.SetNotePinnedParams{
		ArtifactID: noteID,
		Pinned:     req.Pinned,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update pin")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"pinned": req.Pinned})
}

// SetAuthorCodes toggles the per-note "stamp the writer's member code on every
// line" behaviour (FIR-2810). Anyone who may edit the note may toggle it — the
// toggle changes how the note is written, not who can see it, and a
// business-review note is often owned by whoever created the recurring note
// rather than the person running the meeting.
func (h *Handler) SetAuthorCodes(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	_, ownerUUID, ok := h.scope(w, r, userID)
	if !ok {
		return
	}
	noteID, ok := pathUUID(w, r)
	if !ok {
		return
	}
	if !h.requireCanEdit(w, r, noteID, ownerUUID) {
		return
	}
	var req setAuthorCodesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.Cerebro.SetNoteAuthorCodes(r.Context(), cerebrodb.SetNoteAuthorCodesParams{
		ArtifactID:  noteID,
		AuthorCodes: req.AuthorCodes,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update author codes")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"author_codes": req.AuthorCodes})
}

// DeleteNote removes a note: the cerebro_note row, its shares, and the
// underlying artifact. Only the owner may delete.
func (h *Handler) DeleteNote(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ownerUUID, ok := h.scope(w, r, userID)
	if !ok {
		return
	}
	noteID, ok := pathUUID(w, r)
	if !ok {
		return
	}
	if !h.requireOwner(w, r, noteID, ownerUUID) {
		return
	}
	_ = h.Cerebro.ReplaceNoteShares(r.Context(), noteID)
	if err := h.Cerebro.DeleteNote(r.Context(), noteID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete note")
		return
	}
	if err := h.Upstream.DeleteArtifact(r.Context(), db.DeleteArtifactParams{ID: noteID, WorkspaceID: wsUUID}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete note")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListShares returns the user IDs a 'shared' note is shared with.
func (h *Handler) ListShares(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	_, ownerUUID, ok := h.scope(w, r, userID)
	if !ok {
		return
	}
	noteID, ok := pathUUID(w, r)
	if !ok {
		return
	}
	if !h.requireOwner(w, r, noteID, ownerUUID) {
		return
	}
	shares, err := h.Cerebro.ListNoteShares(r.Context(), noteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list shares")
		return
	}
	ids := make([]string, 0, len(shares))
	for _, s := range shares {
		ids = append(ids, uuidStr(s.UserID))
	}
	writeJSON(w, http.StatusOK, map[string][]string{"shared_user_ids": ids})
}

// --- helpers ---

// scope resolves the workspace + owner UUIDs from the request context and the
// authenticated user. Returns false (and writes an error) when either is bad.
func (h *Handler) scope(w http.ResponseWriter, r *http.Request, userID string) (pgtype.UUID, pgtype.UUID, bool) {
	wsID := middleware.WorkspaceIDFromContext(r.Context())
	if wsID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	wsUUID, err := util.ParseUUID(wsID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	ownerUUID, err := util.ParseUUID(userID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	return wsUUID, ownerUUID, true
}

// requireCanEdit returns true when the caller may edit and save the note.
// FIR-2595 Phase 4: edit/save is driven by folder access — the caller may write
// when they own the note (baseline; covers personal root notes) OR they hold an
// 'editor' / 'full_access' Collections grant on the note's folder or an
// ancestor. A 'viewer' grant is read-only. This is additive to the legacy
// owner-only gate: it only ever GRANTS write to folder editors.
func (h *Handler) requireCanEdit(w http.ResponseWriter, r *http.Request, noteID, callerUUID pgtype.UUID) bool {
	if _, err := h.Cerebro.GetNote(r.Context(), noteID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "note not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to load note")
		}
		return false
	}
	allowed, err := h.Cerebro.CanUserEditNote(r.Context(), cerebrodb.CanUserEditNoteParams{
		ArtifactID: noteID,
		OwnerID:    callerUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load note")
		return false
	}
	if !allowed {
		writeError(w, http.StatusForbidden, "you don't have edit access to this note")
		return false
	}
	return true
}

// requireOwner returns true only when the caller owns the note.
func (h *Handler) requireOwner(w http.ResponseWriter, r *http.Request, noteID, ownerUUID pgtype.UUID) bool {
	noteRow, err := h.Cerebro.GetNote(r.Context(), noteID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "note not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to load note")
		}
		return false
	}
	if uuidStr(noteRow.OwnerID) != uuidStr(ownerUUID) {
		writeError(w, http.StatusForbidden, "only the owner can change this note")
		return false
	}
	return true
}

// requireCanSaveVersion gates the manual "Save version" and "Restore" endpoints
// for BOTH notes and documents (FIR-2697). A note keeps its stricter, unchanged
// rule: only its owner may checkpoint or restore it (requireOwner). A plain
// document (no cerebro_note row) is instead gated by document edit access
// (CanUserEditArtifact) — folder-grant or the member author. This never loosens
// note behavior; it only extends save/restore to documents.
func (h *Handler) requireCanSaveVersion(w http.ResponseWriter, r *http.Request, artifactID, callerUUID pgtype.UUID) bool {
	if _, err := h.Cerebro.GetNote(r.Context(), artifactID); err == nil {
		// It is a Notes-feature note → preserve owner-only behavior.
		return h.requireOwner(w, r, artifactID, callerUUID)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "failed to load document")
		return false
	}
	// Plain document → edit access controls save/restore.
	allowed, err := h.Cerebro.CanUserEditArtifact(r.Context(), cerebrodb.CanUserEditArtifactParams{
		ID:    artifactID,
		PUser: callerUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load document")
		return false
	}
	if !allowed {
		writeError(w, http.StatusForbidden, "you don't have edit access to this document")
		return false
	}
	return true
}

func pathUUID(w http.ResponseWriter, r *http.Request) (pgtype.UUID, bool) {
	id := chi.URLParam(r, "id")
	u, err := util.ParseUUID(id)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid note id")
		return pgtype.UUID{}, false
	}
	return u, true
}

func pageParams(r *http.Request) (limit, offset int32) {
	limit = defaultListLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			if n > maxListLimit {
				n = maxListLimit
			}
			limit = int32(n)
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = int32(n)
		}
	}
	return limit, offset
}

func listRowToResponse(n cerebrodb.ListNotesForUserRow) NoteResponse {
	return NoteResponse{
		ID:           uuidStr(n.ID),
		WorkspaceID:  uuidStr(n.WorkspaceID),
		FolderID:     uuidPtr(n.FolderID),
		Title:        n.Title,
		Body:         n.Body,
		OwnerID:      uuidStr(n.OwnerID),
		Visibility:   n.Visibility,
		Pinned:       n.Pinned,
		CommentCount: n.CommentCount,
		CreatedAt:    tsStr(n.CreatedAt),
		UpdatedAt:    tsStr(n.UpdatedAt),
	}
}

func searchRowToResponse(n cerebrodb.SearchNotesForUserRow) NoteResponse {
	return NoteResponse{
		ID:           uuidStr(n.ID),
		WorkspaceID:  uuidStr(n.WorkspaceID),
		FolderID:     uuidPtr(n.FolderID),
		Title:        n.Title,
		Body:         n.Body,
		OwnerID:      uuidStr(n.OwnerID),
		Visibility:   n.Visibility,
		Pinned:       n.Pinned,
		CommentCount: n.CommentCount,
		CreatedAt:    tsStr(n.CreatedAt),
		UpdatedAt:    tsStr(n.UpdatedAt),
	}
}

func recentRowToResponse(n cerebrodb.ListRecentNotesForUserRow) NoteResponse {
	return NoteResponse{
		ID:           uuidStr(n.ID),
		WorkspaceID:  uuidStr(n.WorkspaceID),
		FolderID:     uuidPtr(n.FolderID),
		Title:        n.Title,
		Body:         n.Body,
		OwnerID:      uuidStr(n.OwnerID),
		Visibility:   n.Visibility,
		Pinned:       n.Pinned,
		CommentCount: n.CommentCount,
		CreatedAt:    tsStr(n.CreatedAt),
		UpdatedAt:    tsStr(n.UpdatedAt),
	}
}

func mustUUID(s string) pgtype.UUID {
	u, _ := util.ParseUUID(s)
	return u
}

func uuidStr(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	return uuid.UUID(u.Bytes).String()
}

func uuidPtr(u pgtype.UUID) *string {
	if !u.Valid {
		return nil
	}
	s := uuid.UUID(u.Bytes).String()
	return &s
}

func tsStr(t pgtype.Timestamptz) string {
	if !t.Valid {
		return ""
	}
	return t.Time.UTC().Format("2006-01-02T15:04:05Z07:00")
}

// tsPtr is the nullable variant of tsStr: it returns nil for a NULL timestamp
// (e.g. an unsent comment's sent_to_agent_at) so the client can distinguish
// "not yet sent" from a real timestamp, rather than collapsing both to "".
func tsPtr(t pgtype.Timestamptz) *string {
	if !t.Valid {
		return nil
	}
	s := t.Time.UTC().Format("2006-01-02T15:04:05Z07:00")
	return &s
}

func requireUserID(w http.ResponseWriter, r *http.Request) (string, bool) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return "", false
	}
	return userID, true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

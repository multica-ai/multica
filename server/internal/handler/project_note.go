package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// noteTitleMaxLen bounds the title in runes so a pasted document can't become
// one. The body is deliberately unbounded — a journal grows for months.
const noteTitleMaxLen = 200

// appendSeparator joins an appended chunk to existing body text. A blank line
// keeps consecutive markdown blocks from merging into one paragraph.
const appendSeparator = "\n\n"

// ProjectNoteResponse is the JSON shape returned by the project note API.
//
// BodyMd is omitted from list responses (see projectNoteToSummary): a project
// may hold dozens of long notes and the list view only needs titles, so
// shipping every body on every list call would waste bandwidth for nothing.
type ProjectNoteResponse struct {
	ID          string  `json:"id"`
	ProjectID   string  `json:"project_id"`
	WorkspaceID string  `json:"workspace_id"`
	Title       string  `json:"title"`
	BodyMd      string  `json:"body_md"`
	Position    int32   `json:"position"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
	CreatedBy   *string `json:"created_by"`
}

// ProjectNoteSummary is the list-view shape: everything except the body, plus a
// byte length so the UI can show "12 KB" without transferring the content.
type ProjectNoteSummary struct {
	ID          string  `json:"id"`
	ProjectID   string  `json:"project_id"`
	WorkspaceID string  `json:"workspace_id"`
	Title       string  `json:"title"`
	BodySize    int     `json:"body_size"`
	Position    int32   `json:"position"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
	CreatedBy   *string `json:"created_by"`
}

func projectNoteToResponse(n db.ProjectNote) ProjectNoteResponse {
	return ProjectNoteResponse{
		ID:          uuidToString(n.ID),
		ProjectID:   uuidToString(n.ProjectID),
		WorkspaceID: uuidToString(n.WorkspaceID),
		Title:       n.Title,
		BodyMd:      n.BodyMd,
		Position:    n.Position,
		CreatedAt:   timestampToString(n.CreatedAt),
		UpdatedAt:   timestampToString(n.UpdatedAt),
		CreatedBy:   uuidToPtr(n.CreatedBy),
	}
}

// projectNoteToSummary builds the list row from a note the server already has
// in full. Used when a write echoes back the updated note.
func projectNoteToSummary(n db.ProjectNote) ProjectNoteSummary {
	return ProjectNoteSummary{
		ID:          uuidToString(n.ID),
		ProjectID:   uuidToString(n.ProjectID),
		WorkspaceID: uuidToString(n.WorkspaceID),
		Title:       n.Title,
		BodySize:    len(n.BodyMd),
		Position:    n.Position,
		CreatedAt:   timestampToString(n.CreatedAt),
		UpdatedAt:   timestampToString(n.UpdatedAt),
		CreatedBy:   uuidToPtr(n.CreatedBy),
	}
}

// projectNoteRowToSummary maps the list query's row, whose BodySize comes from
// octet_length in SQL so the body never leaves the database.
func projectNoteRowToSummary(n db.ListProjectNotesRow) ProjectNoteSummary {
	return ProjectNoteSummary{
		ID:          uuidToString(n.ID),
		ProjectID:   uuidToString(n.ProjectID),
		WorkspaceID: uuidToString(n.WorkspaceID),
		Title:       n.Title,
		BodySize:    int(n.BodySize),
		Position:    n.Position,
		CreatedAt:   timestampToString(n.CreatedAt),
		UpdatedAt:   timestampToString(n.UpdatedAt),
		CreatedBy:   uuidToPtr(n.CreatedBy),
	}
}

// CreateProjectNoteRequest is the body for POST /api/projects/{id}/notes.
// body_md is optional so "create an empty notepad" is a title-only call.
type CreateProjectNoteRequest struct {
	Title    string  `json:"title"`
	BodyMd   *string `json:"body_md"`
	Position *int32  `json:"position"`
}

// UpdateProjectNoteRequest is the body for PATCH /api/projects/{id}/notes/{noteId}.
// Every field is optional; omitted fields keep their current value. Sending
// body_md replaces the whole body — use the append endpoint to add to it.
type UpdateProjectNoteRequest struct {
	Title    *string `json:"title"`
	BodyMd   *string `json:"body_md"`
	Position *int32  `json:"position"`
}

// AppendProjectNoteRequest is the body for POST /api/projects/{id}/notes/{noteId}/append.
type AppendProjectNoteRequest struct {
	BodyMd string `json:"body_md"`
}

// normalizeNoteTitle trims and length-checks a title. An empty title is
// rejected rather than defaulted: an untitled note in a list of twenty is
// unfindable, and the caller always knows what it is creating.
//
// The limit counts runes, not bytes: len() would cap a Chinese title at 66
// characters while the error message promises 200, and a user typing a normal
// title in a non-Latin script would hit a wall with no way to understand why.
func normalizeNoteTitle(raw string) (string, bool) {
	title := strings.TrimSpace(raw)
	if title == "" || utf8.RuneCountInString(title) > noteTitleMaxLen {
		return "", false
	}
	return title, true
}

// loadNoteForProject resolves a note by id and verifies it belongs to both the
// caller's workspace and the project in the URL. The double check matters: the
// workspace filter alone would let a member read a note from a different
// project by guessing its id.
func (h *Handler) loadNoteForProject(w http.ResponseWriter, r *http.Request, project db.Project) (db.ProjectNote, bool) {
	noteUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "noteId"), "note id")
	if !ok {
		return db.ProjectNote{}, false
	}
	note, err := h.Queries.GetProjectNoteInWorkspace(r.Context(), db.GetProjectNoteInWorkspaceParams{
		ID: noteUUID, WorkspaceID: project.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "project note not found")
		return db.ProjectNote{}, false
	}
	if uuidToString(note.ProjectID) != uuidToString(project.ID) {
		writeError(w, http.StatusNotFound, "project note not found")
		return db.ProjectNote{}, false
	}
	return note, true
}

// ListProjectNotes returns the notes attached to a project, bodies excluded.
func (h *Handler) ListProjectNotes(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	notes, err := h.Queries.ListProjectNotes(r.Context(), project.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list project notes")
		return
	}
	resp := make([]ProjectNoteSummary, len(notes))
	for i, n := range notes {
		resp[i] = projectNoteRowToSummary(n)
	}
	writeJSON(w, http.StatusOK, map[string]any{"notes": resp, "total": len(resp)})
}

// GetProjectNote returns a single note including its full body.
func (h *Handler) GetProjectNote(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	note, ok := h.loadNoteForProject(w, r, project)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, projectNoteToResponse(note))
}

// CreateProjectNote adds a new notepad to a project.
func (h *Handler) CreateProjectNote(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req CreateProjectNoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	title, ok := normalizeNoteTitle(req.Title)
	if !ok {
		writeError(w, http.StatusBadRequest, "title is required and must be at most 200 characters")
		return
	}
	var body string
	if req.BodyMd != nil {
		body = *req.BodyMd
	}
	var position int32
	if req.Position != nil {
		position = *req.Position
	} else {
		// Append after existing notes. MaxProjectNotePosition returns -1 for an
		// empty project, so a first note lands at 0.
		maxPos, err := h.Queries.MaxProjectNotePosition(r.Context(), project.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to determine note position")
			return
		}
		position = maxPos + 1
	}

	creator, _ := h.parseUserUUIDOrZero(userID)
	note, err := h.Queries.CreateProjectNote(r.Context(), db.CreateProjectNoteParams{
		ProjectID:   project.ID,
		WorkspaceID: project.WorkspaceID,
		Title:       title,
		BodyMd:      body,
		Position:    position,
		CreatedBy:   creator,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create project note")
		return
	}

	resp := projectNoteToResponse(note)
	h.publish(
		protocol.EventProjectNoteCreated,
		uuidToString(project.WorkspaceID),
		"member",
		userID,
		map[string]any{"note": projectNoteToSummary(note), "project_id": uuidToString(project.ID)},
	)
	writeJSON(w, http.StatusCreated, resp)
}

// UpdateProjectNote edits a note's title, body, or position. Sending body_md
// replaces the entire body; callers adding to a journal should use the append
// endpoint instead so concurrent writers don't clobber each other.
func (h *Handler) UpdateProjectNote(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	note, ok := h.loadNoteForProject(w, r, project)
	if !ok {
		return
	}
	var req UpdateProjectNoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	title := note.Title
	if req.Title != nil {
		normalized, ok := normalizeNoteTitle(*req.Title)
		if !ok {
			writeError(w, http.StatusBadRequest, "title must be non-empty and at most 200 characters")
			return
		}
		title = normalized
	}
	body := note.BodyMd
	if req.BodyMd != nil {
		body = *req.BodyMd
	}
	position := note.Position
	if req.Position != nil {
		position = *req.Position
	}

	updated, err := h.Queries.UpdateProjectNote(r.Context(), db.UpdateProjectNoteParams{
		ID:       note.ID,
		Title:    title,
		BodyMd:   body,
		Position: position,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update project note")
		return
	}

	resp := projectNoteToResponse(updated)
	h.publish(
		protocol.EventProjectNoteUpdated,
		uuidToString(project.WorkspaceID),
		"member",
		userID,
		map[string]any{"note": projectNoteToSummary(updated), "project_id": uuidToString(project.ID)},
	)
	writeJSON(w, http.StatusOK, resp)
}

// AppendProjectNote concatenates text onto a note's body. This is the primary
// write path for agents keeping a journal: the concatenation happens in SQL, so
// two appends racing each other both survive instead of one overwriting the
// other's read-modify-write.
func (h *Handler) AppendProjectNote(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	note, ok := h.loadNoteForProject(w, r, project)
	if !ok {
		return
	}
	var req AppendProjectNoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.BodyMd) == "" {
		writeError(w, http.StatusBadRequest, "body_md is required")
		return
	}

	updated, err := h.Queries.AppendProjectNote(r.Context(), db.AppendProjectNoteParams{
		ID:        note.ID,
		BodyMd:    req.BodyMd,
		Separator: appendSeparator,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to append to project note")
		return
	}

	resp := projectNoteToResponse(updated)
	h.publish(
		protocol.EventProjectNoteUpdated,
		uuidToString(project.WorkspaceID),
		"member",
		userID,
		map[string]any{"note": projectNoteToSummary(updated), "project_id": uuidToString(project.ID)},
	)
	writeJSON(w, http.StatusOK, resp)
}

// DeleteProjectNote removes a note from a project.
func (h *Handler) DeleteProjectNote(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	note, ok := h.loadNoteForProject(w, r, project)
	if !ok {
		return
	}
	if err := h.Queries.DeleteProjectNote(r.Context(), note.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete project note")
		return
	}
	h.publish(
		protocol.EventProjectNoteDeleted,
		uuidToString(project.WorkspaceID),
		"member",
		userID,
		map[string]any{
			"project_id": uuidToString(project.ID),
			"note_id":    uuidToString(note.ID),
		},
	)
	w.WriteHeader(http.StatusNoContent)
}

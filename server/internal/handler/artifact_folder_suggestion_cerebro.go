package handler

// CEREBRO-PATCH(folder-suggestion): FIR-2697 part 2 — folder suggestions with
// human accept. An agent that files a document/note may PROPOSE an existing
// folder (any folder other than the automatic Agent Runs structure) instead of
// moving the artifact outright. The artifact only moves once a human accepts
// the proposal, so an agent never relocates a person's document without
// sign-off. Notes and Documents keep separate folder trees
// (artifact_folder.kind), so a proposal records its surface and the accept path
// rejects a cross-surface move.
//
// The proposals live in the cerebro-only table cerebro_artifact_folder_suggestion
// (migration 9121). This handler reaches it with raw SQL through h.DB because
// the shared sqlc generate step is currently blocked by unrelated broken query
// files; keeping the access hand-written here avoids faking generated code and
// keeps the whole feature in one reviewable place. The move itself reuses the
// upstream MoveArtifactToFolder query, and the accept path republishes
// artifact:updated so the version listener and realtime clients refresh.
//
// Gated by the workspace feature flag cerebro_folder_suggestions (default off).

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// folderSuggestionsFlagKey is the workspace kill switch for the whole folder
// suggestion feature. Mirrored in the TypeScript registry
// (packages/cerebro-feature-flags/registry.ts).
const folderSuggestionsFlagKey = "cerebro_folder_suggestions"

// Event types for suggestion lifecycle. Defined here (not in pkg/protocol) to
// stay merge-additive, matching where the artifact folder events live.
const (
	EventArtifactFolderSuggestionCreated  = "artifact_folder_suggestion:created"
	EventArtifactFolderSuggestionResolved = "artifact_folder_suggestion:resolved"
)

// folderSuggestionsEnabled reports whether the feature is switched ON for this
// workspace. The flag default is OFF (no stored row), so a missing override or
// a lookup error resolves to OFF — the feature stays sealed unless an admin
// explicitly enabled it.
func (h *Handler) folderSuggestionsEnabled(ctx context.Context, wsID pgtype.UUID) bool {
	if h.CerebroQueries == nil {
		return false
	}
	enabled, err := h.CerebroQueries.GetCerebroFeatureFlag(ctx, cerebrodb.GetCerebroFeatureFlagParams{
		WorkspaceID: wsID,
		UserID:      pgtype.UUID{Valid: true}, // all-zero sentinel = workspace-level row
		FlagKey:     folderSuggestionsFlagKey,
	})
	if err != nil {
		return false
	}
	return enabled
}

// ArtifactFolderSuggestionResponse is the wire shape of a proposal. folder_name
// is populated on the reads that join the target folder (empty otherwise).
type ArtifactFolderSuggestionResponse struct {
	ID              string  `json:"id"`
	WorkspaceID     string  `json:"workspace_id"`
	ArtifactID      string  `json:"artifact_id"`
	ArtifactTitle   string  `json:"artifact_title,omitempty"`
	FolderID        string  `json:"folder_id"`
	FolderName      string  `json:"folder_name,omitempty"`
	Surface         string  `json:"surface"`
	Status          string  `json:"status"`
	Reason          string  `json:"reason"`
	SuggestedByType string  `json:"suggested_by_type"`
	SuggestedByID   string  `json:"suggested_by_id"`
	ResolvedByID    *string `json:"resolved_by_id"`
	ResolvedAt      *string `json:"resolved_at"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
}

// surfaceForArtifact maps an artifact to the folder tree it belongs to. A note
// (kind='note', it carries a cerebro_note row) lives in the note tree; anything
// else is a document. This mirrors artifact_folder.kind.
func surfaceForArtifact(a db.Artifact) string {
	if a.Kind == "note" {
		return "note"
	}
	return "document"
}

// folderKindOrDefault treats an empty folder kind as "document" (the historical
// default for folders created before TECH-3637 added the column).
func folderKindOrDefault(f db.ArtifactFolder) string {
	if f.Kind == "" {
		return "document"
	}
	return f.Kind
}

// ---------------------------------------------------------------------------
// CreateArtifactFolderSuggestion — POST /api/artifacts/{id}/folder-suggestion
// ---------------------------------------------------------------------------

type CreateArtifactFolderSuggestionRequest struct {
	FolderID string `json:"folder_id"`
	Reason   string `json:"reason"`
}

func (h *Handler) CreateArtifactFolderSuggestion(w http.ResponseWriter, r *http.Request) {
	artifactID := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	if !h.folderSuggestionsEnabled(r.Context(), parseUUID(workspaceID)) {
		writeError(w, http.StatusForbidden, "folder suggestions are not enabled for this workspace")
		return
	}

	artifact, err := h.Queries.GetArtifact(r.Context(), db.GetArtifactParams{
		ID:          parseUUID(artifactID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "artifact not found")
		return
	}

	var req CreateArtifactFolderSuggestionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.FolderID == "" {
		writeError(w, http.StatusBadRequest, "folder_id is required")
		return
	}
	folderUUID, ok := parseUUIDOrBadRequest(w, req.FolderID, "folder_id")
	if !ok {
		return
	}

	folder, err := h.Queries.GetArtifactFolder(r.Context(), db.GetArtifactFolderParams{
		ID:          folderUUID,
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "folder not found")
		return
	}

	// A proposal only makes sense within one surface's tree: a note may only be
	// proposed into a note folder, a document into a document folder.
	surface := surfaceForArtifact(artifact)
	if folderKindOrDefault(folder) != surface {
		writeError(w, http.StatusBadRequest, "folder belongs to a different surface (notes vs documents)")
		return
	}

	// Proposing the folder the artifact already lives in is a no-op.
	if artifact.FolderID.Valid && uuidToString(artifact.FolderID) == uuidToString(folder.ID) {
		writeError(w, http.StatusBadRequest, "artifact is already in this folder")
		return
	}

	suggestedByType, suggestedByID := h.resolveActor(r, userID, workspaceID)

	// Retire any live proposal first so the partial unique index never trips,
	// then insert the fresh one — in a single tx so a concurrent create can't
	// leave two pending rows.
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record suggestion")
		return
	}
	defer tx.Rollback(r.Context())

	if _, err := tx.Exec(r.Context(),
		`UPDATE cerebro_artifact_folder_suggestion
		 SET status = 'superseded', updated_at = now()
		 WHERE artifact_id = $1 AND status = 'pending'`,
		artifact.ID,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record suggestion")
		return
	}

	newID, _ := uuid.NewV7()
	row := tx.QueryRow(r.Context(),
		`INSERT INTO cerebro_artifact_folder_suggestion
		   (id, workspace_id, artifact_id, folder_id, surface, reason, suggested_by_type, suggested_by_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING id, workspace_id, artifact_id, folder_id, surface, status, reason,
		           suggested_by_type, suggested_by_id, resolved_by_id, resolved_at, created_at, updated_at`,
		pgtype.UUID{Bytes: newID, Valid: true},
		artifact.WorkspaceID, artifact.ID, folder.ID, surface, req.Reason,
		suggestedByType, parseUUID(suggestedByID),
	)
	resp, err := scanSuggestion(row)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record suggestion")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record suggestion")
		return
	}
	resp.FolderName = folder.Name
	resp.ArtifactTitle = artifact.Title

	h.publish(EventArtifactFolderSuggestionCreated, workspaceID, suggestedByType, suggestedByID, map[string]any{
		"suggestion": resp,
	})
	writeJSON(w, http.StatusCreated, resp)
}

// ---------------------------------------------------------------------------
// GetArtifactFolderSuggestion — GET /api/artifacts/{id}/folder-suggestion
// Returns the live proposal for an artifact (or {"suggestion": null}). Only a
// caller who can see the target folder gets it — they are the ones who could
// accept it, and it avoids leaking a private folder's name.
// ---------------------------------------------------------------------------

func (h *Handler) GetArtifactFolderSuggestion(w http.ResponseWriter, r *http.Request) {
	artifactID := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	if !h.folderSuggestionsEnabled(r.Context(), parseUUID(workspaceID)) {
		writeJSON(w, http.StatusOK, map[string]any{"suggestion": nil})
		return
	}

	row := h.DB.QueryRow(r.Context(),
		`SELECT s.id, s.workspace_id, s.artifact_id, s.folder_id, s.surface, s.status, s.reason,
		        s.suggested_by_type, s.suggested_by_id, s.resolved_by_id, s.resolved_at,
		        s.created_at, s.updated_at, f.name
		 FROM cerebro_artifact_folder_suggestion s
		 JOIN artifact_folder f ON f.id = s.folder_id
		 WHERE s.artifact_id = $1 AND s.workspace_id = $2 AND s.status = 'pending'
		 ORDER BY s.created_at DESC
		 LIMIT 1`,
		parseUUID(artifactID), parseUUID(workspaceID),
	)
	resp, folderID, err := scanSuggestionWithFolderName(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusOK, map[string]any{"suggestion": nil})
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load suggestion")
		return
	}
	if !h.folderVisibleToCaller(r, folderID) {
		writeJSON(w, http.StatusOK, map[string]any{"suggestion": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"suggestion": resp})
}

// ---------------------------------------------------------------------------
// ListArtifactFolderSuggestions — GET /api/artifact-folder-suggestions
// Review inbox: every live proposal in the workspace the caller can act on.
// Optional ?surface=document|note scopes to one tree.
// ---------------------------------------------------------------------------

func (h *Handler) ListArtifactFolderSuggestions(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	if !h.folderSuggestionsEnabled(r.Context(), parseUUID(workspaceID)) {
		writeJSON(w, http.StatusOK, []ArtifactFolderSuggestionResponse{})
		return
	}
	surface := r.URL.Query().Get("surface")

	rows, err := h.DB.Query(r.Context(),
		`SELECT s.id, s.workspace_id, s.artifact_id, s.folder_id, s.surface, s.status, s.reason,
		        s.suggested_by_type, s.suggested_by_id, s.resolved_by_id, s.resolved_at,
		        s.created_at, s.updated_at, f.name, a.title
		 FROM cerebro_artifact_folder_suggestion s
		 JOIN artifact_folder f ON f.id = s.folder_id
		 JOIN artifact a ON a.id = s.artifact_id
		 WHERE s.workspace_id = $1 AND s.status = 'pending'
		   AND ($2 = '' OR s.surface = $2)
		 ORDER BY s.created_at DESC`,
		parseUUID(workspaceID), surface,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list suggestions")
		return
	}
	defer rows.Close()

	out := []ArtifactFolderSuggestionResponse{}
	for rows.Next() {
		var s ArtifactFolderSuggestionResponse
		var folderID, artifactID, workspaceUUID, suggestedBy pgtype.UUID
		var resolvedBy pgtype.UUID
		var resolvedAt, createdAt, updatedAt pgtype.Timestamptz
		var folderName, artifactTitle string
		if err := rows.Scan(
			&s.ID, &workspaceUUID, &artifactID, &folderID, &s.Surface, &s.Status, &s.Reason,
			&s.SuggestedByType, &suggestedBy, &resolvedBy, &resolvedAt,
			&createdAt, &updatedAt, &folderName, &artifactTitle,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list suggestions")
			return
		}
		// Only surface proposals into folders the caller can see/act on.
		if !h.folderVisibleToCaller(r, folderID) {
			continue
		}
		s.WorkspaceID = uuidToString(workspaceUUID)
		s.ArtifactID = uuidToString(artifactID)
		s.FolderID = uuidToString(folderID)
		s.SuggestedByID = uuidToString(suggestedBy)
		s.FolderName = folderName
		s.ArtifactTitle = artifactTitle
		s.CreatedAt = timestampToString(createdAt)
		s.UpdatedAt = timestampToString(updatedAt)
		if resolvedBy.Valid {
			v := uuidToString(resolvedBy)
			s.ResolvedByID = &v
		}
		if resolvedAt.Valid {
			v := timestampToString(resolvedAt)
			s.ResolvedAt = &v
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list suggestions")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// ---------------------------------------------------------------------------
// AcceptArtifactFolderSuggestion — POST /api/artifact-folder-suggestions/{id}/accept
// A human accepts a proposal: the artifact moves to the folder and the proposal
// is marked accepted. Agents may not accept — the accept is the human gate.
// ---------------------------------------------------------------------------

func (h *Handler) AcceptArtifactFolderSuggestion(w http.ResponseWriter, r *http.Request) {
	h.resolveArtifactFolderSuggestion(w, r, "accepted")
}

// RejectArtifactFolderSuggestion — POST /api/artifact-folder-suggestions/{id}/reject
func (h *Handler) RejectArtifactFolderSuggestion(w http.ResponseWriter, r *http.Request) {
	h.resolveArtifactFolderSuggestion(w, r, "rejected")
}

func (h *Handler) resolveArtifactFolderSuggestion(w http.ResponseWriter, r *http.Request, decision string) {
	suggestionID := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	if !h.folderSuggestionsEnabled(r.Context(), parseUUID(workspaceID)) {
		writeError(w, http.StatusForbidden, "folder suggestions are not enabled for this workspace")
		return
	}

	// Accept/reject is a human governance action; an agent proposes, a person
	// decides. Reject an agent-actor caller.
	actorType, _ := h.resolveActor(r, userID, workspaceID)
	if actorType != "member" {
		writeError(w, http.StatusForbidden, "only a person can accept or reject a folder suggestion")
		return
	}

	// Load the pending proposal.
	row := h.DB.QueryRow(r.Context(),
		`SELECT id, workspace_id, artifact_id, folder_id, surface, status, reason,
		        suggested_by_type, suggested_by_id, resolved_by_id, resolved_at, created_at, updated_at
		 FROM cerebro_artifact_folder_suggestion
		 WHERE id = $1 AND workspace_id = $2`,
		parseUUID(suggestionID), parseUUID(workspaceID),
	)
	suggestion, err := scanSuggestion(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "suggestion not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load suggestion")
		return
	}
	if suggestion.Status != "pending" {
		writeError(w, http.StatusConflict, "suggestion has already been resolved")
		return
	}

	// The accepter must be able to see the target folder and to edit the
	// artifact (or be a workspace admin). This keeps a member from moving a
	// document they cannot touch, or into a folder they cannot see.
	targetFolderID := parseUUID(suggestion.FolderID)
	isAdmin := roleAllowed(member.Role, "owner", "admin")
	if decision == "accepted" && !h.folderVisibleToCaller(r, targetFolderID) {
		writeError(w, http.StatusForbidden, "you cannot move into a folder you do not have access to")
		return
	}
	if !isAdmin {
		canEdit, err := h.CerebroQueries.CanUserEditArtifact(r.Context(), cerebrodb.CanUserEditArtifactParams{
			ID:    parseUUID(suggestion.ArtifactID),
			PUser: parseUUID(userID),
		})
		if err != nil || !canEdit {
			writeError(w, http.StatusForbidden, "not authorized to resolve this suggestion")
			return
		}
	}

	// Resolve atomically: claim the pending row and, on accept, move the
	// artifact in the SAME transaction. Move-then-flip as two separate calls had
	// a race — two people acting on one proposal at once (one Accept, one
	// Dismiss) could both pass the pending pre-check; the accepter's move would
	// land while its flip lost the still-pending guard, leaving a document that
	// is really moved but logged as "rejected". Claiming the row first and doing
	// the move inside the same tx closes that window (same pattern as create).
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve suggestion")
		return
	}
	defer tx.Rollback(r.Context())

	// Claim the resolution: only the first caller flips the still-pending row; a
	// concurrent second caller matches zero rows and gets a 409, and never moves.
	resRow := tx.QueryRow(r.Context(),
		`UPDATE cerebro_artifact_folder_suggestion
		 SET status = $3, resolved_by_id = $4, resolved_at = now(), updated_at = now()
		 WHERE id = $1 AND workspace_id = $2 AND status = 'pending'
		 RETURNING id, workspace_id, artifact_id, folder_id, surface, status, reason,
		           suggested_by_type, suggested_by_id, resolved_by_id, resolved_at, created_at, updated_at`,
		parseUUID(suggestionID), parseUUID(workspaceID), decision, parseUUID(userID),
	)
	resolved, err := scanSuggestion(resRow)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusConflict, "suggestion has already been resolved")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to resolve suggestion")
		return
	}

	// Only the caller that won the claim moves the artifact, and only inside the
	// same tx — so the move and the accepted status commit together or not at all.
	if decision == "accepted" {
		if _, err := h.Queries.WithTx(tx).MoveArtifactToFolder(r.Context(), db.MoveArtifactToFolderParams{
			ID:          parseUUID(suggestion.ArtifactID),
			WorkspaceID: parseUUID(workspaceID),
			FolderID:    targetFolderID,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to move artifact")
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve suggestion")
		return
	}

	// Republish the artifact so the version listener snapshots the moved state
	// and realtime clients (the editor) refresh. Best-effort read.
	if decision == "accepted" {
		if updated, err := h.Queries.GetArtifact(r.Context(), db.GetArtifactParams{
			ID:          parseUUID(suggestion.ArtifactID),
			WorkspaceID: parseUUID(workspaceID),
		}); err == nil {
			h.publish(EventArtifactUpdated, workspaceID, "member", userID, map[string]any{
				"artifact": artifactToResponse(updated),
			})
		}
	}
	h.publish(EventArtifactFolderSuggestionResolved, workspaceID, "member", userID, map[string]any{
		"suggestion": resolved,
	})
	writeJSON(w, http.StatusOK, resolved)
}

// ---------------------------------------------------------------------------
// scan helpers (see below)
// ---------------------------------------------------------------------------

// scanSuggestion reads the 13 base columns (no joins) into a response.
func scanSuggestion(row pgx.Row) (ArtifactFolderSuggestionResponse, error) {
	var s ArtifactFolderSuggestionResponse
	var workspaceUUID, artifactID, folderID, suggestedBy, resolvedBy pgtype.UUID
	var resolvedAt, createdAt, updatedAt pgtype.Timestamptz
	if err := row.Scan(
		&s.ID, &workspaceUUID, &artifactID, &folderID, &s.Surface, &s.Status, &s.Reason,
		&s.SuggestedByType, &suggestedBy, &resolvedBy, &resolvedAt, &createdAt, &updatedAt,
	); err != nil {
		return ArtifactFolderSuggestionResponse{}, err
	}
	s.WorkspaceID = uuidToString(workspaceUUID)
	s.ArtifactID = uuidToString(artifactID)
	s.FolderID = uuidToString(folderID)
	s.SuggestedByID = uuidToString(suggestedBy)
	s.CreatedAt = timestampToString(createdAt)
	s.UpdatedAt = timestampToString(updatedAt)
	if resolvedBy.Valid {
		v := uuidToString(resolvedBy)
		s.ResolvedByID = &v
	}
	if resolvedAt.Valid {
		v := timestampToString(resolvedAt)
		s.ResolvedAt = &v
	}
	return s, nil
}

// scanSuggestionWithFolderName reads the base columns plus a trailing folder
// name, and returns the folder UUID separately so the caller can gate on
// visibility.
func scanSuggestionWithFolderName(row pgx.Row) (ArtifactFolderSuggestionResponse, pgtype.UUID, error) {
	var s ArtifactFolderSuggestionResponse
	var workspaceUUID, artifactID, folderID, suggestedBy, resolvedBy pgtype.UUID
	var resolvedAt, createdAt, updatedAt pgtype.Timestamptz
	var folderName string
	if err := row.Scan(
		&s.ID, &workspaceUUID, &artifactID, &folderID, &s.Surface, &s.Status, &s.Reason,
		&s.SuggestedByType, &suggestedBy, &resolvedBy, &resolvedAt, &createdAt, &updatedAt, &folderName,
	); err != nil {
		return ArtifactFolderSuggestionResponse{}, pgtype.UUID{}, err
	}
	s.WorkspaceID = uuidToString(workspaceUUID)
	s.ArtifactID = uuidToString(artifactID)
	s.FolderID = uuidToString(folderID)
	s.SuggestedByID = uuidToString(suggestedBy)
	s.FolderName = folderName
	s.CreatedAt = timestampToString(createdAt)
	s.UpdatedAt = timestampToString(updatedAt)
	if resolvedBy.Valid {
		v := uuidToString(resolvedBy)
		s.ResolvedByID = &v
	}
	if resolvedAt.Valid {
		v := timestampToString(resolvedAt)
		s.ResolvedAt = &v
	}
	return s, folderID, nil
}

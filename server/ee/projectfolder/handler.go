// Package projectfolder groups a workspace's projects one level deep
// (MUL-9390), a Business plan feature.
//
// Every member can create, rename and delete folders and file projects in
// them; a folder carries no access of its own. Creating a folder and filing a
// project in one add to the feature; renaming, moving a project out and
// deleting never do, so a workspace whose plan lacks folders keeps the ones
// it has and can still tidy them up.
package projectfolder

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/ee/projectfolder/folderdb"
	"github.com/multica-ai/multica/server/internal/extension"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	EventCreated = "project_folder:created"
	EventUpdated = "project_folder:updated"
	EventDeleted = "project_folder:deleted"
	// EventFiled is a project moved into, between or out of folders.
	EventFiled = "project_folder:filed"
)

const maxNameLen = 64

func init() {
	extension.RegisterWorkspaceRoutes(Mount)
	// Filing names the project, which may be private.
	extension.RegisterProjectEvent(EventFiled)
}

// Mount registers the folder routes on r.
func Mount(r chi.Router, h *handler.Handler) {
	s := New(h)
	r.Route("/api/project-folders", func(r chi.Router) {
		r.Get("/", s.List)
		r.Post("/", s.Create)
		r.Post("/file", s.File)
		r.Route("/{id}", func(r chi.Router) {
			r.Patch("/", s.Rename)
			r.Delete("/", s.Delete)
		})
	})
}

type Service struct {
	h *handler.Handler
	q *folderdb.Queries
}

func New(h *handler.Handler) *Service {
	return &Service{h: h, q: folderdb.New(h.DB)}
}

type FolderResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	Name        string  `json:"name"`
	CreatorType *string `json:"creator_type"`
	CreatorID   *string `json:"creator_id"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

func folderToResponse(f folderdb.ProjectFolder) FolderResponse {
	return FolderResponse{
		ID:          util.UUIDToString(f.ID),
		WorkspaceID: util.UUIDToString(f.WorkspaceID),
		Name:        f.Name,
		CreatorType: textPtr(f.CreatorType),
		CreatorID:   uuidPtr(f.CreatorID),
		CreatedAt:   util.TimestampToString(f.CreatedAt),
		UpdatedAt:   util.TimestampToString(f.UpdatedAt),
	}
}

// FiledProject places one project the caller can see in a folder.
type FiledProject struct {
	ProjectID string `json:"project_id"`
	FolderID  string `json:"folder_id"`
}

type ListResponse struct {
	Folders []FolderResponse `json:"folders"`
	Filed   []FiledProject   `json:"filed"`
}

func validateName(raw string) (string, error) {
	for _, r := range raw {
		if unicode.IsControl(r) {
			return "", errors.New("name cannot contain tabs, newlines, or control characters")
		}
	}
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", errors.New("name is required")
	}
	if utf8.RuneCountInString(name) > maxNameLen {
		return "", errors.New("name must be 64 characters or fewer")
	}
	return name, nil
}

func decodeName(w http.ResponseWriter, r *http.Request) (string, bool) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handler.WriteError(w, http.StatusBadRequest, "invalid request body")
		return "", false
	}
	name, err := validateName(req.Name)
	if err != nil {
		handler.WriteError(w, http.StatusBadRequest, err.Error())
		return "", false
	}
	return name, true
}

// List returns the workspace's folders and where each project the caller can
// see is filed. Private projects the caller cannot see are left out.
func (s *Service) List(w http.ResponseWriter, r *http.Request) {
	workspaceID := s.h.WorkspaceID(r)
	wsUUID, ok := handler.ParseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	folders, err := s.q.ListProjectFolders(r.Context(), wsUUID)
	if err != nil {
		slog.Warn("ListProjectFolders failed", append(logger.RequestAttrs(r), "error", err)...)
		handler.WriteError(w, http.StatusInternalServerError, "failed to list project folders")
		return
	}
	filed, err := s.q.ListFiledProjects(r.Context(), wsUUID)
	if err != nil {
		slog.Warn("ListFiledProjects failed", append(logger.RequestAttrs(r), "error", err)...)
		handler.WriteError(w, http.StatusInternalServerError, "failed to list project folders")
		return
	}
	hidden, ok := s.h.HiddenProjects(w, r, workspaceID)
	if !ok {
		return
	}
	resp := ListResponse{Folders: make([]FolderResponse, len(folders)), Filed: make([]FiledProject, 0, len(filed))}
	for i, f := range folders {
		resp.Folders[i] = folderToResponse(f)
	}
	for _, p := range filed {
		if hidden.Empty() || hidden.CanSeeProject(p.ID) {
			resp.Filed = append(resp.Filed, FiledProject{ProjectID: util.UUIDToString(p.ID), FolderID: util.UUIDToString(p.FolderID)})
		}
	}
	handler.WriteJSON(w, http.StatusOK, resp)
}

func (s *Service) Create(w http.ResponseWriter, r *http.Request) {
	name, ok := decodeName(w, r)
	if !ok {
		return
	}
	workspaceID := s.h.WorkspaceID(r)
	wsUUID, ok := handler.ParseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	userID, ok := handler.RequireUserID(w, r)
	if !ok {
		return
	}
	actorType, actorID := s.h.Actor(r, userID, workspaceID)
	creatorID, ok := handler.ParseUUIDOrBadRequest(w, actorID, "creator_id")
	if !ok {
		return
	}
	folder, err := s.q.CreateProjectFolder(r.Context(), folderdb.CreateProjectFolderParams{
		WorkspaceID: wsUUID,
		Name:        name,
		CreatorType: pgtype.Text{String: actorType, Valid: true},
		CreatorID:   creatorID,
	})
	if err != nil {
		if handler.IsUniqueViolation(err) {
			handler.WriteError(w, http.StatusConflict, "a folder with that name already exists")
			return
		}
		slog.Warn("CreateProjectFolder failed", append(logger.RequestAttrs(r), "error", err)...)
		handler.WriteError(w, http.StatusInternalServerError, "failed to create project folder")
		return
	}
	resp := folderToResponse(folder)
	s.h.Publish(EventCreated, workspaceID, actorType, actorID, map[string]any{"folder": resp})
	handler.WriteJSON(w, http.StatusCreated, resp)
}

func (s *Service) Rename(w http.ResponseWriter, r *http.Request) {
	idUUID, ok := handler.ParseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "folder id")
	if !ok {
		return
	}
	name, ok := decodeName(w, r)
	if !ok {
		return
	}
	workspaceID := s.h.WorkspaceID(r)
	wsUUID, ok := handler.ParseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	userID, ok := handler.RequireUserID(w, r)
	if !ok {
		return
	}
	folder, err := s.q.RenameProjectFolder(r.Context(), folderdb.RenameProjectFolderParams{
		ID: idUUID, WorkspaceID: wsUUID, Name: name,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			handler.WriteError(w, http.StatusNotFound, "project folder not found")
			return
		}
		if handler.IsUniqueViolation(err) {
			handler.WriteError(w, http.StatusConflict, "a folder with that name already exists")
			return
		}
		slog.Warn("RenameProjectFolder failed", append(logger.RequestAttrs(r), "error", err)...)
		handler.WriteError(w, http.StatusInternalServerError, "failed to update project folder")
		return
	}
	actorType, actorID := s.h.Actor(r, userID, workspaceID)
	resp := folderToResponse(folder)
	s.h.Publish(EventUpdated, workspaceID, actorType, actorID, map[string]any{"folder": resp})
	handler.WriteJSON(w, http.StatusOK, resp)
}

// Delete moves the folder's projects out of it and deletes it in one
// transaction; the projects themselves are untouched.
func (s *Service) Delete(w http.ResponseWriter, r *http.Request) {
	idUUID, ok := handler.ParseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "folder id")
	if !ok {
		return
	}
	workspaceID := s.h.WorkspaceID(r)
	wsUUID, ok := handler.ParseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	userID, ok := handler.RequireUserID(w, r)
	if !ok {
		return
	}
	tx, err := s.h.TxStarter.Begin(r.Context())
	if err != nil {
		handler.WriteError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := s.q.WithTx(tx)
	if _, err := qtx.DeleteProjectFolder(r.Context(), folderdb.DeleteProjectFolderParams{
		ID: idUUID, WorkspaceID: wsUUID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			handler.WriteError(w, http.StatusNotFound, "project folder not found")
			return
		}
		slog.Warn("DeleteProjectFolder failed", append(logger.RequestAttrs(r), "error", err)...)
		handler.WriteError(w, http.StatusInternalServerError, "failed to delete project folder")
		return
	}
	if err := qtx.ClearProjectFolder(r.Context(), folderdb.ClearProjectFolderParams{
		WorkspaceID: wsUUID, FolderID: idUUID,
	}); err != nil {
		handler.WriteError(w, http.StatusInternalServerError, "failed to move projects out of the folder")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		handler.WriteError(w, http.StatusInternalServerError, "failed to commit project folder deletion")
		return
	}
	actorType, actorID := s.h.Actor(r, userID, workspaceID)
	s.h.Publish(EventDeleted, workspaceID, actorType, actorID, map[string]any{"folder_id": util.UUIDToString(idUUID)})
	w.WriteHeader(http.StatusNoContent)
}

type FileRequest struct {
	ProjectID string `json:"project_id"`
	// FolderID is the folder to file the project in; null moves it out.
	FolderID *string `json:"folder_id"`
}

// File moves a project into a folder, between folders, or out of every
// folder. Only moving in adds to the feature.
func (s *Service) File(w http.ResponseWriter, r *http.Request) {
	var req FileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handler.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	projectUUID, ok := handler.ParseUUIDOrBadRequest(w, req.ProjectID, "project_id")
	if !ok {
		return
	}
	var folderUUID pgtype.UUID
	if req.FolderID != nil && *req.FolderID != "" {
		if folderUUID, ok = handler.ParseUUIDOrBadRequest(w, *req.FolderID, "folder_id"); !ok {
			return
		}
	}
	workspaceID := s.h.WorkspaceID(r)
	wsUUID, ok := handler.ParseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	userID, ok := handler.RequireUserID(w, r)
	if !ok {
		return
	}
	if _, err := s.h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{ID: projectUUID, WorkspaceID: wsUUID}); err != nil {
		handler.WriteError(w, http.StatusNotFound, "project not found")
		return
	}
	if !s.h.RequireProjectVisible(w, r, workspaceID, projectUUID) {
		return
	}

	if folderUUID.Valid {
		n, err := s.q.FileProject(r.Context(), folderdb.FileProjectParams{
			FolderID: folderUUID, ProjectID: projectUUID, WorkspaceID: wsUUID,
		})
		if err != nil {
			slog.Warn("FileProject failed", append(logger.RequestAttrs(r), "error", err)...)
			handler.WriteError(w, http.StatusInternalServerError, "failed to move project")
			return
		}
		if n == 0 {
			handler.WriteError(w, http.StatusBadRequest, "folder not found in this workspace")
			return
		}
	} else if err := s.q.UnfileProject(r.Context(), folderdb.UnfileProjectParams{ID: projectUUID, WorkspaceID: wsUUID}); err != nil {
		slog.Warn("UnfileProject failed", append(logger.RequestAttrs(r), "error", err)...)
		handler.WriteError(w, http.StatusInternalServerError, "failed to move project")
		return
	}

	resp := FiledProject{ProjectID: req.ProjectID}
	if folderUUID.Valid {
		resp.FolderID = util.UUIDToString(folderUUID)
	}
	actorType, actorID := s.h.Actor(r, userID, workspaceID)
	s.h.Publish(EventFiled, workspaceID, actorType, actorID, map[string]any{
		"project_id": resp.ProjectID,
		"folder_id":  nullableString(resp.FolderID),
	})
	handler.WriteJSON(w, http.StatusOK, map[string]any{"project_id": resp.ProjectID, "folder_id": nullableString(resp.FolderID)})
}

func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func textPtr(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	return &t.String
}

func uuidPtr(u pgtype.UUID) *string {
	if !u.Valid {
		return nil
	}
	s := util.UUIDToString(u)
	return &s
}

package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	maxLinksPerScope = 20
	maxLinkIconBytes = 32
)

var linkVariableRE = regexp.MustCompile(`\{([^{}]*)\}`)

// Emoji sequences remain one icon: flags, keycaps, modifiers, and ZWJ families.
const linkEmojiBase = `[\x{00A9}\x{00AE}\x{203C}\x{2049}\x{2122}\x{2139}\x{2194}-\x{2199}\x{21A9}-\x{21AA}\x{231A}-\x{231B}\x{2328}\x{23CF}\x{23E9}-\x{23F3}\x{23F8}-\x{23FA}\x{24C2}\x{25AA}-\x{25AB}\x{25B6}\x{25C0}\x{25FB}-\x{25FE}\x{2600}-\x{27BF}\x{2934}-\x{2935}\x{2B05}-\x{2B07}\x{2B1B}-\x{2B1C}\x{2B50}\x{2B55}\x{3030}\x{303D}\x{3297}\x{3299}\x{1F300}-\x{1FAFF}]`

var linkEmojiRE = regexp.MustCompile(`^(?:[\x{1F1E6}-\x{1F1FF}]{2}|[0-9#*]\x{FE0F}?\x{20E3}|` + linkEmojiBase + `\x{FE0F}?[\x{1F3FB}-\x{1F3FF}]?(?:\x{200D}` + linkEmojiBase + `\x{FE0F}?[\x{1F3FB}-\x{1F3FF}]?)*(?:[\x{E0020}-\x{E007E}]+\x{E007F})?)$`)

// validateLinkURL permits only absolute HTTPS destinations so quick links cannot
// execute URL-scheme payloads or load insecure content inside the app. Variables
// are limited to path/query values: substitution must never change the origin
// that an admin reviewed, or introduce credentials or fragment-only behavior.
func validateLinkURL(template string, project bool) error {
	if len(template) > 2048 {
		return fmt.Errorf("url_template must be at most 2048 characters")
	}
	expanded := template
	for _, match := range linkVariableRE.FindAllStringSubmatch(template, -1) {
		switch match[1] {
		case "workspace_slug", "workspace_id":
		case "project_id":
			if !project {
				return fmt.Errorf("{project_id} is only allowed for project links")
			}
		default:
			return fmt.Errorf("unknown variable {%s}", match[1])
		}
		expanded = strings.ReplaceAll(expanded, match[0], "sample")
	}
	if strings.ContainsAny(expanded, "{}\\\r\n\t") {
		return fmt.Errorf("invalid URL template")
	}
	u, err := url.Parse(expanded)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.Opaque != "" {
		return fmt.Errorf("url_template must be an absolute https URL")
	}
	if u.User != nil {
		return fmt.Errorf("url_template must not contain userinfo")
	}
	// Only path and query substitution is supported; the origin is always literal.
	authorityEnd := strings.Index(template, "://")
	if authorityEnd < 0 {
		return fmt.Errorf("url_template must be an absolute https URL")
	}
	authorityEnd += 3
	if end := strings.IndexAny(template[authorityEnd:], "/?#"); end >= 0 {
		authorityEnd += end
	} else {
		authorityEnd = len(template)
	}
	if strings.ContainsAny(template[:authorityEnd], "{}") {
		return fmt.Errorf("variables are not allowed in scheme or host")
	}
	if hash := strings.Index(template, "#"); hash >= 0 && strings.ContainsAny(template[hash:], "{}") {
		return fmt.Errorf("variables are only allowed in path or query")
	}
	return nil
}

// validateWorkspaceLink bounds the header label and accepts only a fixed icon
// key or one emoji sequence before applying the destination's template rules.
func validateWorkspaceLink(link *db.WorkspaceLink) error {
	link.Name = strings.TrimSpace(link.Name)
	if n := utf8.RuneCountInString(link.Name); n < 1 || n > 40 {
		return fmt.Errorf("name must contain 1 to 40 characters")
	}
	if len(link.Icon) > maxLinkIconBytes {
		return fmt.Errorf("icon must be at most %d bytes", maxLinkIconBytes)
	}
	if link.Icon != "" && !linkEmojiRE.MatchString(link.Icon) {
		switch link.Icon {
		case "globe", "link", "book-open", "chart-bar", "code", "terminal", "folder", "file-text", "database", "server", "rocket", "shield":
		default:
			return fmt.Errorf("icon must be a supported icon key or a single emoji")
		}
	}
	if link.OpenMode != "embed" && link.OpenMode != "new_tab" {
		return fmt.Errorf("open_mode must be embed or new_tab")
	}
	return validateLinkURL(link.UrlTemplate, link.ProjectID.Valid)
}

type WorkspaceLinkResponse struct {
	ID              string  `json:"id"`
	WorkspaceID     string  `json:"workspace_id"`
	ProjectID       *string `json:"project_id"`
	Name            string  `json:"name"`
	Icon            string  `json:"icon"`
	ShowName        bool    `json:"show_name"`
	URLTemplate     string  `json:"url_template"`
	OpenMode        string  `json:"open_mode"`
	AllowSameOrigin bool    `json:"allow_same_origin"`
	Position        int32   `json:"position"`
	CreatedBy       *string `json:"created_by"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
}

func workspaceLinkResponse(l db.WorkspaceLink) WorkspaceLinkResponse {
	return WorkspaceLinkResponse{
		ID:              uuidToString(l.ID),
		ShowName:        l.ShowName,
		WorkspaceID:     uuidToString(l.WorkspaceID),
		ProjectID:       uuidToPtr(l.ProjectID),
		Name:            l.Name,
		Icon:            l.Icon,
		URLTemplate:     l.UrlTemplate,
		OpenMode:        l.OpenMode,
		AllowSameOrigin: l.AllowSameOrigin,
		Position:        l.Position,
		CreatedBy:       uuidToPtr(l.CreatedBy),
		CreatedAt:       timestampToString(l.CreatedAt),
		UpdatedAt:       timestampToString(l.UpdatedAt),
	}
}

func writeLinkDBError(w http.ResponseWriter, err error) {
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "link or project not found")
	} else {
		writeError(w, http.StatusInternalServerError, "failed to access links")
	}
}

func (h *Handler) linkWorkspace(w http.ResponseWriter, r *http.Request, write bool) (pgtype.UUID, bool) {
	ws := h.resolveWorkspaceID(r)
	id, ok := parseUUIDOrBadRequest(w, ws, "workspace id")
	if !ok {
		return id, false
	}
	if write {
		_, ok = h.requireWorkspaceRole(w, r, ws, "workspace not found", "owner", "admin")
	} else {
		_, ok = h.requireWorkspaceMember(w, r, ws, "workspace not found")
	}
	return id, ok
}

func (h *Handler) linkProject(w http.ResponseWriter, r *http.Request, ws pgtype.UUID, raw string) (pgtype.UUID, bool) {
	if raw == "" {
		return pgtype.UUID{}, true
	}
	id, ok := parseUUIDOrBadRequest(w, raw, "project_id")
	if !ok {
		return id, false
	}
	_, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{ID: id, WorkspaceID: ws})
	if err != nil {
		writeLinkDBError(w, err)
		return id, false
	}
	return id, true
}

// ListWorkspaceLinks returns the workspace scope or the requested project's
// scope. Any workspace member may read the catalog.
func (h *Handler) ListWorkspaceLinks(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.linkWorkspace(w, r, false)
	if !ok {
		return
	}
	project, ok := h.linkProject(w, r, ws, r.URL.Query().Get("project_id"))
	if !ok {
		return
	}
	rows, err := h.Queries.ListWorkspaceLinks(r.Context(), db.ListWorkspaceLinksParams{WorkspaceID: ws, ProjectID: project})
	if err != nil {
		writeLinkDBError(w, err)
		return
	}
	out := make([]WorkspaceLinkResponse, 0, len(rows))
	for _, l := range rows {
		out = append(out, workspaceLinkResponse(l))
	}
	writeJSON(w, http.StatusOK, map[string]any{"links": out})
}

// CreateWorkspaceLink creates an admin-configured link within the scope cap.
func (h *Handler) CreateWorkspaceLink(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.linkWorkspace(w, r, true)
	if !ok {
		return
	}
	var req struct {
		ProjectID       string `json:"project_id"`
		Name            string `json:"name"`
		Icon            string `json:"icon"`
		ShowName        *bool  `json:"show_name"`
		URLTemplate     string `json:"url_template"`
		OpenMode        string `json:"open_mode"`
		AllowSameOrigin bool   `json:"allow_same_origin"`
		Position        int32  `json:"position"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	project, ok := h.linkProject(w, r, ws, req.ProjectID)
	if !ok {
		return
	}
	if req.OpenMode == "" {
		req.OpenMode = "embed"
	}
	l := db.WorkspaceLink{
		ShowName:        true,
		WorkspaceID:     ws,
		ProjectID:       project,
		Name:            req.Name,
		Icon:            req.Icon,
		UrlTemplate:     req.URLTemplate,
		OpenMode:        req.OpenMode,
		AllowSameOrigin: req.AllowSameOrigin,
		Position:        req.Position,
	}
	if req.ShowName != nil {
		l.ShowName = *req.ShowName
	}
	if err := validateWorkspaceLink(&l); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Serialize the count and insertion per workspace so the scope cap is exact.
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeLinkDBError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	q := h.Queries.WithTx(tx)
	if _, err = q.LockWorkspaceForDelete(r.Context(), ws); err != nil {
		writeLinkDBError(w, err)
		return
	}
	if project.Valid {
		if _, err = q.LockProjectForChatSessionCreate(r.Context(), db.LockProjectForChatSessionCreateParams{ID: project, WorkspaceID: ws}); err != nil {
			writeLinkDBError(w, err)
			return
		}
	}
	count, err := q.CountWorkspaceLinks(r.Context(), db.CountWorkspaceLinksParams{WorkspaceID: ws, ProjectID: project})
	if err != nil {
		writeLinkDBError(w, err)
		return
	}
	if count >= maxLinksPerScope {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("a scope can have at most %d links", maxLinksPerScope))
		return
	}
	user, ok := requireUserID(w, r)
	if !ok {
		return
	}
	creator, ok := parseUUIDOrBadRequest(w, user, "user id")
	if !ok {
		return
	}
	l, err = q.CreateWorkspaceLink(r.Context(), db.CreateWorkspaceLinkParams{
		WorkspaceID:     ws,
		ProjectID:       project,
		Name:            l.Name,
		Icon:            l.Icon,
		UrlTemplate:     l.UrlTemplate,
		OpenMode:        l.OpenMode,
		AllowSameOrigin: l.AllowSameOrigin,
		Position:        l.Position,
		CreatedBy:       creator,
		ShowName:        l.ShowName,
	})
	if err != nil {
		writeLinkDBError(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeLinkDBError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, workspaceLinkResponse(l))
}

// UpdateWorkspaceLink applies an admin's edits to an existing scoped link.
func (h *Handler) UpdateWorkspaceLink(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.linkWorkspace(w, r, true)
	if !ok {
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "link id")
	if !ok {
		return
	}
	// Serialize the full read-merge-validate-write flow, as for project resources
	// (#9238), so a partial PATCH cannot overwrite another writer's changes.
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeLinkDBError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	l, err := qtx.GetWorkspaceLinkForUpdate(r.Context(), db.GetWorkspaceLinkForUpdateParams{
		ID:          id,
		WorkspaceID: ws,
	})
	if err != nil {
		writeLinkDBError(w, err)
		return
	}
	var req struct {
		Name            *string `json:"name"`
		Icon            *string `json:"icon"`
		ShowName        *bool   `json:"show_name"`
		URLTemplate     *string `json:"url_template"`
		OpenMode        *string `json:"open_mode"`
		AllowSameOrigin *bool   `json:"allow_same_origin"`
		Position        *int32  `json:"position"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name != nil {
		l.Name = *req.Name
	}
	if req.Icon != nil {
		l.Icon = *req.Icon
	}
	if req.ShowName != nil {
		l.ShowName = *req.ShowName
	}
	if req.URLTemplate != nil {
		l.UrlTemplate = *req.URLTemplate
	}
	if req.OpenMode != nil {
		l.OpenMode = *req.OpenMode
	}
	if req.AllowSameOrigin != nil {
		l.AllowSameOrigin = *req.AllowSameOrigin
	}
	if req.Position != nil {
		l.Position = *req.Position
	}
	if err = validateWorkspaceLink(&l); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	l, err = qtx.UpdateWorkspaceLink(r.Context(), db.UpdateWorkspaceLinkParams{
		ShowName:        l.ShowName,
		ID:              id,
		WorkspaceID:     ws,
		Name:            l.Name,
		Icon:            l.Icon,
		UrlTemplate:     l.UrlTemplate,
		OpenMode:        l.OpenMode,
		AllowSameOrigin: l.AllowSameOrigin,
		Position:        l.Position,
	})
	if err != nil {
		writeLinkDBError(w, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeLinkDBError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, workspaceLinkResponse(l))
}

// DeleteWorkspaceLink removes a link after checking workspace admin access.
func (h *Handler) DeleteWorkspaceLink(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.linkWorkspace(w, r, true)
	if !ok {
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "link id")
	if !ok {
		return
	}
	_, err := h.Queries.DeleteWorkspaceLink(r.Context(), db.DeleteWorkspaceLinkParams{ID: id, WorkspaceID: ws})
	if err != nil {
		writeLinkDBError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

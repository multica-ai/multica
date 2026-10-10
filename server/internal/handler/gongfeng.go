package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/integrations/vcs"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type GongfengRepositoryResponse struct {
	ID                int64   `json:"id"`
	Path              string  `json:"path"`
	WebURL            string  `json:"web_url"`
	CloneURL          string  `json:"clone_url"`
	Description       string  `json:"description"`
	DefaultBranch     string  `json:"default_branch"`
	Archived          bool    `json:"archived"`
	SyncedAt          *string `json:"synced_at"`
	Syncing           bool    `json:"syncing"`
	SyncError         string  `json:"sync_error"`
	WebhookConfigured bool    `json:"webhook_configured"`
}

func gongfengRepositoryResponse(p db.GongfengRepository) GongfengRepositoryResponse {
	return GongfengRepositoryResponse{ID: p.ProjectID, Path: p.Path, WebURL: p.WebUrl, CloneURL: p.CloneUrl, Description: p.Description, DefaultBranch: p.DefaultBranch, SyncedAt: timestampToPtr(p.SyncedAt), Syncing: p.ScanStartedAt.Valid || !p.SyncedAt.Valid, SyncError: p.SyncError, WebhookConfigured: p.HookID > 0 && p.HookRevision.Valid}
}
func (h *Handler) gongfengClient(conn db.VcsConnection) (*vcs.GongfengClient, error) {
	if !h.isVCSAvailable() || !h.isVCSConfigured() || conn.Provider != "gongfeng" {
		return nil, errors.New("Gongfeng integration is not configured")
	}
	token, err := h.openVCSSecret(conn.AccessTokenEncrypted)
	if err != nil {
		return nil, errors.New("Could not decrypt the Gongfeng token")
	}
	return vcs.NewGongfengClient(conn.InstanceUrl, token)
}
func (h *Handler) loadGongfengConnection(w http.ResponseWriter, r *http.Request, write bool) (db.VcsConnection, *vcs.GongfengClient, func(), bool) {
	if !h.isVCSAvailable() {
		writeError(w, http.StatusNotFound, "vcs integration is not available")
		return db.VcsConnection{}, nil, nil, false
	}
	if !h.isVCSConfigured() {
		writeFeatureDisabled(w, "vcs_not_configured", "vcs integration not configured")
		return db.VcsConnection{}, nil, nil, false
	}
	ws, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace id")
	if !ok {
		return db.VcsConnection{}, nil, nil, false
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "connectionId"), "connection id")
	if !ok {
		return db.VcsConnection{}, nil, nil, false
	}
	unlock := h.GongfengSync.lockConnection(id, write)
	conn, err := h.Queries.GetVCSConnectionByID(r.Context(), id)
	if err != nil || conn.WorkspaceID != ws || conn.Provider != "gongfeng" {
		unlock()
		writeError(w, 404, "Gongfeng connection not found")
		return db.VcsConnection{}, nil, nil, false
	}
	client, err := h.gongfengClient(conn)
	if err != nil {
		writeError(w, 500, err.Error())
		unlock()
		return conn, nil, nil, false
	}
	return conn, client, unlock, true
}
func gongfengProjectID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "projectId"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, 400, "invalid project id")
		return 0, false
	}
	return id, true
}

// Repository management is admin-only at the router, just like GitHub browsing.
func (h *Handler) ListGongfengRepositories(w http.ResponseWriter, r *http.Request) {
	conn, client, unlock, ok := h.loadGongfengConnection(w, r, false)
	if !ok {
		return
	}
	defer unlock()
	page := 1
	if raw := r.URL.Query().Get("page"); raw != "" {
		var err error
		page, err = strconv.Atoi(raw)
		if err != nil || page < 1 || page > 10000 {
			writeError(w, 400, "invalid page")
			return
		}
	}
	projects, next, err := client.Projects(r.Context(), r.URL.Query().Get("search"), page)
	if err != nil {
		writeError(w, 502, err.Error())
		return
	}
	rows, err := h.Queries.ListGongfengRepositories(r.Context(), db.ListGongfengRepositoriesParams{ConnectionID: conn.ID, WorkspaceID: conn.WorkspaceID})
	if err != nil {
		writeError(w, 500, "failed to list selected repositories")
		return
	}
	repositories := make([]GongfengRepositoryResponse, 0, len(projects))
	selected := make([]GongfengRepositoryResponse, 0, len(rows))
	for _, p := range projects {
		repositories = append(repositories, GongfengRepositoryResponse{ID: p.ID, Path: p.Path, WebURL: p.WebURL, CloneURL: p.CloneURL(), Description: p.Description, DefaultBranch: p.DefaultBranch, Archived: p.Archived})
	}
	for _, row := range rows {
		selected = append(selected, gongfengRepositoryResponse(row))
	}
	writeJSON(w, 200, map[string]any{"repositories": repositories, "selected": selected, "next_page": next})
}
func (h *Handler) AddGongfengRepository(w http.ResponseWriter, r *http.Request) {
	conn, client, unlock, ok := h.loadGongfengConnection(w, r, true)
	if !ok {
		return
	}
	defer unlock()
	id, ok := gongfengProjectID(w, r)
	if !ok {
		return
	}
	project, err := client.Project(r.Context(), strconv.FormatInt(id, 10))
	if err != nil {
		writeError(w, 502, err.Error())
		return
	}
	if project.ID != id || project.Archived {
		writeError(w, 400, "repository is archived or has a mismatched identity")
		return
	}
	row, err := h.Queries.AddGongfengRepository(r.Context(), db.AddGongfengRepositoryParams{ConnectionID: conn.ID, ProjectID: project.ID, Path: project.Path, WebUrl: project.WebURL, CloneUrl: project.CloneURL(), Description: project.Description, DefaultBranch: project.DefaultBranch})
	if err != nil {
		writeError(w, 500, "failed to save repository")
		return
	}
	h.GongfengSync.enqueueRepository(row)
	writeJSON(w, 200, gongfengRepositoryResponse(row))
}
func (h *Handler) RetryGongfengRepository(w http.ResponseWriter, r *http.Request) {
	conn, _, unlock, ok := h.loadGongfengConnection(w, r, true)
	if !ok {
		return
	}
	defer unlock()
	id, ok := gongfengProjectID(w, r)
	if !ok {
		return
	}
	row, err := h.Queries.ResetGongfengRepositorySync(r.Context(), db.ResetGongfengRepositorySyncParams{ConnectionID: conn.ID, ProjectID: id})
	if err != nil {
		writeError(w, 404, "selected repository not found")
		return
	}
	h.GongfengSync.enqueueRepository(row)
	writeJSON(w, 200, gongfengRepositoryResponse(row))
}
func (h *Handler) DeleteGongfengRepository(w http.ResponseWriter, r *http.Request) {
	conn, client, unlock, ok := h.loadGongfengConnection(w, r, true)
	if !ok {
		return
	}
	defer unlock()
	id, ok := gongfengProjectID(w, r)
	if !ok {
		return
	}
	row, err := h.Queries.GetGongfengRepository(r.Context(), db.GetGongfengRepositoryParams{ConnectionID: conn.ID, ProjectID: id})
	if err != nil {
		writeError(w, 404, "selected repository not found")
		return
	}
	if err = client.RemoveHook(r.Context(), id, row.HookID, h.vcsWebhookURL(uuidToString(conn.ID))); err != nil {
		writeError(w, 502, err.Error())
		return
	}
	if err = h.Queries.DeleteGongfengRepository(r.Context(), db.DeleteGongfengRepositoryParams{ConnectionID: conn.ID, ProjectID: id}); err != nil {
		writeError(w, 500, "failed to remove repository")
		return
	}
	w.WriteHeader(204)
}
func (h *Handler) resetGongfengHooks(ctx context.Context, conn db.VcsConnection) error {
	if conn.Provider != "gongfeng" {
		return nil
	}
	if err := h.Queries.ResetGongfengConnectionHooks(ctx, conn.ID); err != nil {
		return err
	}
	rows, err := h.Queries.ListGongfengRepositories(ctx, db.ListGongfengRepositoriesParams{ConnectionID: conn.ID, WorkspaceID: conn.WorkspaceID})
	if err != nil {
		return err
	}
	for _, row := range rows {
		h.GongfengSync.enqueueRepository(row)
	}
	return nil
}
func (h *Handler) removeGongfengHooks(ctx context.Context, conn db.VcsConnection) error {
	client, err := h.gongfengClient(conn)
	if err != nil {
		return err
	}
	rows, err := h.Queries.ListGongfengRepositories(ctx, db.ListGongfengRepositoriesParams{ConnectionID: conn.ID, WorkspaceID: conn.WorkspaceID})
	if err != nil {
		return err
	}
	for _, row := range rows {
		if err := client.RemoveHook(ctx, row.ProjectID, row.HookID, h.vcsWebhookURL(uuidToString(conn.ID))); err != nil {
			return err
		}
	}
	return nil
}

// Keep workers and callbacks outside workspace teardown. Capture hook metadata
// before its rows disappear, and remove remote hooks only after the DB commits.
func (h *Handler) prepareGongfengWorkspaceDeletion(ctx context.Context, wsID pgtype.UUID) (func(context.Context), func(), error) {
	connections, err := h.Queries.ListVCSConnectionsByWorkspace(ctx, wsID)
	if err != nil {
		return nil, nil, err
	}
	sort.Slice(connections, func(i, j int) bool { return uuidToString(connections[i].ID) < uuidToString(connections[j].ID) })
	var unlocks []func()
	unlock := func() {
		for i := len(unlocks) - 1; i >= 0; i-- {
			unlocks[i]()
		}
	}
	type hookCleanup struct {
		connection db.VcsConnection
		repository db.GongfengRepository
	}
	var hooks []hookCleanup
	for _, conn := range connections {
		if conn.Provider != "gongfeng" {
			continue
		}
		unlocks = append(unlocks, h.GongfengSync.lockConnection(conn.ID, true))
		conn, err = h.Queries.GetVCSConnectionByID(ctx, conn.ID)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			unlock()
			return nil, nil, err
		}
		rows, err := h.Queries.ListGongfengRepositories(ctx, db.ListGongfengRepositoriesParams{ConnectionID: conn.ID, WorkspaceID: wsID})
		if err != nil {
			unlock()
			return nil, nil, err
		}
		for _, row := range rows {
			if row.HookID > 0 {
				hooks = append(hooks, hookCleanup{conn, row})
			}
		}
	}
	cleanup := func(ctx context.Context) {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		for _, hook := range hooks {
			client, err := h.gongfengClient(hook.connection)
			if err == nil {
				err = client.RemoveHook(ctx, hook.repository.ProjectID, hook.repository.HookID, h.vcsWebhookURL(uuidToString(hook.connection.ID)))
			}
			if err != nil {
				slog.Warn("gongfeng: deleted workspace webhook cleanup failed", "connection_id", uuidToString(hook.connection.ID), "project_id", hook.repository.ProjectID, "err", err)
			}
		}
	}
	return cleanup, unlock, nil
}

func (h *Handler) publishGongfengSnapshot(ctx context.Context, pr db.VcsPullRequest) {
	ids, err := h.Queries.ListIssueIDsForVCSPullRequest(ctx, pr.ID)
	if err != nil {
		return
	}
	linked := make([]string, 0, len(ids))
	for _, id := range ids {
		linked = append(linked, uuidToString(id))
	}
	h.publish(protocol.EventPullRequestUpdated, uuidToString(pr.WorkspaceID), "system", "", map[string]any{"linked_issue_ids": linked, "pull_request": vcsPullRequestToResponse(pr)})
}

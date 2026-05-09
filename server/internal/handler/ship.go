package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service/ship"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	gh "github.com/multica-ai/multica/server/pkg/github"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// requireShipHubEnabled mirrors requireChannelsEnabled — every Ship Hub
// endpoint 404s when the workspace flag is off so the surface is invisible
// to anyone who hasn't opted in.
func (h *Handler) requireShipHubEnabled(w http.ResponseWriter, r *http.Request) (pgtype.UUID, db.Workspace, bool) {
	workspaceID := h.resolveWorkspaceID(r)
	wsID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return pgtype.UUID{}, db.Workspace{}, false
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), wsID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return pgtype.UUID{}, db.Workspace{}, false
	}
	if !ws.ShipHubEnabled {
		writeError(w, http.StatusNotFound, "not found")
		return pgtype.UUID{}, db.Workspace{}, false
	}
	return wsID, ws, true
}

// shipServiceFromWorkspace constructs a ship.Service whose GitHub client
// uses the workspace's stored token. Returns ok=false (with error already
// written) when the workspace has no token AND the request needed one.
//
// Read-only handlers (list PRs, list envs) call this with requireToken=false
// — they're fine working off cached rows. Sync/refresh handlers call it
// with requireToken=true.
func (h *Handler) shipServiceFromWorkspace(w http.ResponseWriter, ws db.Workspace, requireToken bool) (*ship.Service, bool) {
	token := readShipHubGitHubToken(ws.Settings)
	if requireToken && token == "" {
		writeError(w, http.StatusBadRequest, "ship_hub: github_token is not configured for this workspace")
		return nil, false
	}
	return &ship.Service{
		Q:      h.Queries,
		Github: gh.NewClient(token),
	}, true
}

// pullRequestToResponse is the JSON shape the frontend Kanban renders.
// Keep this stable — new fields go at the end so older Electron builds
// don't choke on a column they don't know about.
type pullRequestResponse struct {
	ID              string  `json:"id"`
	WorkspaceID     string  `json:"workspace_id"`
	ProjectID       *string `json:"project_id"`
	RepoURL         string  `json:"repo_url"`
	Number          int32   `json:"number"`
	Title           string  `json:"title"`
	State           string  `json:"state"`
	IsDraft         bool    `json:"is_draft"`
	AuthorLogin     string  `json:"author_login"`
	AuthorAvatarURL *string `json:"author_avatar_url"`
	BaseRef         string  `json:"base_ref"`
	HeadRef         string  `json:"head_ref"`
	HeadSHA         string  `json:"head_sha"`
	HTMLURL         string  `json:"html_url"`
	Body            *string `json:"body"`
	CIStatus        *string `json:"ci_status"`
	ReviewDecision  *string `json:"review_decision"`
	Mergeable       *string `json:"mergeable"`
	Additions       int32   `json:"additions"`
	Deletions       int32   `json:"deletions"`
	ChangedFiles    int32   `json:"changed_files"`
	Labels          any     `json:"labels"`
	PRCreatedAt     string  `json:"pr_created_at"`
	PRUpdatedAt     string  `json:"pr_updated_at"`
	PRMergedAt      *string `json:"pr_merged_at"`
	PRClosedAt      *string `json:"pr_closed_at"`
	FetchedAt       string  `json:"fetched_at"`
	// Phase 4 — linkage fields. Per CLAUDE.md "API Response Compatibility",
	// these are appended at the end so an older Electron build that
	// doesn't know the schema simply ignores them.
	OriginatingIssueID     *string `json:"originating_issue_id"`
	OriginatingAgentTaskID *string `json:"originating_agent_task_id"`
	AutoCloseIssueOnMerge  bool    `json:"auto_close_issue_on_merge"`
	ConversationChannelID  *string `json:"conversation_channel_id"`
	StackParentPRID        *string `json:"stack_parent_pr_id"`
	Source                 string  `json:"source"`
}

func pullRequestToResponse(pr db.PullRequest) pullRequestResponse {
	var labels any
	if len(pr.Labels) > 0 {
		json.Unmarshal(pr.Labels, &labels)
	}
	if labels == nil {
		labels = []any{}
	}
	return pullRequestResponse{
		ID:              uuidToString(pr.ID),
		WorkspaceID:     uuidToString(pr.WorkspaceID),
		ProjectID:       uuidToPtr(pr.ProjectID),
		RepoURL:         pr.RepoUrl,
		Number:          pr.PrNumber,
		Title:           pr.Title,
		State:           string(pr.State),
		IsDraft:         pr.IsDraft,
		AuthorLogin:     pr.AuthorLogin,
		AuthorAvatarURL: textToPtr(pr.AuthorAvatarUrl),
		BaseRef:         pr.BaseRef,
		HeadRef:         pr.HeadRef,
		HeadSHA:         pr.HeadSha,
		HTMLURL:         pr.HtmlUrl,
		Body:            textToPtr(pr.Body),
		CIStatus:        textToPtr(pr.CiStatus),
		ReviewDecision:  textToPtr(pr.ReviewDecision),
		Mergeable:       textToPtr(pr.Mergeable),
		Additions:       pr.Additions,
		Deletions:       pr.Deletions,
		ChangedFiles:    pr.ChangedFiles,
		Labels:          labels,
		PRCreatedAt:     timestampToString(pr.PrCreatedAt),
		PRUpdatedAt:     timestampToString(pr.PrUpdatedAt),
		PRMergedAt:      timestampToPtr(pr.PrMergedAt),
		PRClosedAt:      timestampToPtr(pr.PrClosedAt),
		FetchedAt:       timestampToString(pr.FetchedAt),
		OriginatingIssueID:     uuidToPtr(pr.OriginatingIssueID),
		OriginatingAgentTaskID: uuidToPtr(pr.OriginatingAgentTaskID),
		AutoCloseIssueOnMerge:  pr.AutoCloseIssueOnMerge,
		ConversationChannelID:  uuidToPtr(pr.ConversationChannelID),
		StackParentPRID:        uuidToPtr(pr.StackParentPrID),
		Source:                 pr.Source,
	}
}

type deployEnvironmentResponse struct {
	ID                string  `json:"id"`
	WorkspaceID       string  `json:"workspace_id"`
	ProjectID         string  `json:"project_id"`
	Kind              string  `json:"kind"`
	Name              string  `json:"name"`
	TargetBranch      string  `json:"target_branch"`
	TargetURL         *string `json:"target_url"`
	CurrentSHA        *string `json:"current_sha"`
	CurrentDeployedAt *string `json:"current_deployed_at"`
	AutoPromote       bool    `json:"auto_promote"`
	CreatedAt         string  `json:"created_at"`
	UpdatedAt         string  `json:"updated_at"`
}

func deployEnvironmentToResponse(e db.DeployEnvironment) deployEnvironmentResponse {
	return deployEnvironmentResponse{
		ID:                uuidToString(e.ID),
		WorkspaceID:       uuidToString(e.WorkspaceID),
		ProjectID:         uuidToString(e.ProjectID),
		Kind:              string(e.Kind),
		Name:              e.Name,
		TargetBranch:      e.TargetBranch,
		TargetURL:         textToPtr(e.TargetUrl),
		CurrentSHA:        textToPtr(e.CurrentSha),
		CurrentDeployedAt: timestampToPtr(e.CurrentDeployedAt),
		AutoPromote:       e.AutoPromote,
		CreatedAt:         timestampToString(e.CreatedAt),
		UpdatedAt:         timestampToString(e.UpdatedAt),
	}
}

type deployResponse struct {
	ID            string  `json:"id"`
	WorkspaceID   string  `json:"workspace_id"`
	EnvironmentID string  `json:"environment_id"`
	Ref           string  `json:"ref"`
	SHA           string  `json:"sha"`
	Status        string  `json:"status"`
	TriggeredBy   *string `json:"triggered_by"`
	TriggeredAt   string  `json:"triggered_at"`
	StartedAt     *string `json:"started_at"`
	CompletedAt   *string `json:"completed_at"`
	LogURL        *string `json:"log_url"`
	ErrorMessage  *string `json:"error_message"`
	CreatedAt     string  `json:"created_at"`
}

func deployToResponse(d db.Deploy) deployResponse {
	return deployResponse{
		ID:            uuidToString(d.ID),
		WorkspaceID:   uuidToString(d.WorkspaceID),
		EnvironmentID: uuidToString(d.EnvironmentID),
		Ref:           d.Ref,
		SHA:           d.Sha,
		Status:        string(d.Status),
		TriggeredBy:   uuidToPtr(d.TriggeredBy),
		TriggeredAt:   timestampToString(d.TriggeredAt),
		StartedAt:     timestampToPtr(d.StartedAt),
		CompletedAt:   timestampToPtr(d.CompletedAt),
		LogURL:        textToPtr(d.LogUrl),
		ErrorMessage:  textToPtr(d.ErrorMessage),
		CreatedAt:     timestampToString(d.CreatedAt),
	}
}

// ListShipProjects returns the projects in the workspace that have at
// least one github_repo resource attached, decorated with open-PR + env
// counts. The Kanban renders one column per project.
func (h *Handler) ListShipProjects(w http.ResponseWriter, r *http.Request) {
	wsID, _, ok := h.requireShipHubEnabled(w, r)
	if !ok {
		return
	}
	projects, err := h.Queries.ListProjects(r.Context(), db.ListProjectsParams{
		WorkspaceID:     wsID,
		IncludeArchived: false,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list projects")
		return
	}
	if len(projects) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"projects": []any{}})
		return
	}

	projectIDs := make([]pgtype.UUID, len(projects))
	for i, p := range projects {
		projectIDs[i] = p.ID
	}

	// We need each project's github_repo resources to filter the list.
	// One query, then bucket in-memory.
	resources, _ := h.Queries.ListProjectResourcesForProjects(r.Context(), projectIDs)
	hasGithub := map[string]bool{}
	for _, res := range resources {
		if res.ResourceType == "github_repo" {
			hasGithub[uuidToString(res.ProjectID)] = true
		}
	}

	prCounts := map[string]int64{}
	if rows, err := h.Queries.CountOpenPullRequestsForProjects(r.Context(), projectIDs); err == nil {
		for _, row := range rows {
			prCounts[uuidToString(row.ProjectID)] = row.OpenCount
		}
	}
	envCounts := map[string]int64{}
	if rows, err := h.Queries.CountDeployEnvironmentsForProjects(r.Context(), projectIDs); err == nil {
		for _, row := range rows {
			envCounts[uuidToString(row.ProjectID)] = row.EnvCount
		}
	}

	out := make([]map[string]any, 0, len(projects))
	for _, p := range projects {
		id := uuidToString(p.ID)
		if !hasGithub[id] {
			continue
		}
		out = append(out, map[string]any{
			"id":             id,
			"title":          p.Title,
			"icon":           textToPtr(p.Icon),
			"open_pr_count":  prCounts[id],
			"env_count":      envCounts[id],
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": out})
}

// loadShipProject resolves the project ID URL param and enforces workspace
// scoping. Returns the project row + the workspace UUID so callers can pass
// both downstream.
func (h *Handler) loadShipProject(w http.ResponseWriter, r *http.Request) (db.Project, pgtype.UUID, db.Workspace, bool) {
	wsID, ws, ok := h.requireShipHubEnabled(w, r)
	if !ok {
		return db.Project{}, pgtype.UUID{}, db.Workspace{}, false
	}
	projectUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "project id")
	if !ok {
		return db.Project{}, pgtype.UUID{}, db.Workspace{}, false
	}
	project, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
		ID: projectUUID, WorkspaceID: wsID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return db.Project{}, pgtype.UUID{}, db.Workspace{}, false
	}
	return project, wsID, ws, true
}

// ListProjectPullRequests returns the cached PR rows for a project. Use
// SyncProjectPullRequests first to refresh from GitHub.
func (h *Handler) ListProjectPullRequests(w http.ResponseWriter, r *http.Request) {
	project, _, _, ok := h.loadShipProject(w, r)
	if !ok {
		return
	}
	stateParam := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("state")))
	stateFilter := db.NullPullRequestState{}
	switch stateParam {
	case "":
		// default: open only
		stateFilter = db.NullPullRequestState{PullRequestState: db.PullRequestStateOpen, Valid: true}
	case "open":
		stateFilter = db.NullPullRequestState{PullRequestState: db.PullRequestStateOpen, Valid: true}
	case "closed":
		stateFilter = db.NullPullRequestState{PullRequestState: db.PullRequestStateClosed, Valid: true}
	case "merged":
		stateFilter = db.NullPullRequestState{PullRequestState: db.PullRequestStateMerged, Valid: true}
	case "all":
		// Valid=false means "no filter" in the query.
	default:
		writeError(w, http.StatusBadRequest, "invalid state")
		return
	}
	rows, err := h.Queries.ListPullRequestsByProject(r.Context(), db.ListPullRequestsByProjectParams{
		ProjectID: project.ID,
		State:     stateFilter,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list pull requests")
		return
	}
	out := make([]pullRequestResponse, len(rows))
	for i, pr := range rows {
		out[i] = pullRequestToResponse(pr)
	}
	writeJSON(w, http.StatusOK, map[string]any{"pull_requests": out, "total": len(out)})
}

// SyncProjectPullRequests triggers a fresh pull from GitHub and returns
// the per-call SyncResult so the UI can confirm what changed.
func (h *Handler) SyncProjectPullRequests(w http.ResponseWriter, r *http.Request) {
	project, wsID, ws, ok := h.loadShipProject(w, r)
	if !ok {
		return
	}
	svc, ok := h.shipServiceFromWorkspace(w, ws, true)
	if !ok {
		return
	}
	result, err := svc.SyncProject(r.Context(), wsID, project.ID)
	if err != nil {
		// Translate the typed errors so the UI can render something
		// actionable. Auth errors get 401 so the workspace owner can
		// re-enter the token; rate limits get 429 so the UI can back off.
		if errors.Is(err, gh.ErrUnauthorized) {
			writeError(w, http.StatusUnauthorized, "github: invalid or revoked token")
			return
		}
		if errors.Is(err, gh.ErrRateLimited) {
			writeError(w, http.StatusTooManyRequests, "github: rate limit hit, retry shortly")
			return
		}
		writeError(w, http.StatusInternalServerError, "ship hub sync failed: "+err.Error())
		return
	}
	userID := requestUserID(r)
	h.publish(protocol.EventPullRequestSynced, uuidToString(wsID), "member", userID, map[string]any{
		"project_id": uuidToString(project.ID),
		"result":     result,
	})
	writeJSON(w, http.StatusOK, result)
}

// ListProjectDeployEnvironments returns the staging/production envs the
// user has configured for a project.
func (h *Handler) ListProjectDeployEnvironments(w http.ResponseWriter, r *http.Request) {
	project, _, _, ok := h.loadShipProject(w, r)
	if !ok {
		return
	}
	envs, err := h.Queries.ListDeployEnvironmentsByProject(r.Context(), project.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list deploy environments")
		return
	}
	out := make([]deployEnvironmentResponse, len(envs))
	for i, e := range envs {
		out[i] = deployEnvironmentToResponse(e)
	}
	writeJSON(w, http.StatusOK, map[string]any{"environments": out})
}

// CreateDeployEnvironmentRequest is the body for POST /api/projects/{id}/deploy_environments.
type CreateDeployEnvironmentRequest struct {
	Kind         string  `json:"kind"`
	Name         string  `json:"name"`
	TargetBranch *string `json:"target_branch"`
	TargetURL    *string `json:"target_url"`
	AutoPromote  *bool   `json:"auto_promote"`
}

func (h *Handler) CreateProjectDeployEnvironment(w http.ResponseWriter, r *http.Request) {
	project, wsID, _, ok := h.loadShipProject(w, r)
	if !ok {
		return
	}
	var req CreateDeployEnvironmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	kind, ok := normalizeDeployKind(req.Kind)
	if !ok {
		writeError(w, http.StatusBadRequest, "kind must be 'staging' or 'production'")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	branch := "main"
	if req.TargetBranch != nil && strings.TrimSpace(*req.TargetBranch) != "" {
		branch = strings.TrimSpace(*req.TargetBranch)
	}
	autoPromote := false
	if req.AutoPromote != nil {
		autoPromote = *req.AutoPromote
	}
	env, err := h.Queries.UpsertDeployEnvironment(r.Context(), db.UpsertDeployEnvironmentParams{
		WorkspaceID:  wsID,
		ProjectID:    project.ID,
		Kind:         kind,
		Name:         name,
		TargetBranch: branch,
		TargetUrl:    ptrToText(req.TargetURL),
		AutoPromote:  autoPromote,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create deploy environment")
		return
	}
	writeJSON(w, http.StatusCreated, deployEnvironmentToResponse(env))
}

// UpdateDeployEnvironmentRequest is the PATCH /api/deploy_environments/{id} body.
// Pointer fields distinguish "not present" from "present and empty"; nil
// leaves the column unchanged.
type UpdateDeployEnvironmentRequest struct {
	Name         *string `json:"name"`
	TargetBranch *string `json:"target_branch"`
	TargetURL    *string `json:"target_url"`
	AutoPromote  *bool   `json:"auto_promote"`
}

// loadDeployEnvironment resolves the environment ID URL param and verifies
// it lives in the caller's workspace.
func (h *Handler) loadDeployEnvironment(w http.ResponseWriter, r *http.Request) (db.DeployEnvironment, pgtype.UUID, bool) {
	wsID, _, ok := h.requireShipHubEnabled(w, r)
	if !ok {
		return db.DeployEnvironment{}, pgtype.UUID{}, false
	}
	envUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "environment id")
	if !ok {
		return db.DeployEnvironment{}, pgtype.UUID{}, false
	}
	env, err := h.Queries.GetDeployEnvironmentInWorkspace(r.Context(), db.GetDeployEnvironmentInWorkspaceParams{
		ID: envUUID, WorkspaceID: wsID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "deploy environment not found")
		return db.DeployEnvironment{}, pgtype.UUID{}, false
	}
	return env, wsID, true
}

func (h *Handler) UpdateDeployEnvironment(w http.ResponseWriter, r *http.Request) {
	env, _, ok := h.loadDeployEnvironment(w, r)
	if !ok {
		return
	}
	var req UpdateDeployEnvironmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	updated, err := h.Queries.UpdateDeployEnvironment(r.Context(), db.UpdateDeployEnvironmentParams{
		ID:           env.ID,
		Name:         ptrToText(req.Name),
		TargetBranch: ptrToText(req.TargetBranch),
		TargetUrl:    ptrToText(req.TargetURL),
		AutoPromote:  pgBoolPtr(req.AutoPromote),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update deploy environment")
		return
	}
	writeJSON(w, http.StatusOK, deployEnvironmentToResponse(updated))
}

// LogDeployRequest is the body for POST /api/deploy_environments/{id}/deploys.
// Manual logging endpoint — used by the user to record a deploy that
// happened outside Multica (e.g. Vercel CLI, GitHub Actions). Webhook
// ingestion lands later.
type LogDeployRequest struct {
	Ref          string  `json:"ref"`
	SHA          string  `json:"sha"`
	Status       string  `json:"status"`
	LogURL       *string `json:"log_url"`
	ErrorMessage *string `json:"error_message"`
}

func (h *Handler) LogDeploy(w http.ResponseWriter, r *http.Request) {
	env, wsID, ok := h.loadDeployEnvironment(w, r)
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req LogDeployRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	status, ok := normalizeDeployStatus(req.Status)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid status")
		return
	}
	if strings.TrimSpace(req.SHA) == "" {
		writeError(w, http.StatusBadRequest, "sha is required")
		return
	}
	ref := strings.TrimSpace(req.Ref)
	if ref == "" {
		ref = env.TargetBranch
	}

	creator, _ := h.parseUserUUIDOrZero(userID)
	// Manual logging endpoint, so we don't have separate "started" /
	// "completed" calls to thread through. Synthesize the timestamps from
	// the status: anything past 'pending' has started; terminal states are
	// also completed.
	nowTs := pgtype.Timestamptz{Time: time.Now(), Valid: true}
	startedAt := pgtype.Timestamptz{}
	completedAt := pgtype.Timestamptz{}
	switch status {
	case db.DeployStatusPending:
		// Neither populated.
	case db.DeployStatusInProgress:
		startedAt = nowTs
	case db.DeployStatusSucceeded, db.DeployStatusFailed, db.DeployStatusRolledBack:
		startedAt = nowTs
		completedAt = nowTs
	}

	deploy, err := h.Queries.InsertDeploy(r.Context(), db.InsertDeployParams{
		WorkspaceID:   wsID,
		EnvironmentID: env.ID,
		Ref:           ref,
		Sha:           strings.TrimSpace(req.SHA),
		Status:        status,
		TriggeredBy:   creator,
		StartedAt:     startedAt,
		CompletedAt:   completedAt,
		LogUrl:        ptrToText(req.LogURL),
		ErrorMessage:  ptrToText(req.ErrorMessage),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to log deploy")
		return
	}

	// On success, bump the parent environment's current_sha so the "what's
	// running" answer becomes a single column read for subsequent loads.
	if status == db.DeployStatusSucceeded {
		_, _ = h.Queries.UpdateDeployEnvironmentCurrent(r.Context(), db.UpdateDeployEnvironmentCurrentParams{
			ID:                env.ID,
			CurrentSha:        pgtype.Text{String: deploy.Sha, Valid: true},
			CurrentDeployedAt: deploy.TriggeredAt,
		})
	}

	eventType := protocol.EventDeployStarted
	if status == db.DeployStatusSucceeded || status == db.DeployStatusFailed || status == db.DeployStatusRolledBack {
		eventType = protocol.EventDeployCompleted
	}
	h.publish(eventType, uuidToString(wsID), "member", userID, map[string]any{
		"deploy":         deployToResponse(deploy),
		"environment_id": uuidToString(env.ID),
	})
	writeJSON(w, http.StatusCreated, deployToResponse(deploy))
}

// ListDeploys returns the most recent deploy attempts for an environment,
// newest first. Default limit 20.
func (h *Handler) ListDeploys(w http.ResponseWriter, r *http.Request) {
	env, _, ok := h.loadDeployEnvironment(w, r)
	if !ok {
		return
	}
	limit := int32(20)
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = int32(n)
		}
	}
	rows, err := h.Queries.ListRecentDeploysByEnvironment(r.Context(), db.ListRecentDeploysByEnvironmentParams{
		EnvironmentID: env.ID,
		Limit:         limit,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list deploys")
		return
	}
	out := make([]deployResponse, len(rows))
	for i, d := range rows {
		out[i] = deployToResponse(d)
	}
	writeJSON(w, http.StatusOK, map[string]any{"deploys": out, "total": len(out)})
}

// normalizeDeployKind validates the user-supplied kind string against the
// pgtype enum. Returning the typed value lets handlers round-trip it back
// into sqlc params without an extra cast.
func normalizeDeployKind(s string) (db.DeployEnvironmentKind, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "staging":
		return db.DeployEnvironmentKindStaging, true
	case "production":
		return db.DeployEnvironmentKindProduction, true
	default:
		return "", false
	}
}

func normalizeDeployStatus(s string) (db.DeployStatus, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "pending":
		return db.DeployStatusPending, true
	case "in_progress":
		return db.DeployStatusInProgress, true
	case "succeeded":
		return db.DeployStatusSucceeded, true
	case "failed":
		return db.DeployStatusFailed, true
	case "rolled_back":
		return db.DeployStatusRolledBack, true
	default:
		return "", false
	}
}

// pgBoolPtr converts a *bool to pgtype.Bool — nil → Valid=false (leaves
// column unchanged in COALESCE-style updates).
func pgBoolPtr(b *bool) pgtype.Bool {
	if b == nil {
		return pgtype.Bool{}
	}
	return pgtype.Bool{Bool: *b, Valid: true}
}

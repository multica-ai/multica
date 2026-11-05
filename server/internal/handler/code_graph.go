package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/pkg/codegraph"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type CodeGraphRepositoryResponse struct {
	AuthMode          string                   `json:"auth_mode"`
	CredentialID      *string                  `json:"credential_id,omitempty"`
	Progress          *CodeGraphBuildResponse  `json:"progress,omitempty"`
	Builds            []CodeGraphBuildResponse `json:"builds,omitempty"`
	ActiveBuildID     *string                  `json:"active_build_id,omitempty"`
	PublicReadEnabled bool                     `json:"public_read_enabled"`
	SymbolCount       int32                    `json:"symbol_count"`
	EdgeCount         int32                    `json:"edge_count"`
	Agents            []CodeGraphAssignedAgent `json:"agents,omitempty"`
	ID                string                   `json:"id"`
	WorkspaceID       string                   `json:"workspace_id"`
	RepoURL           string                   `json:"repo_url"`
	RepoName          string                   `json:"repo_name"`
	Branch            string                   `json:"branch"`
	Status            string                   `json:"status"`
	InternalStatus    *string                  `json:"internal_status,omitempty"`
	CommitHash        *string                  `json:"commit_hash,omitempty"`
	FileCount         int32                    `json:"file_count"`
	SyncError         *string                  `json:"sync_error,omitempty"`
	LastIndexedAt     *string                  `json:"last_indexed_at,omitempty"`
	CreatedBy         *string                  `json:"created_by,omitempty"`
	CreatedAt         string                   `json:"created_at"`
	UpdatedAt         string                   `json:"updated_at"`
}

type CodeGraphBuildResponse struct {
	Phase           string  `json:"phase"`
	CancelRequested bool    `json:"cancel_requested"`
	StartedAt       *string `json:"started_at,omitempty"`
	FinishedAt      *string `json:"finished_at,omitempty"`
	PhaseStartedAt  string  `json:"phase_started_at"`
	ID              string  `json:"id"`
	Status          string  `json:"status"`
	Commit          string  `json:"commit"`
	CreatedAt       string  `json:"created_at"`
	Error           *string `json:"error,omitempty"`
}

type CodeGraphAssignedAgent struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
}

type CodeGraphStatsResponse struct {
	Total      int32 `json:"total"`
	Ready      int32 `json:"ready"`
	Processing int32 `json:"processing"`
	Pending    int32 `json:"pending"`
	Failed     int32 `json:"failed"`
	Files      int32 `json:"files"`
}

type CodeGraphRepositoryListResponse struct {
	Items []CodeGraphRepositoryResponse `json:"items"`
	Stats CodeGraphStatsResponse        `json:"stats"`
}

type createCodeGraphRepositoryRequest struct {
	AuthMode     string `json:"auth_mode"`
	CredentialID string `json:"credential_id"`
	RepoURL      string `json:"repo_url"`
	RepoName     string `json:"repo_name"`
	Branch       string `json:"branch"`
}

type assignCodeGraphRepositoryRequest struct {
	AgentID string `json:"agent_id"`
}

type CodeGraphRepositoryAssignmentResponse struct {
	RepositoryID string `json:"repository_id"`
	AgentID      string `json:"agent_id"`
	CreatedAt    string `json:"created_at"`
}

type codeGraphRepositoryRow struct {
	AuthMode          string
	CredentialID      pgtype.UUID
	ActiveBuildID     pgtype.UUID
	PublicReadEnabled bool
	SymbolCount       int32
	EdgeCount         int32
	ID                pgtype.UUID
	WorkspaceID       pgtype.UUID
	RepoURL           string
	RepoName          string
	Branch            string
	Status            string
	InternalStatus    pgtype.Text
	CommitHash        pgtype.Text
	FileCount         int32
	SyncError         pgtype.Text
	LastIndexedAt     pgtype.Timestamptz
	CreatedBy         pgtype.UUID
	CreatedAt         pgtype.Timestamptz
	UpdatedAt         pgtype.Timestamptz
}

func codeGraphRepositoryToResponse(row codeGraphRepositoryRow) CodeGraphRepositoryResponse {
	return CodeGraphRepositoryResponse{
		AuthMode: row.AuthMode, CredentialID: uuidToPtr(row.CredentialID),
		ActiveBuildID: uuidToPtr(row.ActiveBuildID), PublicReadEnabled: row.PublicReadEnabled, SymbolCount: row.SymbolCount, EdgeCount: row.EdgeCount,
		ID:             uuidToString(row.ID),
		WorkspaceID:    uuidToString(row.WorkspaceID),
		RepoURL:        row.RepoURL,
		RepoName:       row.RepoName,
		Branch:         row.Branch,
		Status:         row.Status,
		InternalStatus: textToPtr(row.InternalStatus),
		CommitHash:     textToPtr(row.CommitHash),
		FileCount:      row.FileCount,
		SyncError:      textToPtr(row.SyncError),
		LastIndexedAt:  timestampToPtr(row.LastIndexedAt),
		CreatedBy:      uuidToPtr(row.CreatedBy),
		CreatedAt:      timestampToString(row.CreatedAt),
		UpdatedAt:      timestampToString(row.UpdatedAt),
	}
}

func scanCodeGraphRepository(row pgx.Row) (codeGraphRepositoryRow, error) {
	var out codeGraphRepositoryRow
	err := row.Scan(
		&out.ID,
		&out.WorkspaceID,
		&out.RepoURL,
		&out.RepoName,
		&out.Branch,
		&out.Status,
		&out.InternalStatus,
		&out.CommitHash,
		&out.FileCount,
		&out.SyncError,
		&out.LastIndexedAt,
		&out.CreatedBy,
		&out.CreatedAt,
		&out.UpdatedAt,
		&out.ActiveBuildID, &out.PublicReadEnabled, &out.SymbolCount, &out.EdgeCount,
		&out.AuthMode, &out.CredentialID,
	)
	return out, err
}

func codeGraphSelectColumns() string {
	return `id, workspace_id, repo_url, repo_name, branch, status, internal_status, commit_hash, file_count, sync_error, last_indexed_at, created_by, created_at, updated_at, active_build_id, public_read_enabled, symbol_count, edge_count, auth_mode, credential_id`
}

func (h *Handler) requireCodeGraphWriter(w http.ResponseWriter, r *http.Request, workspaceID string) bool {
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return false
	}
	if actorType, _ := h.resolveActor(r, requestUserID(r), workspaceID); actorType == "agent" {
		writeError(w, http.StatusForbidden, "agents cannot modify code graph repositories")
		return false
	}
	if !roleAllowed(member.Role, "owner", "admin") {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return false
	}
	return true
}

func (h *Handler) ListWorkspaceCodeGraphs(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	idUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}

	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if err := h.reconcileCodeGraphCheckouts(r.Context(), idUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check repository storage")
		return
	}
	if status != "" && status != "pending" && status != "processing" && status != "ready" && status != "failed" {
		writeError(w, http.StatusBadRequest, "invalid status")
		return
	}
	search := strings.TrimSpace(r.URL.Query().Get("search"))

	rows, err := h.DB.Query(r.Context(), `
		SELECT `+codeGraphSelectColumns()+`
		FROM code_graph_repository
		WHERE workspace_id = $1
		  AND ($2 = '' OR status = $2)
		  AND ($3 = '' OR repo_name ILIKE '%' || $3 || '%' OR branch ILIKE '%' || $3 || '%' OR repo_url ILIKE '%' || $3 || '%' OR id::text = $3)
		ORDER BY updated_at DESC, repo_name ASC
	`, idUUID, status, search)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list code graph repositories")
		return
	}
	defer rows.Close()

	items := make([]CodeGraphRepositoryResponse, 0)
	for rows.Next() {
		row, err := scanCodeGraphRepository(rows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read code graph repository")
			return
		}
		items = append(items, codeGraphRepositoryToResponse(row))
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list code graph repositories")
		return
	}

	rows.Close()
	assignments, err := h.codeGraphAssignmentSummaries(r, idUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load assigned agents")
		return
	}
	for i := range items {
		items[i].Agents = assignments[items[i].ID]
	}
	progressRows, err := h.DB.Query(r.Context(), `SELECT DISTINCT ON (repository_id) repository_id,id,status,commit_hash,created_at,error,phase,cancel_requested,started_at,finished_at,phase_started_at FROM code_graph_build WHERE workspace_id=$1 ORDER BY repository_id,created_at DESC`, idUUID)
	if err != nil {
		writeError(w, 500, "Cannot load build progress")
		return
	}
	progress := make(map[string]*CodeGraphBuildResponse)
	for progressRows.Next() {
		var repo, build pgtype.UUID
		var created, started, finished, phaseAt pgtype.Timestamptz
		var failure pgtype.Text
		item := &CodeGraphBuildResponse{}
		if err := progressRows.Scan(&repo, &build, &item.Status, &item.Commit, &created, &failure, &item.Phase, &item.CancelRequested, &started, &finished, &phaseAt); err != nil {
			progressRows.Close()
			writeError(w, 500, "Cannot read progress")
			return
		}
		item.ID = uuidToString(build)
		item.CreatedAt = timestampToString(created)
		item.Error = textToPtr(failure)
		item.PhaseStartedAt = timestampToString(phaseAt)
		if started.Valid {
			value := timestampToString(started)
			item.StartedAt = &value
		}
		if finished.Valid {
			value := timestampToString(finished)
			item.FinishedAt = &value
		}
		progress[uuidToString(repo)] = item
	}
	err = progressRows.Err()
	progressRows.Close()
	if err != nil {
		writeError(w, 500, "Cannot read progress")
		return
	}
	for i := range items {
		items[i].Progress = progress[items[i].ID]
	}
	stats, err := h.codeGraphStats(r.Context(), idUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load code graph stats")
		return
	}
	writeJSON(w, http.StatusOK, CodeGraphRepositoryListResponse{Items: items, Stats: stats})
}

func (h *Handler) CreateWorkspaceCodeGraph(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	idUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if !h.requireCodeGraphWriter(w, r, workspaceID) {
		return
	}

	var req createCodeGraphRepositoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	repoURL := strings.TrimSpace(req.RepoURL)
	branch := strings.TrimSpace(req.Branch)
	if branch == "" {
		branch = "main"
	}
	parsedURL, _ := url.Parse(repoURL)
	if repoURL == "" || !isValidGitRepoURL(repoURL) || (parsedURL != nil && parsedURL.User != nil && parsedURL.Scheme != "ssh") || strings.ContainsAny(repoURL, "\r\n") || len(branch) > 200 || strings.HasPrefix(branch, "-") {
		writeError(w, http.StatusBadRequest, "repo_url must be a valid http(s) or ssh git URL")
		return
	}
	repoName := strings.TrimSpace(req.RepoName)
	if repoName == "" {
		repoName = deriveCodeGraphRepoName(repoURL)
	}
	if repoName == "" {
		writeError(w, http.StatusBadRequest, "repo_name is required")
		return
	}

	userUUID, ok := parseUUIDOrBadRequest(w, requestUserID(r), "user id")
	if !ok {
		return
	}
	authRequest := graphAuthRequest{Mode: req.AuthMode, CredentialID: req.CredentialID, RepoURL: repoURL, Branch: branch}
	if err := h.verifyGraphAuth(r.Context(), idUUID, authRequest); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "Cannot save repository")
		return
	}
	defer tx.Rollback(r.Context())
	var cid any
	if req.AuthMode == "token" {
		parsed, _ := parseCredentialUUID(req.CredentialID)
		cid = parsed
		var locked string
		if tx.QueryRow(r.Context(), `SELECT id::text FROM code_graph_git_credential WHERE id=$1 AND workspace_id=$2 FOR UPDATE`, parsed, idUUID).Scan(&locked) != nil {
			writeError(w, 409, "Git credential changed; try again")
			return
		}
	}
	row, err := scanCodeGraphRepository(tx.QueryRow(r.Context(), `
		INSERT INTO code_graph_repository (workspace_id, repo_url, repo_name, branch, created_by,auth_mode,credential_id)
		VALUES ($1, $2, $3, $4, $5,$6,$7)
 ON CONFLICT(workspace_id,repo_url,branch) DO NOTHING
		RETURNING `+codeGraphSelectColumns(),
		idUUID, repoURL, repoName, branch, userUUID, req.AuthMode, cid,
	))
	statusCode := http.StatusCreated
	if errors.Is(err, pgx.ErrNoRows) {
		row, err = scanCodeGraphRepository(tx.QueryRow(r.Context(), `
				SELECT `+codeGraphSelectColumns()+`
				FROM code_graph_repository
				WHERE workspace_id = $1 AND repo_url = $2 AND branch = $3
			`, idUUID, repoURL, branch))
		statusCode = http.StatusOK
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create code graph repository")
		return
	}
	if tx.Commit(r.Context()) != nil {
		writeError(w, 500, "Cannot save repository")
		return
	}
	if statusCode == http.StatusCreated {
		if err := h.queueCodeGraph(r.Context(), row); err != nil {
			writeError(w, 500, "Repository saved but build could not be queued; synchronize to retry")
			return
		}
		row.InternalStatus = pgtype.Text{String: "queued", Valid: true}
	}
	writeJSON(w, statusCode, codeGraphRepositoryToResponse(row))
}

func (h *Handler) GetWorkspaceCodeGraph(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	idUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	if err := h.reconcileCodeGraphCheckouts(r.Context(), idUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check repository storage")
		return
	}
	repoUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "codeGraphId"), "code graph id")
	if !ok {
		return
	}
	row, err := scanCodeGraphRepository(h.DB.QueryRow(r.Context(), `
		SELECT `+codeGraphSelectColumns()+`
		FROM code_graph_repository
		WHERE id = $1 AND workspace_id = $2
	`, repoUUID, idUUID))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "code graph repository not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get code graph repository")
		return
	}
	response := codeGraphRepositoryToResponse(row)
	buildRows, err := h.DB.Query(r.Context(), `SELECT id,status,commit_hash,created_at,error,phase,cancel_requested,started_at,finished_at,phase_started_at FROM code_graph_build WHERE repository_id=$1 AND workspace_id=$2 ORDER BY created_at DESC LIMIT 20`, repoUUID, idUUID)
	if err != nil {
		writeError(w, 500, "Cannot load build history")
		return
	}
	for buildRows.Next() {
		var id pgtype.UUID
		var created pgtype.Timestamptz
		var started, finished, phaseAt pgtype.Timestamptz
		var failure pgtype.Text
		var item CodeGraphBuildResponse
		if err := buildRows.Scan(&id, &item.Status, &item.Commit, &created, &failure, &item.Phase, &item.CancelRequested, &started, &finished, &phaseAt); err != nil {
			buildRows.Close()
			writeError(w, 500, "Cannot read build history")
			return
		}
		item.ID = uuidToString(id)
		item.CreatedAt = timestampToString(created)
		item.Error = textToPtr(failure)
		item.PhaseStartedAt = timestampToString(phaseAt)
		if started.Valid {
			value := timestampToString(started)
			item.StartedAt = &value
		}
		if finished.Valid {
			value := timestampToString(finished)
			item.FinishedAt = &value
		}
		response.Builds = append(response.Builds, item)
		if response.Progress == nil {
			response.Progress = &item
		}
	}
	err = buildRows.Err()
	buildRows.Close()
	if err != nil {
		writeError(w, 500, "Cannot load build history")
		return
	}
	rows, err := h.DB.Query(r.Context(), `SELECT a.id, a.name, b.created_at
		FROM agent_code_graph_repository b
		JOIN agent a ON a.id = b.agent_id
		WHERE b.repository_id = $1 AND a.workspace_id = $2 AND a.archived_at IS NULL
		ORDER BY a.name, a.id`, repoUUID, idUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load assigned agents")
		return
	}
	defer rows.Close()
	response.Agents = make([]CodeGraphAssignedAgent, 0)
	for rows.Next() {
		var agentID pgtype.UUID
		var name string
		var createdAt pgtype.Timestamptz
		if err := rows.Scan(&agentID, &name, &createdAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read assigned agents")
			return
		}
		response.Agents = append(response.Agents, CodeGraphAssignedAgent{ID: uuidToString(agentID), Name: name, CreatedAt: timestampToString(createdAt)})
	}
	if rows.Err() != nil {
		writeError(w, http.StatusInternalServerError, "failed to load assigned agents")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) UnassignWorkspaceCodeGraphAgent(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	if !h.requireCodeGraphWriter(w, r, workspaceID) {
		return
	}
	ws, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	repo, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "codeGraphId"), "code graph id")
	if !ok {
		return
	}
	agent, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "agentId"), "agent id")
	if !ok {
		return
	}
	target, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{ID: agent, WorkspaceID: ws})
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	if !h.canManageAgent(w, r, target) {
		return
	}
	tag, err := h.DB.Exec(r.Context(), `DELETE FROM agent_code_graph_repository b
		USING code_graph_repository r WHERE b.repository_id = r.id
		AND r.id = $1 AND r.workspace_id = $2 AND b.agent_id = $3`, repo, ws, agent)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove assignment")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "assignment not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) SyncWorkspaceCodeGraph(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	idUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if !h.requireCodeGraphWriter(w, r, workspaceID) {
		return
	}
	repoUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "codeGraphId"), "code graph id")
	if !ok {
		return
	}
	row, err := scanCodeGraphRepository(h.DB.QueryRow(r.Context(), `
		SELECT `+codeGraphSelectColumns()+` FROM code_graph_repository WHERE id=$1 AND workspace_id=$2`,
		repoUUID, idUUID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "code graph repository not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to queue code graph sync")
		return
	}
	if err := h.queueCodeGraph(r.Context(), row); err != nil {
		writeError(w, 500, "Failed to queue graph build")
		return
	}
	row.Status = "pending"
	row.InternalStatus = pgtype.Text{String: "queued", Valid: true}
	writeJSON(w, http.StatusAccepted, codeGraphRepositoryToResponse(row))
}

func (h *Handler) DeleteWorkspaceCodeGraph(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	idUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if !h.requireCodeGraphWriter(w, r, workspaceID) {
		return
	}
	repoUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "codeGraphId"), "code graph id")
	if !ok {
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete code graph repository")
		return
	}
	defer tx.Rollback(r.Context())
	// Serialize deletion with enqueue and reject deletion of a running snapshot.
	if _, err := tx.Exec(r.Context(), `SELECT id FROM code_graph_repository WHERE id=$1 AND workspace_id=$2 FOR UPDATE`, repoUUID, idUUID); err != nil {
		writeError(w, 500, "Cannot lock repository")
		return
	}
	var active bool
	if err := tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM code_graph_build WHERE repository_id=$1 AND status IN ('queued','processing'))`, repoUUID).Scan(&active); err != nil {
		writeError(w, 500, "Cannot check build")
		return
	}
	if active {
		writeError(w, 409, "Wait for the active build before deleting")
		return
	}
	if _, err := tx.Exec(r.Context(), `DELETE FROM code_graph_build WHERE repository_id IN(SELECT id FROM code_graph_repository WHERE id=$1 AND workspace_id=$2)`, repoUUID, idUUID); err != nil {
		writeError(w, 500, "Cannot remove build history")
		return
	}
	if _, err := tx.Exec(r.Context(), `
		DELETE FROM agent_code_graph_repository
		WHERE repository_id IN (
			SELECT id FROM code_graph_repository WHERE id = $1 AND workspace_id = $2
		)
	`, repoUUID, idUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete code graph repository")
		return
	}
	tag, err := tx.Exec(r.Context(), `DELETE FROM code_graph_repository WHERE id = $1 AND workspace_id = $2`, repoUUID, idUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete code graph repository")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "code graph repository not found")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete code graph repository")
		return
	}
	_ = os.RemoveAll(codeGraphCheckoutDir(uuidToString(idUUID), uuidToString(repoUUID)))
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) AssignWorkspaceCodeGraphToAgent(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	idUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if !h.requireCodeGraphWriter(w, r, workspaceID) {
		return
	}
	repoUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "codeGraphId"), "code graph id")
	if !ok {
		return
	}

	var req assignCodeGraphRepositoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	agentUUID, ok := parseUUIDOrBadRequest(w, req.AgentID, "agent id")
	if !ok {
		return
	}
	agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{ID: agentUUID, WorkspaceID: idUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	if !h.canManageAgent(w, r, agent) {
		return
	}
	runtime, err := h.Queries.GetAgentRuntime(r.Context(), agent.RuntimeID)
	if err != nil || !codegraph.SupportsLocalTools(runtime.RuntimeMode, runtime.Provider) {
		writeError(w, http.StatusUnprocessableEntity, "this runtime cannot receive CodeGraph tools; choose a runtime that supports local tools")
		return
	}
	userUUID, _ := parseUUIDOrBadRequest(w, requestUserID(r), "user id")

	var createdAt pgtype.Timestamptz
	err = h.DB.QueryRow(r.Context(), `
		WITH repo AS (
			SELECT id
			FROM code_graph_repository
			WHERE id = $1 AND workspace_id = $2
		),
		target_agent AS (
			SELECT id
			FROM agent
			WHERE id = $3 AND workspace_id = $2 AND archived_at IS NULL
		),
		ins AS (
			INSERT INTO agent_code_graph_repository (repository_id, agent_id, created_by)
			SELECT repo.id, target_agent.id, $4
			FROM repo, target_agent
			ON CONFLICT (agent_id, repository_id) DO NOTHING
			RETURNING created_at
		)
		SELECT created_at FROM ins
		UNION ALL
		SELECT acgr.created_at
		FROM agent_code_graph_repository acgr
		JOIN repo ON repo.id = acgr.repository_id
		JOIN target_agent ON target_agent.id = acgr.agent_id
		LIMIT 1
	`, repoUUID, idUUID, agentUUID, userUUID).Scan(&createdAt)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "code graph repository or agent not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to assign code graph repository")
		return
	}

	writeJSON(w, http.StatusOK, CodeGraphRepositoryAssignmentResponse{
		RepositoryID: uuidToString(repoUUID),
		AgentID:      uuidToString(agentUUID),
		CreatedAt:    timestampToString(createdAt),
	})
}

func (h *Handler) codeGraphStats(ctx context.Context, workspaceID pgtype.UUID) (CodeGraphStatsResponse, error) {
	rows, err := h.DB.Query(ctx, `
		SELECT status, count(*)::int, COALESCE(sum(file_count), 0)::int
		FROM code_graph_repository
		WHERE workspace_id = $1
		GROUP BY status
	`, workspaceID)
	if err != nil {
		return CodeGraphStatsResponse{}, err
	}
	defer rows.Close()
	var stats CodeGraphStatsResponse
	for rows.Next() {
		var status string
		var count int32
		var files int32
		if err := rows.Scan(&status, &count, &files); err != nil {
			return CodeGraphStatsResponse{}, err
		}
		stats.Total += count
		stats.Files += files
		switch status {
		case "ready":
			stats.Ready = count
		case "processing":
			stats.Processing = count
		case "pending":
			stats.Pending = count
		case "failed":
			stats.Failed = count
		}
	}
	return stats, rows.Err()
}

func deriveCodeGraphRepoName(repoURL string) string {
	repoURL = strings.TrimSuffix(strings.TrimSpace(repoURL), "/")
	if repoURL == "" {
		return ""
	}
	parts := strings.FieldsFunc(repoURL, func(r rune) bool { return r == '/' || r == ':' })
	if len(parts) == 0 {
		return ""
	}
	return strings.TrimSuffix(parts[len(parts)-1], ".git")
}

func codeGraphCheckoutDir(workspaceID, repoID string) string {
	if root := os.Getenv("MULTICA_CODEGRAPH_ROOT"); root != "" {
		return filepath.Join(root, workspaceID, repoID)
	}
	root, err := os.UserCacheDir()
	if err != nil || root == "" {
		root = os.TempDir()
	}
	return filepath.Join(root, "multica", "code-graph", workspaceID, repoID)
}

func (h *Handler) markCodeGraphFailed(ctx context.Context, repo codeGraphRepositoryRow, status string, err error) {
	msg := err.Error()
	if len(msg) > 1000 {
		msg = msg[:1000]
	}
	_, _ = h.DB.Exec(ctx, `
		UPDATE code_graph_repository
		SET status = 'failed', internal_status = $3, sync_error = $4, updated_at = now()
		WHERE id = $1 AND workspace_id = $2
	`, repo.ID, repo.WorkspaceID, status, msg)
}

type codeGraphGitAuth struct {
	username string
	password string
}

func (h *Handler) codeGraphGitAuthEnv(ctx context.Context, workspaceID pgtype.UUID, repoURL string, branch ...string) ([]string, func(), func(string) string, error) {
	cleanup := func() {}
	auth, err := h.matchCodeGraphGitAuth(ctx, workspaceID, repoURL, branch...)
	if err != nil || auth == nil {
		if err != nil {
			return nil, cleanup, nil, err
		}
	}
	return graphGitAuthEnvironment(auth)
}

func graphGitAuthEnvironment(auth *codeGraphGitAuth) ([]string, func(), func(string) string, error) {
	cleanup := func() {}
	if auth == nil {
		return []string{"GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=/bin/false", "GIT_TRACE=0", "GIT_TRACE_CURL=0", "GIT_CURL_VERBOSE=0"}, cleanup, nil, nil
	}
	script, err := os.CreateTemp("", "multica-codegraph-askpass-*")
	if err != nil {
		return nil, cleanup, nil, err
	}
	scriptPath := script.Name()
	body := `#!/bin/sh
case "$1" in
  *Username*) printf '%s\n' "$MULTICA_CODEGRAPH_GIT_USERNAME" ;;
  *Password*) printf '%s\n' "$MULTICA_CODEGRAPH_GIT_PASSWORD" ;;
  *) printf '\n' ;;
esac
`
	if _, err := script.WriteString(body); err != nil {
		_ = script.Close()
		_ = os.Remove(scriptPath)
		return nil, cleanup, nil, err
	}
	if err := script.Close(); err != nil {
		_ = os.Remove(scriptPath)
		return nil, cleanup, nil, err
	}
	if err := os.Chmod(scriptPath, 0o700); err != nil {
		_ = os.Remove(scriptPath)
		return nil, cleanup, nil, err
	}
	cleanup = func() { _ = os.Remove(scriptPath) }
	redactor := func(s string) string {
		s = strings.ReplaceAll(s, auth.password, "[redacted]")
		s = strings.ReplaceAll(s, url.QueryEscape(auth.password), "[redacted]")
		if auth.username != "" {
			s = strings.ReplaceAll(s, auth.username, "[redacted]")
			s = strings.ReplaceAll(s, url.QueryEscape(auth.username), "[redacted]")
		}
		return s
	}
	return []string{
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ASKPASS=" + scriptPath,
		"MULTICA_CODEGRAPH_GIT_USERNAME=" + auth.username,
		"MULTICA_CODEGRAPH_GIT_PASSWORD=" + auth.password,
		"GIT_TRACE=0", "GIT_TRACE_CURL=0", "GIT_CURL_VERBOSE=0",
	}, cleanup, redactor, nil
}

func (h *Handler) matchCodeGraphGitAuth(ctx context.Context, workspaceID pgtype.UUID, repoURL string, branch ...string) (*codeGraphGitAuth, error) {
	var mode string
	var cid pgtype.UUID
	// URL/branch instances share the same selected credential only when unambiguous.
	query := `SELECT DISTINCT auth_mode,credential_id FROM code_graph_repository WHERE workspace_id=$1 AND repo_url=$2`
	args := []any{workspaceID, repoURL}
	if len(branch) > 0 {
		query += " AND branch=$3"
		args = append(args, branch[0])
	}
	rows, err := h.DB.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	count := 0
	for rows.Next() {
		if err = rows.Scan(&mode, &cid); err != nil {
			rows.Close()
			return nil, err
		}
		count++
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if count != 1 {
		return nil, errors.New("Repository authentication must be explicitly bound")
	}
	credential := ""
	if cid.Valid {
		credential = uuidToString(cid)
	}
	return h.resolveGraphAuth(ctx, workspaceID, graphAuthRequest{Mode: mode, CredentialID: credential, RepoURL: repoURL})
}

func codeGraphVCSConnectionMatchesRepo(conn db.VcsConnection, repo *url.URL) bool {
	instance, err := url.Parse(conn.InstanceUrl)
	if err != nil || instance.Host == "" {
		return false
	}
	if !strings.EqualFold(instance.Host, repo.Host) {
		return false
	}
	instancePath := strings.Trim(strings.TrimSuffix(instance.Path, "/"), "/")
	if instancePath == "" {
		return true
	}
	repoPath := strings.Trim(repo.Path, "/")
	return repoPath == instancePath || strings.HasPrefix(repoPath, instancePath+"/")
}

func defaultCodeGraphGitUsername(provider string) string {
	if provider == "gitlab" {
		return "oauth2"
	}
	return "git"
}

func countCodeGraphFiles(root string) (int32, error) {
	var count int32
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if !d.IsDir() {
			count++
		}
		return nil
	})
	return count, err
}

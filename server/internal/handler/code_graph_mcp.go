package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/multica-ai/multica/server/pkg/codegraph"
)

type agentCodeGraphStore struct {
	h           *Handler
	workspaceID pgtype.UUID
	agentID     pgtype.UUID
}

func (s agentCodeGraphStore) List(ctx context.Context) ([]codegraph.Repository, error) {
	if err := s.h.reconcileCodeGraphCheckouts(ctx, s.workspaceID); err != nil {
		return nil, errors.New("failed to check repository storage")
	}
	rows, err := s.h.DB.Query(ctx, `SELECT r.id::text, r.repo_name, r.branch, r.status, COALESCE(r.commit_hash, ''), COALESCE(r.active_build_id::text,'')
 FROM code_graph_repository r
 JOIN agent_code_graph_repository b ON b.repository_id = r.id
 JOIN agent a ON a.id = b.agent_id AND a.workspace_id = r.workspace_id AND a.archived_at IS NULL
 WHERE r.workspace_id = $1 AND a.id = $2 ORDER BY r.repo_name, r.id`, s.workspaceID, s.agentID)
	if err != nil {
		return nil, errors.New("failed to load repository assignments")
	}
	defer rows.Close()
	repos := make([]codegraph.Repository, 0)
	for rows.Next() {
		var repo codegraph.Repository
		if err := rows.Scan(&repo.ID, &repo.Name, &repo.Branch, &repo.Status, &repo.Commit, &repo.ActiveBuildID); err != nil {
			return nil, err
		}
		repos = append(repos, repo)
	}
	return repos, rows.Err()
}

func (s agentCodeGraphStore) Checkout(ctx context.Context, repositoryID string) (string, error) {
	if err := s.h.reconcileCodeGraphCheckouts(ctx, s.workspaceID); err != nil {
		return "", errors.New("failed to check repository storage")
	}
	// Compare as text so untrusted tool arguments cannot trigger UUID parser panics.
	var id, status string
	var build pgtype.UUID
	err := s.h.DB.QueryRow(ctx, `SELECT r.id::text, r.status, r.active_build_id FROM code_graph_repository r
 JOIN agent_code_graph_repository b ON b.repository_id = r.id
 JOIN agent a ON a.id = b.agent_id AND a.workspace_id = r.workspace_id AND a.archived_at IS NULL
 WHERE r.workspace_id = $1 AND a.id = $2 AND r.id::text = $3`, s.workspaceID, s.agentID, repositoryID).Scan(&id, &status, &build)
	if err != nil {
		return "", errors.New("repository unavailable or not assigned to this agent")
	}
	if status != "ready" && !build.Valid {
		return "", errors.New("repository is not ready; synchronize it before querying")
	}
	if build.Valid {
		return codeGraphBuildDir(uuidToString(s.workspaceID), id, uuidToString(build)), nil
	}
	return codeGraphCheckoutDir(uuidToString(s.workspaceID), id), nil
}

func (s agentCodeGraphStore) Query(ctx context.Context, input codegraph.QueryInput) (codegraph.QueryResult, error) {
	var row codeGraphRepositoryRow
	err := s.h.DB.QueryRow(ctx, `SELECT r.id,r.repo_name,r.active_build_id FROM code_graph_repository r
 WHERE r.workspace_id=$1 AND r.id::text=$3 AND EXISTS (
 SELECT 1 FROM agent_code_graph_repository b JOIN agent a ON a.id=b.agent_id
 WHERE b.repository_id=r.id AND a.id=$2 AND a.workspace_id=r.workspace_id AND a.archived_at IS NULL)`, s.workspaceID, s.agentID, input.RepositoryID).Scan(&row.ID, &row.RepoName, &row.ActiveBuildID)
	if err != nil {
		return codegraph.QueryResult{}, errors.New("Repository unavailable or not assigned to this agent")
	}
	row.WorkspaceID = s.workspaceID
	result, _, err := s.h.executeCodeGraphQuery(ctx, row, input)
	return result, err
}

// Mounted behind Auth: identities below are stamped from the task token, never tool input.
func (h *Handler) CodeGraphMCP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Actor-Source") != "task_token" {
		writeError(w, http.StatusForbidden, "CodeGraph MCP requires an agent task token")
		return
	}
	workspaceID, ok := parseUUIDOrBadRequest(w, r.Header.Get("X-Workspace-ID"), "workspace id")
	if !ok {
		return
	}
	agentID, ok := parseUUIDOrBadRequest(w, r.Header.Get("X-Agent-ID"), "agent id")
	if !ok {
		return
	}
	store := agentCodeGraphStore{h: h, workspaceID: workspaceID, agentID: agentID}
	server := codegraph.NewServer(store, codegraph.RegisterGraphQueries(store.Query))
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true}).ServeHTTP(w, r)
}

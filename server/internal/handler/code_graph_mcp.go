package handler

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/pkg/codegraph"
	"net/http"
)

type agentCodeGraphStore struct {
	h           *Handler
	workspaceID pgtype.UUID
	agentID     pgtype.UUID
	taskID      pgtype.UUID
}

func (s agentCodeGraphStore) access() service.CodeGraphAccessService {
	return service.CodeGraphAccessService{DB: s.h.DB, WorkspaceID: s.workspaceID, AgentID: s.agentID, TaskID: s.taskID}
}
func (s agentCodeGraphStore) List(ctx context.Context) ([]codegraph.Repository, error) {
	if err := s.h.reconcileCodeGraphCheckouts(ctx, s.workspaceID); err != nil {
		return nil, errors.New("failed to check repository storage")
	}
	return s.access().Repositories(ctx, "")
}
func (s agentCodeGraphStore) Checkout(ctx context.Context, repositoryID string) (string, error) {
	if err := s.h.reconcileCodeGraphCheckouts(ctx, s.workspaceID); err != nil {
		return "", errors.New("failed to check repository storage")
	}
	repos, err := s.access().Repositories(ctx, repositoryID)
	if err != nil || len(repos) != 1 {
		return "", errors.New("repository unavailable or not assigned to this run")
	}
	repo := repos[0]
	if repo.ActiveBuildID == "" {
		return "", errors.New("repository has no successful build; synchronize it before querying")
	}
	return codeGraphBuildDir(uuidToString(s.workspaceID), repo.ID, repo.ActiveBuildID), nil
}
func (s agentCodeGraphStore) Query(ctx context.Context, input codegraph.QueryInput) (codegraph.QueryResult, error) {
	repos, err := s.access().Repositories(ctx, input.RepositoryID)
	if err != nil || len(repos) != 1 {
		return codegraph.QueryResult{}, errors.New("repository unavailable or not assigned to this run")
	}
	repo := repos[0]
	row := codeGraphRepositoryRow{ID: parseUUID(repo.ID), WorkspaceID: s.workspaceID, RepoName: repo.Name}
	if repo.ActiveBuildID != "" {
		row.ActiveBuildID = parseUUID(repo.ActiveBuildID)
	}
	result, _, err := s.h.executeCodeGraphQuery(ctx, row, input)
	return result, err
}

// Mounted behind Auth: identities are stamped from the task token, never tool input.
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
	taskID, ok := parseUUIDOrBadRequest(w, r.Header.Get("X-Task-ID"), "task id")
	if !ok {
		return
	}
	store := agentCodeGraphStore{h: h, workspaceID: workspaceID, agentID: agentID, taskID: taskID}
	server := codegraph.NewServer(store, codegraph.RegisterGraphQueries(store.Query))
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true}).ServeHTTP(w, r)
}

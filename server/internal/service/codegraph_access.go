package service

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/pkg/codegraph"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// CodeGraphAccessService resolves live grants for inventory and every tool call.
// TaskID must come from authenticated task context, never an MCP argument.
type CodeGraphAccessService struct {
	DB          db.DBTX
	WorkspaceID pgtype.UUID
	AgentID     pgtype.UUID
	TaskID      pgtype.UUID
}

func (s CodeGraphAccessService) Repositories(ctx context.Context, repositoryID string) ([]codegraph.Repository, error) {
	rows, err := s.DB.Query(ctx, `
WITH principal AS (
 SELECT a.id FROM agent a WHERE a.id=$2 AND a.workspace_id=$1 AND a.archived_at IS NULL
 AND ($3::uuid IS NULL OR EXISTS(SELECT 1 FROM agent_task_queue t WHERE t.id=$3 AND t.agent_id=a.id))
), task_project AS (
 SELECT p.id FROM agent_task_queue t
 JOIN principal a ON a.id=t.agent_id
 LEFT JOIN issue i ON i.id=t.issue_id AND i.workspace_id=$1
 LEFT JOIN chat_session c ON c.id=t.chat_session_id AND c.workspace_id=$1 AND c.agent_id=a.id
 JOIN project p ON p.id=CASE WHEN t.issue_id IS NOT NULL THEN i.project_id ELSE c.project_id END AND p.workspace_id=$1
 WHERE t.id=$3
), grants AS (
 SELECT r.id::text, r.repo_name, r.branch, r.status, COALESCE(r.commit_hash,''), COALESCE(r.active_build_id::text,''),
 ARRAY_REMOVE(ARRAY[
 CASE WHEN r.is_workspace_shared THEN 'workspace' END,
 CASE WHEN EXISTS(SELECT 1 FROM agent_code_graph_repository b WHERE b.repository_id=r.id AND b.agent_id=$2) THEN 'agent' END,
 CASE WHEN EXISTS(SELECT 1 FROM project_code_graph_repository b JOIN task_project p ON p.id=b.project_id WHERE b.repository_id=r.id) THEN 'project' END
 ],NULL) AS sources
 FROM code_graph_repository r WHERE r.workspace_id=$1 AND EXISTS(SELECT 1 FROM principal)
 AND ($4::text='' OR r.id::text=$4)
)
SELECT * FROM grants WHERE cardinality(sources)>0 ORDER BY repo_name,id`, s.WorkspaceID, s.AgentID, s.TaskID, repositoryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	repositories := make([]codegraph.Repository, 0)
	for rows.Next() {
		var repo codegraph.Repository
		if err := rows.Scan(&repo.ID, &repo.Name, &repo.Branch, &repo.Status, &repo.Commit, &repo.ActiveBuildID, &repo.Sources); err != nil {
			return nil, err
		}
		repositories = append(repositories, repo)
	}
	return repositories, rows.Err()
}

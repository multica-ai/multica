package handler

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
)

// Binding summaries follow the same configuration visibility policy as repository details.
func (h *Handler) codeGraphAssignmentSummaries(r *http.Request, workspace pgtype.UUID) (map[string][]CodeGraphAssignedAgent, error) {
	rows, err := h.DB.Query(r.Context(), `SELECT b.repository_id,a.id,a.name,b.created_at
		FROM agent_code_graph_repository b
		JOIN agent a ON a.id=b.agent_id
		JOIN code_graph_repository repo ON repo.id=b.repository_id
		WHERE repo.workspace_id=$1 AND a.workspace_id=$1 AND a.archived_at IS NULL
		ORDER BY a.name,a.id`, workspace)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string][]CodeGraphAssignedAgent)
	for rows.Next() {
		var repo, agent pgtype.UUID
		var name string
		var created pgtype.Timestamptz
		if err := rows.Scan(&repo, &agent, &name, &created); err != nil {
			return nil, err
		}
		key := uuidToString(repo)
		result[key] = append(result[key], CodeGraphAssignedAgent{ID: uuidToString(agent), Name: name, CreatedAt: timestampToString(created)})
	}
	return result, rows.Err()
}

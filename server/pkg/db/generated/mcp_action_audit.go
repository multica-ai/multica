package db

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgtype"
)

// MCPActionAudit is one persisted MCP action. Hand-written beside sqlc output:
// sqlc does not own this file.
type MCPActionAudit struct {
	ID               pgtype.UUID        `json:"id"`
	WorkspaceID      pgtype.UUID        `json:"workspace_id"`
	TaskID           pgtype.UUID        `json:"task_id"`
	AgentID          pgtype.UUID        `json:"agent_id"`
	IssueID          pgtype.UUID        `json:"issue_id"`
	ChatSessionID    pgtype.UUID        `json:"chat_session_id"`
	OriginatorUserID pgtype.UUID        `json:"originator_user_id"`
	Provider         pgtype.Text        `json:"provider"`
	Source           string             `json:"source"`
	MCPServer        pgtype.Text        `json:"mcp_server"`
	ToolName         string             `json:"tool_name"`
	CallID           pgtype.Text        `json:"call_id"`
	Arguments        json.RawMessage    `json:"arguments"`
	Outcome          string             `json:"outcome"`
	Error            pgtype.Text        `json:"error"`
	DurationMs       pgtype.Int4        `json:"duration_ms"`
	CreatedAt        pgtype.Timestamptz `json:"created_at"`
}

const upsertMCPActionAuditSQL = `
INSERT INTO mcp_action_audit (
    workspace_id, task_id, agent_id, issue_id, chat_session_id, originator_user_id,
    provider, source, mcp_server, tool_name, call_id, arguments, outcome, error, duration_ms
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NULLIF($11, ''), NULLIF($12, '')::jsonb, $13, $14, $15
)
ON CONFLICT (task_id, source, call_id) WHERE call_id IS NOT NULL AND call_id <> ''
DO UPDATE SET
    outcome = CASE
        WHEN EXCLUDED.outcome = 'started' AND mcp_action_audit.outcome <> 'started'
            THEN mcp_action_audit.outcome
        ELSE EXCLUDED.outcome
    END,
    error = COALESCE(EXCLUDED.error, mcp_action_audit.error),
    duration_ms = COALESCE(EXCLUDED.duration_ms, mcp_action_audit.duration_ms),
    arguments = COALESCE(mcp_action_audit.arguments, EXCLUDED.arguments),
    mcp_server = COALESCE(mcp_action_audit.mcp_server, EXCLUDED.mcp_server),
    provider = COALESCE(mcp_action_audit.provider, EXCLUDED.provider)
`

func (q *Queries) UpsertMCPActionAudit(ctx context.Context, row MCPActionAudit) error {
	args := ""
	if len(row.Arguments) > 0 {
		args = string(row.Arguments)
	}
	_, err := q.db.Exec(ctx, upsertMCPActionAuditSQL,
		row.WorkspaceID,
		row.TaskID,
		row.AgentID,
		row.IssueID,
		row.ChatSessionID,
		row.OriginatorUserID,
		row.Provider,
		row.Source,
		row.MCPServer,
		row.ToolName,
		row.CallID,
		args,
		row.Outcome,
		row.Error,
		row.DurationMs,
	)
	return err
}

const listMCPActionAuditsSQL = `
SELECT id, workspace_id, task_id, agent_id, issue_id, chat_session_id, originator_user_id,
       provider, source, mcp_server, tool_name, call_id, arguments, outcome, error, duration_ms, created_at
FROM mcp_action_audit
WHERE workspace_id = $1
  AND ($2::uuid IS NULL OR task_id = $2)
  AND ($3::timestamptz IS NULL OR created_at >= $3)
ORDER BY created_at DESC
LIMIT $4
`

func (q *Queries) ListMCPActionAudits(ctx context.Context, workspaceID, taskID pgtype.UUID, since pgtype.Timestamptz, limit int32) ([]MCPActionAudit, error) {
	rows, err := q.db.Query(ctx, listMCPActionAuditsSQL, workspaceID, taskID, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MCPActionAudit
	for rows.Next() {
		var row MCPActionAudit
		if err := rows.Scan(
			&row.ID, &row.WorkspaceID, &row.TaskID, &row.AgentID, &row.IssueID, &row.ChatSessionID, &row.OriginatorUserID,
			&row.Provider, &row.Source, &row.MCPServer, &row.ToolName, &row.CallID, &row.Arguments, &row.Outcome, &row.Error, &row.DurationMs, &row.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// DeleteExpiredMCPActionAudits drops rows older than each workspace's retention.
// defaultDays is used when the workspace has no numeric mcp_action_audit_retention_days setting.
const deleteExpiredMCPActionAuditsSQL = `
DELETE FROM mcp_action_audit AS a
USING workspace AS w
WHERE a.workspace_id = w.id
  AND a.created_at < now() - make_interval(days => COALESCE(
        CASE
            WHEN (w.settings->>'mcp_action_audit_retention_days') ~ '^[0-9]+$'
            THEN LEAST(365, GREATEST(1, (w.settings->>'mcp_action_audit_retention_days')::int))
        END,
        $1::int
      ))
`

func (q *Queries) DeleteExpiredMCPActionAudits(ctx context.Context, defaultDays int32) (int64, error) {
	tag, err := q.db.Exec(ctx, deleteExpiredMCPActionAuditsSQL, defaultDays)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

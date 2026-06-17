// CEREBRO-PATCH(workspace-copy): TECH-3582 copiers for project, chat and autopilot.
package workspacecopy

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// CopyProject copies a project (row + members + nesting) into targetWorkspace,
// non-destructively. Its issues are copied separately via CopyIssue and linked
// through the copy map. An agent lead is remapped to the copied agent if present,
// otherwise cleared; a parent project is remapped if it was copied, else nulled.
func (s *Store) CopyProject(ctx context.Context, runID, targetWorkspace, sourceProject pgtype.UUID) (CopyResult, error) {
	return s.inTx(ctx, func(tx pgx.Tx) (CopyResult, error) {
		return copyProjectTx(ctx, tx, runID, targetWorkspace, sourceProject)
	})
}

// CopyChat copies a chat session and its messages. The session's agent must
// already be copied into the target (copy agents first); otherwise ErrAgentNotCopied.
func (s *Store) CopyChat(ctx context.Context, runID, targetWorkspace, sourceChat pgtype.UUID) (CopyResult, error) {
	return s.inTx(ctx, func(tx pgx.Tx) (CopyResult, error) {
		return copyChatTx(ctx, tx, runID, targetWorkspace, sourceChat)
	})
}

// CopyAutopilot copies an autopilot and its triggers. Triggers are copied
// disabled with their webhook token cleared, so a copy never fires until armed.
func (s *Store) CopyAutopilot(ctx context.Context, runID, targetWorkspace, sourceAutopilot pgtype.UUID) (CopyResult, error) {
	return s.inTx(ctx, func(tx pgx.Tx) (CopyResult, error) {
		return copyAutopilotTx(ctx, tx, runID, targetWorkspace, sourceAutopilot)
	})
}

// RelinkResult reports how many cross-issue links a relink pass healed.
type RelinkResult struct {
	Parents  int64 `json:"parents_relinked"`
	Projects int64 `json:"projects_relinked"`
}

// RelinkIssueRelations heals issue->issue (sub-issue) and issue->project links in
// the target workspace after copies finish. CopyIssue remaps these inline when the
// counterpart is already copied, but per-item copying happens in any order — a
// child issue may be copied before its parent, or an issue before its project.
// This pass sets every copied issue's parent_issue_id / project_id from its source
// once both ends exist in the target. It is idempotent (only fills links that are
// still null/mismatched) and never touches the source workspace.
func (s *Store) RelinkIssueRelations(ctx context.Context, targetWorkspace pgtype.UUID) (RelinkResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return RelinkResult{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	// parent_issue_id: child and parent are both copied issues; set the child's
	// target parent to the copied parent.
	pTag, err := tx.Exec(ctx, `
		UPDATE issue tgt SET parent_issue_id = pm.target_id, updated_at = now()
		FROM cerebro_workspace_copy_map im
		JOIN issue src ON src.id = im.source_id
		JOIN cerebro_workspace_copy_map pm
		  ON pm.target_workspace_id = im.target_workspace_id
		 AND pm.entity_type = 'issue'
		 AND pm.source_id = src.parent_issue_id
		WHERE im.target_workspace_id = $1
		  AND im.entity_type = 'issue'
		  AND tgt.id = im.target_id
		  AND src.parent_issue_id IS NOT NULL
		  AND tgt.parent_issue_id IS DISTINCT FROM pm.target_id`,
		targetWorkspace)
	if err != nil {
		return RelinkResult{}, fmt.Errorf("relink parents: %w", err)
	}

	// project_id: the issue's source project is a copied project; set the issue's
	// target project to the copied project.
	prTag, err := tx.Exec(ctx, `
		UPDATE issue tgt SET project_id = pm.target_id, updated_at = now()
		FROM cerebro_workspace_copy_map im
		JOIN issue src ON src.id = im.source_id
		JOIN cerebro_workspace_copy_map pm
		  ON pm.target_workspace_id = im.target_workspace_id
		 AND pm.entity_type = 'project'
		 AND pm.source_id = src.project_id
		WHERE im.target_workspace_id = $1
		  AND im.entity_type = 'issue'
		  AND tgt.id = im.target_id
		  AND src.project_id IS NOT NULL
		  AND tgt.project_id IS DISTINCT FROM pm.target_id`,
		targetWorkspace)
	if err != nil {
		return RelinkResult{}, fmt.Errorf("relink projects: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return RelinkResult{}, fmt.Errorf("commit: %w", err)
	}
	return RelinkResult{Parents: pTag.RowsAffected(), Projects: prTag.RowsAffected()}, nil
}

func copyProjectTx(ctx context.Context, tx pgx.Tx, runID, targetWorkspace, sourceProject pgtype.UUID) (CopyResult, error) {
	if existing, done, err := lookupMapping(ctx, tx, targetWorkspace, "project", sourceProject); err != nil {
		return CopyResult{}, err
	} else if done {
		return CopyResult{EntityType: "project", SourceID: sourceProject, TargetID: existing, AlreadyDone: true}, nil
	}

	var sourceWorkspace, leadID pgtype.UUID
	var leadType pgtype.Text
	if err := tx.QueryRow(ctx, `SELECT workspace_id, lead_type, lead_id FROM project WHERE id = $1`, sourceProject).
		Scan(&sourceWorkspace, &leadType, &leadID); err != nil {
		return CopyResult{}, fmt.Errorf("load project: %w", err)
	}

	// remap an agent lead to the copied agent; clear if not copied. member leads kept.
	if leadType.Valid && leadType.String == "agent" && leadID.Valid {
		if mapped, ok, err := lookupMapping(ctx, tx, targetWorkspace, "agent", leadID); err != nil {
			return CopyResult{}, err
		} else if ok {
			leadID = mapped
		} else {
			leadType = pgtype.Text{}
			leadID = pgtype.UUID{}
		}
	}

	var targetID pgtype.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO project (id, workspace_id, title, description, icon, status, lead_type, lead_id,
		                     priority, color, repo_url, access, created_at, updated_at)
		SELECT gen_random_uuid(), $1, p.title, p.description, p.icon, p.status, $2, $3,
		       p.priority, p.color, p.repo_url, p.access, p.created_at, now()
		FROM project p WHERE p.id = $4
		RETURNING id`,
		targetWorkspace, leadType, leadID, sourceProject).Scan(&targetID); err != nil {
		return CopyResult{}, fmt.Errorf("copy project: %w", err)
	}

	// project members (same users exist in target; mapped 1:1 by user_id)
	if _, err := tx.Exec(ctx, `
		INSERT INTO project_member (workspace_id, project_id, user_id, added_by, added_at)
		SELECT $1, $2, pm.user_id, pm.added_by, pm.added_at FROM project_member pm WHERE pm.project_id = $3
		ON CONFLICT DO NOTHING`,
		targetWorkspace, targetID, sourceProject); err != nil {
		return CopyResult{}, fmt.Errorf("copy project members: %w", err)
	}

	// nesting: remap parent if it was copied, else top-level
	var parent pgtype.UUID
	err := tx.QueryRow(ctx, `SELECT parent_project_id FROM project_nesting WHERE project_id = $1`, sourceProject).Scan(&parent)
	if err == nil && parent.Valid {
		if mapped, ok, e := lookupMapping(ctx, tx, targetWorkspace, "project", parent); e != nil {
			return CopyResult{}, e
		} else if ok {
			if _, e := tx.Exec(ctx, `INSERT INTO project_nesting (project_id, parent_project_id) VALUES ($1, $2) ON CONFLICT (project_id) DO NOTHING`, targetID, mapped); e != nil {
				return CopyResult{}, fmt.Errorf("copy nesting: %w", e)
			}
		}
	} else if err != nil && err != pgx.ErrNoRows {
		return CopyResult{}, fmt.Errorf("load nesting: %w", err)
	}

	if err := recordMapping(ctx, tx, runID, sourceWorkspace, targetWorkspace, "project", sourceProject, targetID, nil, nil); err != nil {
		return CopyResult{}, err
	}
	return CopyResult{EntityType: "project", SourceID: sourceProject, TargetID: targetID}, nil
}

func copyChatTx(ctx context.Context, tx pgx.Tx, runID, targetWorkspace, sourceChat pgtype.UUID) (CopyResult, error) {
	if existing, done, err := lookupMapping(ctx, tx, targetWorkspace, "chat", sourceChat); err != nil {
		return CopyResult{}, err
	} else if done {
		return CopyResult{EntityType: "chat", SourceID: sourceChat, TargetID: existing, AlreadyDone: true}, nil
	}

	var sourceWorkspace, sourceAgent pgtype.UUID
	if err := tx.QueryRow(ctx, `SELECT workspace_id, agent_id FROM chat_session WHERE id = $1`, sourceChat).
		Scan(&sourceWorkspace, &sourceAgent); err != nil {
		return CopyResult{}, fmt.Errorf("load chat: %w", err)
	}
	// the chat's agent must already exist in the target
	targetAgent, ok, err := lookupMapping(ctx, tx, targetWorkspace, "agent", sourceAgent)
	if err != nil {
		return CopyResult{}, err
	}
	if !ok {
		return CopyResult{}, fmt.Errorf("%w: copy the chat's agent first", ErrAgentNotCopied)
	}

	// session parked (runtime_id null); session_id reset so it gets a fresh client session.
	var targetID pgtype.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO chat_session (id, workspace_id, agent_id, creator_id, title, session_id, work_dir,
		                          status, runtime_id, created_at, updated_at)
		SELECT gen_random_uuid(), $1, $2, c.creator_id, c.title, NULL, c.work_dir,
		       c.status, NULL, c.created_at, now()
		FROM chat_session c WHERE c.id = $3
		RETURNING id`,
		targetWorkspace, targetAgent, sourceChat).Scan(&targetID); err != nil {
		return CopyResult{}, fmt.Errorf("copy chat session: %w", err)
	}

	// messages (fresh ids, new session); task_id cleared (tasks not copied).
	mTag, err := tx.Exec(ctx, `
		INSERT INTO chat_message (id, chat_session_id, role, content, task_id, created_at, responded_at, failure_reason, elapsed_ms)
		SELECT gen_random_uuid(), $1, m.role, m.content, NULL, m.created_at, m.responded_at, m.failure_reason, m.elapsed_ms
		FROM chat_message m WHERE m.chat_session_id = $2`,
		targetID, sourceChat)
	if err != nil {
		return CopyResult{}, fmt.Errorf("copy chat messages: %w", err)
	}

	if err := recordMapping(ctx, tx, runID, sourceWorkspace, targetWorkspace, "chat", sourceChat, targetID, nil, nil); err != nil {
		return CopyResult{}, err
	}
	return CopyResult{EntityType: "chat", SourceID: sourceChat, TargetID: targetID, Comments: mTag.RowsAffected()}, nil
}

func copyAutopilotTx(ctx context.Context, tx pgx.Tx, runID, targetWorkspace, sourceAutopilot pgtype.UUID) (CopyResult, error) {
	if existing, done, err := lookupMapping(ctx, tx, targetWorkspace, "autopilot", sourceAutopilot); err != nil {
		return CopyResult{}, err
	} else if done {
		return CopyResult{EntityType: "autopilot", SourceID: sourceAutopilot, TargetID: existing, AlreadyDone: true}, nil
	}

	var sourceWorkspace, assigneeID, projectID pgtype.UUID
	var assigneeType pgtype.Text
	if err := tx.QueryRow(ctx, `SELECT workspace_id, assignee_type, assignee_id, project_id FROM autopilot WHERE id = $1`, sourceAutopilot).
		Scan(&sourceWorkspace, &assigneeType, &assigneeID, &projectID); err != nil {
		return CopyResult{}, fmt.Errorf("load autopilot: %w", err)
	}
	// assignee_id is required (NOT NULL). An agent assignee must already be copied
	// so we can remap it; squad/unknown assignees can't be carried (out of scope).
	if assigneeType.Valid && assigneeType.String == "agent" && assigneeID.Valid {
		if mapped, ok, err := lookupMapping(ctx, tx, targetWorkspace, "agent", assigneeID); err != nil {
			return CopyResult{}, err
		} else if ok {
			assigneeID = mapped
		} else {
			return CopyResult{}, fmt.Errorf("%w: copy the autopilot's agent first", ErrAssigneeNotCopied)
		}
	} else {
		return CopyResult{}, fmt.Errorf("%w: only agent-assigned autopilots are supported", ErrAssigneeNotCopied)
	}
	// remap project if copied, else clear
	if projectID.Valid {
		if mapped, ok, err := lookupMapping(ctx, tx, targetWorkspace, "project", projectID); err != nil {
			return CopyResult{}, err
		} else if ok {
			projectID = mapped
		} else {
			projectID = pgtype.UUID{}
		}
	}

	var targetID pgtype.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO autopilot (id, workspace_id, title, description, assignee_type, assignee_id, status,
		                       execution_mode, issue_title_template, created_by_type, created_by_id,
		                       scope, owner_user_id, model, is_private, project_id, created_at, updated_at)
		SELECT gen_random_uuid(), $1, a.title, a.description, a.assignee_type, $2, a.status,
		       a.execution_mode, a.issue_title_template, a.created_by_type, a.created_by_id,
		       a.scope, a.owner_user_id, a.model, a.is_private, $3, a.created_at, now()
		FROM autopilot a WHERE a.id = $4
		RETURNING id`,
		targetWorkspace, assigneeID, projectID, sourceAutopilot).Scan(&targetID); err != nil {
		return CopyResult{}, fmt.Errorf("copy autopilot: %w", err)
	}

	// triggers copied DISABLED with no webhook token, so a copy never fires until armed.
	tTag, err := tx.Exec(ctx, `
		INSERT INTO autopilot_trigger (id, autopilot_id, kind, enabled, cron_expression, timezone, label, provider, event_filters, created_at, updated_at)
		SELECT gen_random_uuid(), $1, t.kind, false, t.cron_expression, t.timezone, t.label, t.provider, t.event_filters, t.created_at, now()
		FROM autopilot_trigger t WHERE t.autopilot_id = $2`,
		targetID, sourceAutopilot)
	if err != nil {
		return CopyResult{}, fmt.Errorf("copy triggers: %w", err)
	}

	if err := recordMapping(ctx, tx, runID, sourceWorkspace, targetWorkspace, "autopilot", sourceAutopilot, targetID, nil, nil); err != nil {
		return CopyResult{}, err
	}
	return CopyResult{EntityType: "autopilot", SourceID: sourceAutopilot, TargetID: targetID, Comments: tTag.RowsAffected()}, nil
}

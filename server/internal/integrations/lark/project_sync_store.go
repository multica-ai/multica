package lark

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

const projectBindingColumns = `id, workspace_id, project_id, installation_id, channel_type,
	channel_chat_id, channel_chat_name, state, bind_token_hash, bind_token_expires_at,
	created_by_user_id, bound_by_user_id, unbound_by_user_id, created_at, bound_at,
	unbound_at, updated_at`

const projectBindingColumnsCPB = `cpb.id, cpb.workspace_id, cpb.project_id, cpb.installation_id, cpb.channel_type,
	cpb.channel_chat_id, cpb.channel_chat_name, cpb.state, cpb.bind_token_hash, cpb.bind_token_expires_at,
	cpb.created_by_user_id, cpb.bound_by_user_id, cpb.unbound_by_user_id, cpb.created_at, cpb.bound_at,
	cpb.unbound_at, cpb.updated_at`

const issueTopicBindingColumns = `id, workspace_id, installation_id, project_binding_id, project_id, issue_id,
	channel_chat_id, topic_root_message_id, channel_thread_id, binding_source, state,
	created_by_user_id, unbound_by_user_id, created_at, updated_at, unbound_at`

const notificationOutboxColumns = `id, event_id, workspace_id, project_id, project_binding_id,
	issue_topic_binding_id, issue_id, task_id, event_type, payload, status, attempts,
	next_attempt_at, locked_at, locked_by, last_error, created_at, sent_at`

const notificationOutboxColumnsCNO = `cno.id, cno.event_id, cno.workspace_id, cno.project_id, cno.project_binding_id,
	cno.issue_topic_binding_id, cno.issue_id, cno.task_id, cno.event_type, cno.payload, cno.status,
	cno.attempts, cno.next_attempt_at, cno.locked_at, cno.locked_by, cno.last_error,
	cno.created_at, cno.sent_at`

const projectSyncSummarySelect = `
	SELECT ` + projectBindingColumnsCPB + `,
	       ci.agent_id,
	       a.name,
	       COALESCE(ci.config ->> 'bot_name', a.name || ' Bot'),
	       (SELECT count(*) FROM issue i
	        WHERE i.workspace_id = cpb.workspace_id AND i.project_id = cpb.project_id),
	       (SELECT count(*) FROM channel_issue_topic_binding active
	        WHERE active.workspace_id = cpb.workspace_id
	          AND active.project_binding_id = cpb.id AND active.state = 'active'),
	       (SELECT count(*)
	        FROM issue i
	        WHERE i.workspace_id = cpb.workspace_id AND i.project_id = cpb.project_id
	          AND EXISTS (
	              SELECT 1 FROM channel_issue_topic_binding latest
	              WHERE latest.id = (
	                  SELECT l.id FROM channel_issue_topic_binding l
	                  WHERE l.issue_id = i.id
	                  ORDER BY l.created_at DESC LIMIT 1
	              )
	                AND latest.state = 'manual_unbound'
	          )),
	       (SELECT count(*) FROM channel_notification_outbox pending
	        WHERE pending.workspace_id = cpb.workspace_id
	          AND pending.project_binding_id = cpb.id
	          AND pending.status IN ('pending', 'sending')),
	       (SELECT max(sent_at) FROM channel_notification_outbox sent
	        WHERE sent.project_binding_id = cpb.id AND sent.status = 'sent')
	FROM channel_project_binding cpb
	JOIN channel_installation ci ON ci.id = cpb.installation_id
	  AND ci.workspace_id = cpb.workspace_id
	JOIN agent a ON a.id = ci.agent_id AND a.workspace_id = cpb.workspace_id
`

type projectSyncDB interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type ChannelProjectBinding struct {
	ID                 pgtype.UUID
	WorkspaceID        pgtype.UUID
	ProjectID          pgtype.UUID
	InstallationID     pgtype.UUID
	ChannelType        string
	ChannelChatID      pgtype.Text
	ChannelChatName    pgtype.Text
	State              string
	BindTokenHash      pgtype.Text
	BindTokenExpiresAt pgtype.Timestamptz
	CreatedByUserID    pgtype.UUID
	BoundByUserID      pgtype.UUID
	UnboundByUserID    pgtype.UUID
	CreatedAt          pgtype.Timestamptz
	BoundAt            pgtype.Timestamptz
	UnboundAt          pgtype.Timestamptz
	UpdatedAt          pgtype.Timestamptz
}

type ChannelIssueTopicBinding struct {
	ID                 pgtype.UUID
	WorkspaceID        pgtype.UUID
	InstallationID     pgtype.UUID
	ProjectBindingID   pgtype.UUID
	ProjectID          pgtype.UUID
	IssueID            pgtype.UUID
	ChannelChatID      string
	TopicRootMessageID string
	ChannelThreadID    pgtype.Text
	BindingSource      string
	State              string
	CreatedByUserID    pgtype.UUID
	UnboundByUserID    pgtype.UUID
	CreatedAt          pgtype.Timestamptz
	UpdatedAt          pgtype.Timestamptz
	UnboundAt          pgtype.Timestamptz
}

type ChannelNotificationOutbox struct {
	ID                  pgtype.UUID
	EventID             pgtype.UUID
	WorkspaceID         pgtype.UUID
	ProjectID           pgtype.UUID
	ProjectBindingID    pgtype.UUID
	IssueTopicBindingID pgtype.UUID
	IssueID             pgtype.UUID
	TaskID              pgtype.UUID
	EventType           string
	Payload             []byte
	Status              string
	Attempts            int32
	NextAttemptAt       pgtype.Timestamptz
	LockedAt            pgtype.Timestamptz
	LockedBy            pgtype.Text
	LastError           pgtype.Text
	CreatedAt           pgtype.Timestamptz
	SentAt              pgtype.Timestamptz
}

type ChannelProjectBindingListItem struct {
	Binding      ChannelProjectBinding
	ProjectTitle string
	AgentName    string
	BotName      string
}

type ChannelProjectSyncSummary struct {
	Binding                  ChannelProjectBinding
	AgentID                  pgtype.UUID
	AgentName                string
	BotName                  string
	TotalIssueCount          int64
	BoundIssueCount          int64
	ManualUnboundIssueCount  int64
	PendingNotificationCount int64
	LastSyncedAt             pgtype.Timestamptz
}

func scanProjectBinding(row pgx.Row) (ChannelProjectBinding, error) {
	var b ChannelProjectBinding
	err := row.Scan(
		&b.ID, &b.WorkspaceID, &b.ProjectID, &b.InstallationID, &b.ChannelType,
		&b.ChannelChatID, &b.ChannelChatName, &b.State, &b.BindTokenHash, &b.BindTokenExpiresAt,
		&b.CreatedByUserID, &b.BoundByUserID, &b.UnboundByUserID, &b.CreatedAt, &b.BoundAt,
		&b.UnboundAt, &b.UpdatedAt,
	)
	return b, err
}

func scanIssueTopicBinding(row pgx.Row) (ChannelIssueTopicBinding, error) {
	var b ChannelIssueTopicBinding
	err := row.Scan(
		&b.ID, &b.WorkspaceID, &b.InstallationID, &b.ProjectBindingID, &b.ProjectID, &b.IssueID,
		&b.ChannelChatID, &b.TopicRootMessageID, &b.ChannelThreadID, &b.BindingSource, &b.State,
		&b.CreatedByUserID, &b.UnboundByUserID, &b.CreatedAt, &b.UpdatedAt, &b.UnboundAt,
	)
	return b, err
}

func scanNotificationOutbox(row pgx.Row) (ChannelNotificationOutbox, error) {
	var item ChannelNotificationOutbox
	err := row.Scan(
		&item.ID, &item.EventID, &item.WorkspaceID, &item.ProjectID, &item.ProjectBindingID,
		&item.IssueTopicBindingID, &item.IssueID, &item.TaskID, &item.EventType, &item.Payload, &item.Status,
		&item.Attempts, &item.NextAttemptAt, &item.LockedAt, &item.LockedBy,
		&item.LastError, &item.CreatedAt, &item.SentAt,
	)
	return item, err
}

type projectSyncStore struct {
	pool *pgxpool.Pool
}

func newProjectSyncStore(pool *pgxpool.Pool) *projectSyncStore {
	return &projectSyncStore{pool: pool}
}

func (s *projectSyncStore) begin(ctx context.Context) (pgx.Tx, error) {
	return s.pool.Begin(ctx)
}

func (s *projectSyncStore) createActiveProjectBinding(ctx context.Context, q projectSyncDB, workspaceID, projectID, installationID pgtype.UUID, chatID, chatName string, userID pgtype.UUID) (ChannelProjectBinding, error) {
	return scanProjectBinding(q.QueryRow(ctx, `
		INSERT INTO channel_project_binding (
			workspace_id, project_id, installation_id, channel_type,
			channel_chat_id, channel_chat_name, state,
			created_by_user_id, bound_by_user_id, bound_at
		) VALUES ($1, $2, $3, 'feishu', $4, $5, 'active', $6, $6, now())
		RETURNING `+projectBindingColumns,
		workspaceID, projectID, installationID, chatID, chatName, userID,
	))
}

func (s *projectSyncStore) createPendingProjectBinding(ctx context.Context, q projectSyncDB, workspaceID, projectID, installationID pgtype.UUID, tokenHash string, expiresAt time.Time, userID pgtype.UUID) (ChannelProjectBinding, error) {
	return scanProjectBinding(q.QueryRow(ctx, `
		INSERT INTO channel_project_binding (
			workspace_id, project_id, installation_id, channel_type, state,
			bind_token_hash, bind_token_expires_at, created_by_user_id
		) VALUES ($1, $2, $3, 'feishu', 'pending_group', $4, $5, $6)
		RETURNING `+projectBindingColumns,
		workspaceID, projectID, installationID, tokenHash, expiresAt, userID,
	))
}

func (s *projectSyncStore) getCurrentProjectBinding(ctx context.Context, q projectSyncDB, workspaceID, projectID pgtype.UUID) (ChannelProjectBinding, error) {
	return scanProjectBinding(q.QueryRow(ctx, `
		SELECT `+projectBindingColumns+`
		FROM channel_project_binding
		WHERE workspace_id = $1 AND project_id = $2
		  AND state IN ('pending_group', 'active')
		ORDER BY created_at DESC
		LIMIT 1`, workspaceID, projectID))
}

func (s *projectSyncStore) getProjectBindingByID(ctx context.Context, q projectSyncDB, workspaceID, id pgtype.UUID) (ChannelProjectBinding, error) {
	return scanProjectBinding(q.QueryRow(ctx, `
		SELECT `+projectBindingColumns+`
		FROM channel_project_binding
		WHERE workspace_id = $1 AND id = $2`, workspaceID, id))
}

func (s *projectSyncStore) getActiveProjectBindingByGroup(ctx context.Context, q projectSyncDB, installationID pgtype.UUID, chatID string) (ChannelProjectBinding, error) {
	return scanProjectBinding(q.QueryRow(ctx, `
		SELECT `+projectBindingColumns+`
		FROM channel_project_binding
		WHERE installation_id = $1 AND channel_chat_id = $2 AND state = 'active'`,
		installationID, chatID))
}

func (s *projectSyncStore) getPendingProjectBindingByToken(ctx context.Context, q projectSyncDB, installationID pgtype.UUID, tokenHash string) (ChannelProjectBinding, error) {
	return scanProjectBinding(q.QueryRow(ctx, `
		SELECT `+projectBindingColumns+`
		FROM channel_project_binding
		WHERE installation_id = $1
		  AND bind_token_hash = $2
		  AND state = 'pending_group'
		  AND bind_token_expires_at > now()
		FOR UPDATE`, installationID, tokenHash))
}

func (s *projectSyncStore) confirmProjectBinding(ctx context.Context, q projectSyncDB, b ChannelProjectBinding, chatID, chatName string, userID pgtype.UUID) (ChannelProjectBinding, error) {
	return scanProjectBinding(q.QueryRow(ctx, `
		UPDATE channel_project_binding
		SET channel_chat_id = $1, channel_chat_name = $2, state = 'active',
		    bound_by_user_id = $3, bound_at = now(),
		    bind_token_hash = NULL, bind_token_expires_at = NULL, updated_at = now()
		WHERE id = $4 AND workspace_id = $5 AND installation_id = $6
		  AND state = 'pending_group'
		RETURNING `+projectBindingColumns,
		chatID, chatName, userID, b.ID, b.WorkspaceID, b.InstallationID))
}

func (s *projectSyncStore) listProjectBindingsByInstallation(ctx context.Context, q projectSyncDB, workspaceID, installationID pgtype.UUID) ([]ChannelProjectBindingListItem, error) {
	rows, err := q.Query(ctx, `
		SELECT `+projectBindingColumnsCPB+`,
		       p.title,
		       a.name,
		       COALESCE(ci.config ->> 'bot_name', a.name || ' Bot')
		FROM channel_project_binding cpb
		JOIN project p ON p.id = cpb.project_id AND p.workspace_id = cpb.workspace_id
		JOIN channel_installation ci ON ci.id = cpb.installation_id AND ci.workspace_id = cpb.workspace_id
		JOIN agent a ON a.id = ci.agent_id AND a.workspace_id = cpb.workspace_id
		WHERE cpb.workspace_id = $1 AND cpb.installation_id = $2
		  AND cpb.state IN ('pending_group', 'active')
		ORDER BY cpb.created_at`, workspaceID, installationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChannelProjectBindingListItem
	for rows.Next() {
		var item ChannelProjectBindingListItem
		err := rows.Scan(
			&item.Binding.ID, &item.Binding.WorkspaceID, &item.Binding.ProjectID,
			&item.Binding.InstallationID, &item.Binding.ChannelType,
			&item.Binding.ChannelChatID, &item.Binding.ChannelChatName,
			&item.Binding.State, &item.Binding.BindTokenHash,
			&item.Binding.BindTokenExpiresAt, &item.Binding.CreatedByUserID,
			&item.Binding.BoundByUserID, &item.Binding.UnboundByUserID,
			&item.Binding.CreatedAt, &item.Binding.BoundAt,
			&item.Binding.UnboundAt, &item.Binding.UpdatedAt,
			&item.ProjectTitle, &item.AgentName, &item.BotName,
		)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *projectSyncStore) unbindProject(ctx context.Context, q projectSyncDB, workspaceID, bindingID, userID pgtype.UUID) (ChannelProjectBinding, error) {
	return scanProjectBinding(q.QueryRow(ctx, `
		UPDATE channel_project_binding
		SET state = 'unbound', unbound_by_user_id = $1, unbound_at = now(),
		    bind_token_hash = NULL, bind_token_expires_at = NULL, updated_at = now()
		WHERE id = $2 AND workspace_id = $3
		  AND state IN ('pending_group', 'active')
		RETURNING `+projectBindingColumns, userID, bindingID, workspaceID))
}

func (s *projectSyncStore) finishProjectUnbind(ctx context.Context, q projectSyncDB, b ChannelProjectBinding, reason string) error {
	if _, err := q.Exec(ctx, `
		UPDATE channel_issue_topic_binding
		SET state = 'project_unbound', unbound_at = now(), updated_at = now()
		WHERE workspace_id = $1 AND project_binding_id = $2 AND state = 'active'`,
		b.WorkspaceID, b.ID); err != nil {
		return err
	}
	// Also clean up orphan topic bindings that were created by
	// IssueCreateTopicHookForAgentTask with NULL project_binding_id
	// after the project binding was already unbound. Without this,
	// these orphans block future topic bindings on the same chat.
	if b.ChannelChatID.Valid && b.ChannelChatID.String != "" {
		if _, err := q.Exec(ctx, `
			UPDATE channel_issue_topic_binding
			SET state = 'project_unbound', unbound_at = now(), updated_at = now()
			WHERE workspace_id = $1
			  AND installation_id = $2
			  AND channel_chat_id = $3
			  AND project_binding_id IS NULL
			  AND state = 'active'`,
			b.WorkspaceID, b.InstallationID, b.ChannelChatID); err != nil {
			return err
		}
	}
	_, err := q.Exec(ctx, `
		UPDATE channel_notification_outbox
		SET status = 'dead', last_error = $1, locked_at = NULL, locked_by = NULL
		WHERE workspace_id = $2 AND project_binding_id = $3
		  AND status IN ('pending', 'sending')`, reason, b.WorkspaceID, b.ID)
	return err
}

func (s *projectSyncStore) enqueueProjectBackfill(ctx context.Context, q projectSyncDB, b ChannelProjectBinding) error {
	_, err := q.Exec(ctx, `
		INSERT INTO channel_notification_outbox (
			event_id, workspace_id, project_id, project_binding_id, issue_id,
			event_type, payload
		)
		SELECT gen_random_uuid(), i.workspace_id, i.project_id, $1, i.id,
		       'issue_created',
		       jsonb_build_object(
		           'issue_id', i.id, 'number', i.number, 'title', i.title,
		           'status', i.status, 'assignee_type', i.assignee_type,
		           'assignee_id', i.assignee_id, 'creator_type', i.creator_type,
		           'creator_id', i.creator_id, 'backfill', true,
		           'occurred_at', now()
		       )
		FROM issue i
		WHERE i.workspace_id = $2 AND i.project_id = $3
		  AND NOT EXISTS (
		      SELECT 1 FROM channel_issue_topic_binding active
		      WHERE active.workspace_id = i.workspace_id
		        AND active.issue_id = i.id
		        AND active.state = 'active'
		  )
		  AND NOT EXISTS (
		      SELECT 1
		      FROM channel_issue_topic_binding latest
		      WHERE latest.id = (
		          SELECT l.id FROM channel_issue_topic_binding l
		          WHERE l.issue_id = i.id
		          ORDER BY l.created_at DESC LIMIT 1
		      )
		        AND latest.state = 'manual_unbound'
		  )
		  AND NOT EXISTS (
		      SELECT 1 FROM channel_notification_outbox queued
		      WHERE queued.issue_id = i.id
		        AND queued.project_binding_id = $1
		        AND queued.event_type = 'issue_created'
		        AND queued.status IN ('pending', 'sending', 'sent')
		  )`, b.ID, b.WorkspaceID, b.ProjectID)
	return err
}

func (s *projectSyncStore) createIssueTopic(ctx context.Context, q projectSyncDB, workspaceID, installationID, projectBindingID, projectID, issueID pgtype.UUID, chatID, rootMessageID, threadID, source string, userID pgtype.UUID) (ChannelIssueTopicBinding, error) {
	return scanIssueTopicBinding(q.QueryRow(ctx, `
		INSERT INTO channel_issue_topic_binding (
			workspace_id, installation_id, project_binding_id, project_id, issue_id,
			channel_chat_id, topic_root_message_id, channel_thread_id,
			binding_source, state, created_by_user_id
		) VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, ''), $9, 'active', $10)
		RETURNING `+issueTopicBindingColumns,
		workspaceID, installationID, projectBindingID, projectID, issueID,
		chatID, rootMessageID, threadID, source, userID))
}

func (s *projectSyncStore) getActiveIssueTopicByIssue(ctx context.Context, q projectSyncDB, workspaceID, issueID pgtype.UUID) (ChannelIssueTopicBinding, error) {
	return scanIssueTopicBinding(q.QueryRow(ctx, `
		SELECT `+issueTopicBindingColumns+`
		FROM channel_issue_topic_binding
		WHERE workspace_id = $1 AND issue_id = $2 AND state = 'active'`,
		workspaceID, issueID))
}

func (s *projectSyncStore) getLatestIssueTopicByIssue(ctx context.Context, q projectSyncDB, workspaceID, issueID pgtype.UUID) (ChannelIssueTopicBinding, error) {
	return scanIssueTopicBinding(q.QueryRow(ctx, `
		SELECT `+issueTopicBindingColumns+`
		FROM channel_issue_topic_binding
		WHERE workspace_id = $1 AND issue_id = $2
		ORDER BY created_at DESC
		LIMIT 1`, workspaceID, issueID))
}

func (s *projectSyncStore) getIssueTopicByID(ctx context.Context, q projectSyncDB, workspaceID, id pgtype.UUID) (ChannelIssueTopicBinding, error) {
	return scanIssueTopicBinding(q.QueryRow(ctx, `
		SELECT `+issueTopicBindingColumns+`
		FROM channel_issue_topic_binding
		WHERE workspace_id = $1 AND id = $2`, workspaceID, id))
}

func (s *projectSyncStore) getActiveIssueTopicByRoot(ctx context.Context, q projectSyncDB, workspaceID, installationID pgtype.UUID, chatID, rootMessageID string) (ChannelIssueTopicBinding, error) {
	return scanIssueTopicBinding(q.QueryRow(ctx, `
		SELECT `+issueTopicBindingColumns+`
		FROM channel_issue_topic_binding
		WHERE workspace_id = $1 AND installation_id = $2
		  AND channel_chat_id = $3 AND topic_root_message_id = $4
		  AND state = 'active'`,
		workspaceID, installationID, chatID, rootMessageID))
}

func (s *projectSyncStore) replaceActiveIssueTopic(ctx context.Context, q projectSyncDB, workspaceID, issueID, userID pgtype.UUID) error {
	_, err := q.Exec(ctx, `
		UPDATE channel_issue_topic_binding
		SET state = 'replaced', unbound_by_user_id = $1,
		    unbound_at = now(), updated_at = now()
		WHERE workspace_id = $2 AND issue_id = $3 AND state = 'active'`,
		userID, workspaceID, issueID)
	return err
}

func (s *projectSyncStore) manualUnbindIssueTopic(ctx context.Context, q projectSyncDB, workspaceID, bindingID, userID pgtype.UUID) (ChannelIssueTopicBinding, error) {
	return scanIssueTopicBinding(q.QueryRow(ctx, `
		UPDATE channel_issue_topic_binding
		SET state = 'manual_unbound', unbound_by_user_id = $1,
		    unbound_at = now(), updated_at = now()
		WHERE workspace_id = $2 AND id = $3 AND state = 'active'
		RETURNING `+issueTopicBindingColumns,
		userID, workspaceID, bindingID))
}

func (s *projectSyncStore) claimNotifications(ctx context.Context, workerID string, staleBefore time.Time, batchSize int32) ([]ChannelNotificationOutbox, error) {
	rows, err := s.pool.Query(ctx, `
		WITH candidates AS (
			SELECT cno.id
			FROM channel_notification_outbox cno
			WHERE (
			    (cno.status = 'pending' AND cno.next_attempt_at <= now())
			    OR (cno.status = 'sending' AND cno.locked_at < $1)
			)
			  AND NOT EXISTS (
			      SELECT 1 FROM channel_notification_outbox earlier
			      WHERE earlier.issue_id = cno.issue_id
			        AND (earlier.created_at, earlier.event_order) < (cno.created_at, cno.event_order)
			        AND earlier.status IN ('pending', 'sending')
			  )
			ORDER BY cno.created_at, cno.event_order
			FOR UPDATE SKIP LOCKED
			LIMIT $2
		)
		UPDATE channel_notification_outbox cno
		SET status = 'sending', locked_at = now(), locked_by = $3
		FROM candidates
		WHERE cno.id = candidates.id
		RETURNING `+notificationOutboxColumnsCNO,
		staleBefore, batchSize, workerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChannelNotificationOutbox
	for rows.Next() {
		item, err := scanNotificationOutbox(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *projectSyncStore) markNotificationSent(ctx context.Context, id pgtype.UUID, workerID string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE channel_notification_outbox
		SET status = 'sent', sent_at = now(), locked_at = NULL,
		    locked_by = NULL, last_error = NULL
		WHERE id = $1 AND status = 'sending' AND locked_by = $2`, id, workerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (s *projectSyncStore) retryNotification(ctx context.Context, item ChannelNotificationOutbox, workerID, lastError string, next time.Time) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE channel_notification_outbox
		SET status = 'pending', attempts = attempts + 1,
		    next_attempt_at = $1, last_error = $2,
		    locked_at = NULL, locked_by = NULL
		WHERE id = $3 AND status = 'sending' AND locked_by = $4`,
		next, lastError, item.ID, workerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (s *projectSyncStore) deadNotification(ctx context.Context, item ChannelNotificationOutbox, lastError string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE channel_notification_outbox
		SET status = 'dead', attempts = attempts + 1, last_error = $1,
		    locked_at = NULL, locked_by = NULL
		WHERE id = $2 AND status IN ('pending', 'sending')`, lastError, item.ID)
	return err
}

func (s *projectSyncStore) retryDeadNotifications(ctx context.Context, q projectSyncDB, workspaceID, projectID pgtype.UUID) (int64, error) {
	tag, err := q.Exec(ctx, `
		UPDATE channel_notification_outbox
		SET status = 'pending', attempts = 0, next_attempt_at = now(),
		    last_error = NULL, locked_at = NULL, locked_by = NULL
		WHERE workspace_id = $1 AND project_id = $2 AND status = 'dead'
		  AND COALESCE(last_error, '') NOT IN (
		      'project_unbound', 'project_or_topic_unbound', 'manual_unbound'
		  )`, workspaceID, projectID)
	return tag.RowsAffected(), err
}

func scanProjectSyncSummary(scanner interface{ Scan(...any) error }) (ChannelProjectSyncSummary, error) {
	var summary ChannelProjectSyncSummary
	err := scanner.Scan(
		&summary.Binding.ID, &summary.Binding.WorkspaceID, &summary.Binding.ProjectID,
		&summary.Binding.InstallationID, &summary.Binding.ChannelType,
		&summary.Binding.ChannelChatID, &summary.Binding.ChannelChatName,
		&summary.Binding.State, &summary.Binding.BindTokenHash,
		&summary.Binding.BindTokenExpiresAt, &summary.Binding.CreatedByUserID,
		&summary.Binding.BoundByUserID, &summary.Binding.UnboundByUserID,
		&summary.Binding.CreatedAt, &summary.Binding.BoundAt,
		&summary.Binding.UnboundAt, &summary.Binding.UpdatedAt,
		&summary.AgentID, &summary.AgentName, &summary.BotName,
		&summary.TotalIssueCount, &summary.BoundIssueCount,
		&summary.ManualUnboundIssueCount, &summary.PendingNotificationCount,
		&summary.LastSyncedAt,
	)
	return summary, err
}

func (s *projectSyncStore) getProjectSyncSummary(ctx context.Context, workspaceID, projectID pgtype.UUID) (ChannelProjectSyncSummary, error) {
	return scanProjectSyncSummary(s.pool.QueryRow(ctx, projectSyncSummarySelect+`
		WHERE cpb.workspace_id = $1 AND cpb.project_id = $2
		  AND cpb.state IN ('pending_group', 'active')
		ORDER BY cpb.created_at DESC LIMIT 1`, workspaceID, projectID))
}

func (s *projectSyncStore) listProjectSyncSummaries(ctx context.Context, workspaceID pgtype.UUID) ([]ChannelProjectSyncSummary, error) {
	rows, err := s.pool.Query(ctx, projectSyncSummarySelect+`
		WHERE cpb.workspace_id = $1
		  AND cpb.state IN ('pending_group', 'active')
		ORDER BY cpb.created_at`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	summaries := make([]ChannelProjectSyncSummary, 0)
	for rows.Next() {
		summary, err := scanProjectSyncSummary(rows)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, summary)
	}
	return summaries, rows.Err()
}

func isNoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}

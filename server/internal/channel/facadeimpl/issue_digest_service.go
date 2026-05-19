package facadeimpl

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/channel/facade"
	"github.com/multica-ai/multica/server/internal/util"
)

type IssueDigestService struct {
	pool *pgxpool.Pool
}

func NewIssueDigestService(pool *pgxpool.Pool) *IssueDigestService {
	return &IssueDigestService{pool: pool}
}

func (s *IssueDigestService) GetIssueDigest(ctx context.Context, workspaceID pgtype.UUID, identifier string) (facade.IssueDigest, error) {
	digest, err := s.issueDigestBase(ctx, workspaceID, identifier)
	if err != nil {
		return facade.IssueDigest{}, err
	}
	digest.RecentEvents = s.recentEvents(ctx, digest.Issue.ID)
	digest.AgentSummary = s.agentSummary(ctx, digest.Issue.ID)
	return digest, nil
}

func (s *IssueDigestService) GetIssueProgress(ctx context.Context, workspaceID pgtype.UUID, identifier string) (facade.IssueProgress, error) {
	digest, err := s.GetIssueDigest(ctx, workspaceID, identifier)
	if err != nil {
		return facade.IssueProgress{}, err
	}
	latestStatus := s.latestStatusEvent(ctx, digest.Issue.ID)
	return facade.IssueProgress{
		Digest:          digest,
		LatestReply:     s.latestReply(ctx, digest.Issue.ID),
		LatestStatus:    latestStatus,
		RecommendedNext: nextStepForStatus(digest.Issue.Status, digest.AgentSummary),
	}, nil
}

func (s *IssueDigestService) ListProjectProgress(ctx context.Context, workspaceID pgtype.UUID) ([]facade.ProjectProgress, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT p.id, p.title,
		       count(i.id)::bigint AS total,
		       count(i.id) FILTER (WHERE i.status NOT IN ('done', 'cancelled'))::bigint AS open_count,
		       count(i.id) FILTER (WHERE i.status = 'in_progress')::bigint AS in_progress_count,
		       count(i.id) FILTER (WHERE i.status = 'in_review')::bigint AS in_review_count,
		       count(i.id) FILTER (WHERE i.status = 'blocked')::bigint AS blocked_count,
		       count(i.id) FILTER (WHERE i.status IN ('done', 'cancelled'))::bigint AS done_count
		FROM project p
		LEFT JOIN issue i ON i.project_id = p.id
		WHERE p.workspace_id = $1
		GROUP BY p.id, p.title
		ORDER BY open_count DESC, p.updated_at DESC
		LIMIT 8
	`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []facade.ProjectProgress{}
	for rows.Next() {
		var p facade.ProjectProgress
		if err := rows.Scan(&p.ProjectID, &p.ProjectName, &p.Total, &p.Open, &p.InProgress, &p.InReview, &p.Blocked, &p.Done); err != nil {
			return nil, err
		}
		p.FocusIssues = s.projectFocusIssues(ctx, workspaceID, p.ProjectID)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *IssueDigestService) GetIssueDetail(ctx context.Context, workspaceID pgtype.UUID, identifier string) (facade.IssueDetail, error) {
	digest, err := s.GetIssueDigest(ctx, workspaceID, identifier)
	if err != nil {
		return facade.IssueDetail{}, err
	}
	statusHistory := s.statusHistory(ctx, digest.Issue.ID)
	return facade.IssueDetail{Digest: digest, StatusHistory: statusHistory}, nil
}

func (s *IssueDigestService) GetIssueTimeline(ctx context.Context, workspaceID pgtype.UUID, identifier string, page, pageSize int) (facade.IssueTimelinePage, error) {
	digest, err := s.issueDigestBase(ctx, workspaceID, identifier)
	if err != nil {
		return facade.IssueTimelinePage{}, err
	}
	events, hasMore := s.timelineEvents(ctx, digest.Issue.ID, pageSize, pageOffset(page, pageSize))
	return facade.IssueTimelinePage{
		Issue:    digest.Issue,
		Events:   events,
		Page:     page,
		PageSize: pageSize,
		HasMore:  hasMore,
	}, nil
}

func (s *IssueDigestService) GetIssueLogs(ctx context.Context, workspaceID pgtype.UUID, identifier string, page, pageSize int) (facade.IssueLogPage, error) {
	digest, err := s.issueDigestBase(ctx, workspaceID, identifier)
	if err != nil {
		return facade.IssueLogPage{}, err
	}
	summary := s.agentSummary(ctx, digest.Issue.ID)
	if summary == nil || summary.TaskID == "" {
		return facade.IssueLogPage{Issue: digest.Issue, Page: page, PageSize: pageSize}, nil
	}
	taskID, err := util.ParseUUID(summary.TaskID)
	if err != nil {
		return facade.IssueLogPage{Issue: digest.Issue, Page: page, PageSize: pageSize}, nil
	}
	messages, hasMore := s.taskMessages(ctx, taskID, pageSize, pageOffset(page, pageSize))
	return facade.IssueLogPage{
		Issue:         digest.Issue,
		TaskID:        summary.TaskID,
		AgentName:     summary.AgentName,
		TaskStatus:    summary.Status,
		ResultSummary: summary.ResultSummary,
		FailureReason: summary.FailureReason,
		Messages:      messages,
		Page:          page,
		PageSize:      pageSize,
		HasMore:       hasMore,
	}, nil
}

func (s *IssueDigestService) issueDigestBase(ctx context.Context, workspaceID pgtype.UUID, identifier string) (facade.IssueDigest, error) {
	var issue struct {
		ID            pgtype.UUID
		WorkspaceID   pgtype.UUID
		Title         string
		Description   pgtype.Text
		Status        string
		Priority      string
		AssigneeType  pgtype.Text
		AssigneeID    pgtype.UUID
		CreatorType   string
		CreatorID     pgtype.UUID
		ProjectID     pgtype.UUID
		CreatedAt     time.Time
		UpdatedAt     time.Time
		Number        int32
		Prefix        string
		WorkspaceSlug string
	}
	if err := s.pool.QueryRow(ctx, `
		SELECT i.id, i.workspace_id, i.title, i.description, i.status, i.priority,
		       i.assignee_type, i.assignee_id, i.creator_type, i.creator_id,
		       i.project_id, i.created_at, i.updated_at,
		       i.number, w.issue_prefix, w.slug
		FROM issue i
		JOIN workspace w ON w.id = i.workspace_id
		WHERE i.workspace_id = $1
		  AND (w.issue_prefix || '-' || i.number::text) = $2
	`, workspaceID, identifier).Scan(
		&issue.ID,
		&issue.WorkspaceID,
		&issue.Title,
		&issue.Description,
		&issue.Status,
		&issue.Priority,
		&issue.AssigneeType,
		&issue.AssigneeID,
		&issue.CreatorType,
		&issue.CreatorID,
		&issue.ProjectID,
		&issue.CreatedAt,
		&issue.UpdatedAt,
		&issue.Number,
		&issue.Prefix,
		&issue.WorkspaceSlug,
	); err != nil {
		return facade.IssueDigest{}, err
	}

	digest := facade.IssueDigest{
		Issue: facade.IssueDigestIssue{
			ID:          issue.ID,
			WorkspaceID: issue.WorkspaceID,
			Identifier:  fmt.Sprintf("%s-%d", issue.Prefix, issue.Number),
			Title:       issue.Title,
			Description: textValue(issue.Description),
			Status:      issue.Status,
			Priority:    issue.Priority,
			CreatedAt:   issue.CreatedAt,
			UpdatedAt:   issue.UpdatedAt,
		},
		CreatorType:   issue.CreatorType,
		CreatorName:   s.actorName(ctx, issue.CreatorType, issue.CreatorID),
		WorkspaceSlug: issue.WorkspaceSlug,
	}
	if issue.ProjectID.Valid {
		digest.ProjectName = s.projectName(ctx, issue.ProjectID)
	}
	if issue.AssigneeType.Valid && issue.AssigneeID.Valid {
		digest.AssigneeType = issue.AssigneeType.String
		digest.AssigneeName = s.actorName(ctx, issue.AssigneeType.String, issue.AssigneeID)
	}
	digest.Labels = s.labels(ctx, issue.ID)
	return digest, nil
}

func pageOffset(page, pageSize int) int {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		return 0
	}
	return (page - 1) * pageSize
}

func (s *IssueDigestService) projectName(ctx context.Context, projectID pgtype.UUID) string {
	var name string
	if err := s.pool.QueryRow(ctx, `SELECT title FROM project WHERE id = $1`, projectID).Scan(&name); err != nil {
		return ""
	}
	return name
}

func (s *IssueDigestService) labels(ctx context.Context, issueID pgtype.UUID) []string {
	rows, err := s.pool.Query(ctx, `
		SELECT l.name
		FROM issue_label l
		JOIN issue_to_label il ON il.label_id = l.id
		WHERE il.issue_id = $1
		ORDER BY l.name ASC
	`, issueID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return out
		}
		out = append(out, name)
	}
	return out
}

func (s *IssueDigestService) actorName(ctx context.Context, actorType string, actorID pgtype.UUID) string {
	if !actorID.Valid {
		return ""
	}
	var name string
	switch actorType {
	case "member":
		_ = s.pool.QueryRow(ctx, `SELECT name FROM "user" WHERE id = $1`, actorID).Scan(&name)
	case "agent":
		_ = s.pool.QueryRow(ctx, `SELECT name FROM agent WHERE id = $1`, actorID).Scan(&name)
	case "system":
		name = "system"
	}
	return name
}

func (s *IssueDigestService) recentEvents(ctx context.Context, issueID pgtype.UUID) []facade.IssueDigestEvent {
	events, _ := s.timelineEvents(ctx, issueID, 3, 0)
	return events
}

func (s *IssueDigestService) latestReply(ctx context.Context, issueID pgtype.UUID) *facade.IssueProgressReply {
	var reply struct {
		AuthorType string
		AuthorID   pgtype.UUID
		Content    string
		CreatedAt  time.Time
	}
	if err := s.pool.QueryRow(ctx, `
		SELECT author_type, author_id, content, created_at
		FROM comment
		WHERE issue_id = $1
		  AND COALESCE(NULLIF(trim(content), ''), '') <> ''
		ORDER BY created_at DESC
		LIMIT 1
	`, issueID).Scan(&reply.AuthorType, &reply.AuthorID, &reply.Content, &reply.CreatedAt); err != nil {
		return nil
	}
	return &facade.IssueProgressReply{
		AuthorType: reply.AuthorType,
		AuthorName: s.actorName(ctx, reply.AuthorType, reply.AuthorID),
		Content:    strings.TrimSpace(reply.Content),
		CreatedAt:  reply.CreatedAt,
	}
}

func (s *IssueDigestService) latestStatusEvent(ctx context.Context, issueID pgtype.UUID) *facade.IssueDigestEvent {
	events := s.statusHistory(ctx, issueID)
	if len(events) == 0 {
		return nil
	}
	return &events[0]
}

func (s *IssueDigestService) projectFocusIssues(ctx context.Context, workspaceID, projectID pgtype.UUID) []facade.ProjectProgressIssue {
	rows, err := s.pool.Query(ctx, `
		SELECT w.issue_prefix || '-' || i.number::text AS identifier,
		       i.title, i.status, i.assignee_type, i.assignee_id, i.updated_at
		FROM issue i
		JOIN workspace w ON w.id = i.workspace_id
		WHERE i.workspace_id = $1
		  AND i.project_id = $2
		  AND i.status NOT IN ('done', 'cancelled')
		ORDER BY
		  CASE i.status
		    WHEN 'blocked' THEN 0
		    WHEN 'in_review' THEN 1
		    WHEN 'in_progress' THEN 2
		    ELSE 3
		  END,
		  i.updated_at DESC
		LIMIT 3
	`, workspaceID, projectID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	out := []facade.ProjectProgressIssue{}
	for rows.Next() {
		var item facade.ProjectProgressIssue
		var assigneeType pgtype.Text
		var assigneeID pgtype.UUID
		if err := rows.Scan(&item.Identifier, &item.Title, &item.Status, &assigneeType, &assigneeID, &item.UpdatedAt); err != nil {
			return out
		}
		if assigneeType.Valid && assigneeID.Valid {
			item.Assignee = s.actorName(ctx, assigneeType.String, assigneeID)
		}
		out = append(out, item)
	}
	return out
}

func (s *IssueDigestService) statusHistory(ctx context.Context, issueID pgtype.UUID) []facade.IssueDigestEvent {
	rows, err := s.pool.Query(ctx, `
		SELECT 'activity'::text AS kind, actor_type, actor_id,
		       CASE action
		         WHEN 'status_changed' THEN '状态变更：' || COALESCE(details->>'prev_status', details->>'from', '?') || ' -> ' || COALESCE(details->>'status', details->>'to', details->'issue'->>'status', '?')
		         WHEN 'priority_changed' THEN '优先级变更：' || COALESCE(details->>'prev_priority', details->>'from', '?') || ' -> ' || COALESCE(details->>'priority', details->>'to', details->'issue'->>'priority', '?')
		         WHEN 'assignee_changed' THEN '指派变更：' || COALESCE(details->>'prev_assignee_name', details->>'prev_assignee_id', details->>'from_name', details->>'from_id', '未指派') || ' -> ' || COALESCE(details->>'assignee_name', details->>'assignee_id', details->>'to_name', details->>'to_id', '未指派')
		         ELSE action || CASE WHEN details = '{}'::jsonb THEN '' ELSE ' ' || details::text END
		       END AS summary,
		       created_at
		FROM activity_log
		WHERE issue_id = $1
		  AND action IN ('status_changed', 'priority_changed', 'assignee_changed')
		ORDER BY created_at DESC
		LIMIT 5
	`, issueID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	return s.scanEvents(ctx, rows, 5)
}

func (s *IssueDigestService) timelineEvents(ctx context.Context, issueID pgtype.UUID, limit, offset int) ([]facade.IssueDigestEvent, bool) {
	if limit < 1 {
		limit = 5
	}
	rows, err := s.pool.Query(ctx, `
		WITH recent AS (
			SELECT kind, actor_type, actor_id, summary, created_at
			FROM (
				SELECT 'comment'::text AS kind, author_type AS actor_type, author_id AS actor_id,
				       content AS summary, created_at
				FROM comment
				WHERE issue_id = $1
				UNION ALL
				SELECT 'activity'::text AS kind, actor_type, actor_id,
				       CASE action
				         WHEN 'status_changed' THEN '状态变更：' || COALESCE(details->>'prev_status', details->>'from', '?') || ' -> ' || COALESCE(details->>'status', details->>'to', details->'issue'->>'status', '?')
				         WHEN 'priority_changed' THEN '优先级变更：' || COALESCE(details->>'prev_priority', details->>'from', '?') || ' -> ' || COALESCE(details->>'priority', details->>'to', details->'issue'->>'priority', '?')
				         WHEN 'assignee_changed' THEN '指派变更：' || COALESCE(details->>'prev_assignee_name', details->>'prev_assignee_id', details->>'from_name', details->>'from_id', '未指派') || ' -> ' || COALESCE(details->>'assignee_name', details->>'assignee_id', details->>'to_name', details->>'to_id', '未指派')
				         ELSE action || CASE WHEN details = '{}'::jsonb THEN '' ELSE ' ' || details::text END
				       END AS summary,
				       created_at
				FROM activity_log
				WHERE issue_id = $1
			) source
			ORDER BY created_at DESC
			OFFSET $3
			LIMIT $2 + 1
		)
		SELECT kind, actor_type, actor_id, summary, created_at
		FROM recent
	`, issueID, limit, offset)
	if err != nil {
		return nil, false
	}
	defer rows.Close()

	events := s.scanEvents(ctx, rows, limit+1)
	hasMore := len(events) > limit
	if hasMore {
		events = events[:limit]
	}
	return events, hasMore
}

func (s *IssueDigestService) scanEvents(ctx context.Context, rows pgx.Rows, capHint int) []facade.IssueDigestEvent {
	out := make([]facade.IssueDigestEvent, 0, capHint)
	for rows.Next() {
		var kind, summary string
		var actorType pgtype.Text
		var actorID pgtype.UUID
		var createdAt time.Time
		if err := rows.Scan(&kind, &actorType, &actorID, &summary, &createdAt); err != nil {
			return out
		}
		actorTypeText := ""
		if actorType.Valid {
			actorTypeText = actorType.String
		}
		out = append(out, facade.IssueDigestEvent{
			Kind:      kind,
			ActorName: s.actorName(ctx, actorTypeText, actorID),
			Summary:   truncateText(summary, 220),
			CreatedAt: createdAt,
		})
	}
	return out
}

func (s *IssueDigestService) taskMessages(ctx context.Context, taskID pgtype.UUID, limit, offset int) ([]facade.IssueTaskLogEvent, bool) {
	if limit < 1 {
		limit = 8
	}
	rows, err := s.pool.Query(ctx, `
		SELECT seq, type, COALESCE(tool, '') AS tool,
		       COALESCE(NULLIF(content, ''), NULLIF(output, ''), input::text, '') AS body,
		       created_at
		FROM task_message
		WHERE task_id = $1
		ORDER BY seq DESC
		OFFSET $3
		LIMIT $2 + 1
	`, taskID, limit, offset)
	if err != nil {
		return nil, false
	}
	defer rows.Close()

	out := make([]facade.IssueTaskLogEvent, 0, limit+1)
	for rows.Next() {
		var msg facade.IssueTaskLogEvent
		if err := rows.Scan(&msg.Seq, &msg.Type, &msg.Tool, &msg.Content, &msg.CreatedAt); err != nil {
			return out, false
		}
		msg.Content = truncateText(msg.Content, 260)
		out = append(out, msg)
	}
	hasMore := len(out) > limit
	if hasMore {
		out = out[:limit]
	}
	return out, hasMore
}

func (s *IssueDigestService) agentSummary(ctx context.Context, issueID pgtype.UUID) *facade.IssueAgentSummary {
	var task struct {
		ID             pgtype.UUID
		AgentID        pgtype.UUID
		Status         string
		TriggerSummary pgtype.Text
		Result         pgtype.Text
		Error          pgtype.Text
		FailureReason  pgtype.Text
		TouchedAt      time.Time
	}
	if err := s.pool.QueryRow(ctx, `
		SELECT id, agent_id, status, trigger_summary, COALESCE(result::text, '') AS result_text,
		       COALESCE(error, '') AS error_text, failure_reason,
		       COALESCE(completed_at, started_at, dispatched_at, created_at) AS touched_at
		FROM agent_task_queue
		WHERE issue_id = $1
		ORDER BY
			CASE WHEN status IN ('queued', 'dispatched', 'running') THEN 0 ELSE 1 END,
			created_at DESC
		LIMIT 1
	`, issueID).Scan(
		&task.ID,
		&task.AgentID,
		&task.Status,
		&task.TriggerSummary,
		&task.Result,
		&task.Error,
		&task.FailureReason,
		&task.TouchedAt,
	); err != nil {
		if err == pgx.ErrNoRows {
			return nil
		}
		return nil
	}

	progress := s.latestTaskProgress(ctx, task.ID)
	summary := &facade.IssueAgentSummary{
		TaskID:        util.UUIDToString(task.ID),
		AgentName:     s.actorName(ctx, "agent", task.AgentID),
		Status:        task.Status,
		Progress:      progress,
		ResultSummary: firstValidText(task.Result, task.Error),
		FailureReason: textValue(task.FailureReason),
		UpdatedAt:     task.TouchedAt,
	}
	summary.Progress = truncateText(summary.Progress, 180)
	summary.ResultSummary = truncateText(summary.ResultSummary, 220)
	summary.FailureReason = truncateText(summary.FailureReason, 180)
	return summary
}

func (s *IssueDigestService) latestTaskProgress(ctx context.Context, taskID pgtype.UUID) string {
	var content, output pgtype.Text
	if err := s.pool.QueryRow(ctx, `
		SELECT content, output
		FROM task_message
		WHERE task_id = $1 AND (COALESCE(content, '') <> '' OR COALESCE(output, '') <> '')
		ORDER BY seq DESC
		LIMIT 1
	`, taskID).Scan(&content, &output); err != nil {
		return ""
	}
	return firstValidText(content, output)
}

func firstValidText(values ...pgtype.Text) string {
	for _, value := range values {
		if value.Valid && strings.TrimSpace(value.String) != "" {
			return strings.TrimSpace(value.String)
		}
	}
	return ""
}

func textValue(value pgtype.Text) string {
	if !value.Valid {
		return ""
	}
	return strings.TrimSpace(value.String)
}

func truncateText(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || len([]rune(s)) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max]) + "..."
}

func nextStepForStatus(status string, summary *facade.IssueAgentSummary) string {
	switch status {
	case "in_review":
		return "看最新回复并决定通过、补充修改意见，或把状态改成 done。"
	case "blocked":
		return "先补充阻塞原因或解除依赖，再继续推进。"
	case "todo", "backlog":
		return "确认负责人是否明确；需要 agent 处理时直接评论或指派。"
	case "in_progress":
		if summary != nil && (summary.Status == "queued" || summary.Status == "running" || summary.Status == "dispatched") {
			return "agent 正在处理，关注最新回复即可。"
		}
		return "如果长时间没有新回复，可以追问负责人或补充上下文。"
	case "done":
		return "已完成；如果结果不符合预期，补充评论后重新打开或重跑 agent。"
	default:
		return "查看最新回复后决定是否补充评论、指派负责人或推进状态。"
	}
}

var _ facade.IssueDigestService = (*IssueDigestService)(nil)

package wakeup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	TriggerTime        = "time"
	TriggerIssueStatus = "issue_status"
	TriggerGithubCI    = "github_ci"

	StatePending    = "pending"
	StateDispatched = "dispatched"

	// commentTypeWakeup tags the dispatch comment so the frontend renders it as
	// a small collapsible action note instead of a full comment card (TECH-3298).
	commentTypeWakeup = "wakeup"

	// Self-wakeup limit defaults, applied when a workspace has no settings row.
	// Mirrors cerebro_workspace_settings column defaults (migration 9071) and the
	// values agreed on TECH-3298.
	defaultMaxSelfWakeupsPerIssue = 8
	defaultMinWakeupIntervalMin   = 5

	// postponeDelay is how long to wait before retrying a wakeup that couldn't
	// fire because the issue had an active task or the agent runtime was offline.
	postponeDelay = 5 * time.Minute

	// WakeupMinIntervalMinutes is the minimum allowed gap between wakeup
	// creations for the same agent+issue (enforced at creation time).
	// Also used as the delay before re-scheduling after a postpone notification.
	WakeupMinIntervalMinutes = 15

	// WakeupMaxConsecutivePostpones is the number of consecutive wakeup
	// dispatches allowed before an inbox notification is sent to the issue owner.
	WakeupMaxConsecutivePostpones = 3
)

var ErrNotFound = errors.New("wakeup not found")

type Service struct {
	Cerebro *cerebrodb.Queries
	Queries *db.Queries
	Tasks   *service.TaskService
	Bus     *events.Bus
}

type CreateRequest struct {
	AgentID      pgtype.UUID
	IssueID      pgtype.UUID
	Prompt       string
	TriggerType  string
	FireAt       pgtype.Timestamptz
	WatchIssueID pgtype.UUID
	WatchStatus  pgtype.Text
	CreatedByID  pgtype.UUID
	// OriginCommentID is the thread anchor (root comment) of the conversation
	// the scheduling agent was in when it created this wakeup. When set, the
	// dispatched wakeup note is posted as a reply under this thread so the
	// agent's eventual reply lands back in the original conversation instead of
	// a new orphaned root note (TECH-3487). Zero value = no thread context.
	OriginCommentID pgtype.UUID
}

func New(cerebro *cerebrodb.Queries, queries *db.Queries, tasks *service.TaskService, bus *events.Bus) *Service {
	return &Service{Cerebro: cerebro, Queries: queries, Tasks: tasks, Bus: bus}
}

func (s *Service) Create(ctx context.Context, workspaceID pgtype.UUID, req CreateRequest) (cerebrodb.CerebroAgentWakeup, error) {
	req.Prompt = strings.TrimSpace(req.Prompt)
	req.TriggerType = strings.TrimSpace(req.TriggerType)
	if req.Prompt == "" {
		return cerebrodb.CerebroAgentWakeup{}, fmt.Errorf("prompt is required")
	}
	if !req.AgentID.Valid {
		return cerebrodb.CerebroAgentWakeup{}, fmt.Errorf("agent_id is required")
	}
	if !req.IssueID.Valid {
		return cerebrodb.CerebroAgentWakeup{}, fmt.Errorf("issue_id is required")
	}
	if err := s.validateIssueAndAgent(ctx, workspaceID, req.IssueID, req.AgentID); err != nil {
		return cerebrodb.CerebroAgentWakeup{}, err
	}
	switch req.TriggerType {
	case TriggerTime:
		if !req.FireAt.Valid {
			return cerebrodb.CerebroAgentWakeup{}, fmt.Errorf("fire_at is required for time wakeups")
		}
		// Enforce minimum interval: reject if fire_at is too soon.
		minAllowed := time.Now().Add(time.Duration(WakeupMinIntervalMinutes) * time.Minute)
		if req.FireAt.Time.Before(minAllowed) {
			return cerebrodb.CerebroAgentWakeup{}, fmt.Errorf(
				"fire_at must be at least %d minutes from now (got %s)",
				WakeupMinIntervalMinutes, req.FireAt.Time.Format(time.RFC3339),
			)
		}
		// Enforce min interval: reject if there is already a pending wakeup
		// for this agent+issue created within the last WakeupMinIntervalMinutes.
		if recent, err := s.Cerebro.HasRecentPendingWakeupForAgentIssue(ctx, req.AgentID, req.IssueID, WakeupMinIntervalMinutes); err == nil && recent {
			return cerebrodb.CerebroAgentWakeup{}, fmt.Errorf(
				"a wakeup for this agent+issue was already created within the last %d minutes; wait before creating another",
				WakeupMinIntervalMinutes,
			)
		}
	case TriggerIssueStatus:
		if !req.WatchIssueID.Valid || !req.WatchStatus.Valid || strings.TrimSpace(req.WatchStatus.String) == "" {
			return cerebrodb.CerebroAgentWakeup{}, fmt.Errorf("watch_issue_id and watch_status are required for issue_status wakeups")
		}
		if err := s.validateIssue(ctx, workspaceID, req.WatchIssueID); err != nil {
			return cerebrodb.CerebroAgentWakeup{}, fmt.Errorf("watch issue: %w", err)
		}
		req.WatchStatus.String = strings.TrimSpace(req.WatchStatus.String)
	case TriggerGithubCI:
		if !req.WatchIssueID.Valid {
			return cerebrodb.CerebroAgentWakeup{}, fmt.Errorf("watch_issue_id is required for github_ci wakeups")
		}
		if err := s.validateIssue(ctx, workspaceID, req.WatchIssueID); err != nil {
			return cerebrodb.CerebroAgentWakeup{}, fmt.Errorf("watch issue: %w", err)
		}
	default:
		return cerebrodb.CerebroAgentWakeup{}, fmt.Errorf("unsupported trigger_type %q", req.TriggerType)
	}

	// TECH-3176: refuse to create a wakeup whose trigger type is disabled for
	// this workspace, so the agent gets immediate, clear feedback instead of a
	// pending row that never fires.
	if !s.triggerTypeEnabled(ctx, workspaceID, req.TriggerType) {
		return cerebrodb.CerebroAgentWakeup{}, fmt.Errorf("wakeup trigger_type %q is disabled for this workspace", req.TriggerType)
	}

	// TECH-3298: enforce the per-workspace self-wakeup limits so a single agent
	// can't flood one issue with rapid self-wakeups.
	if err := s.enforceSelfWakeupLimits(ctx, workspaceID, req); err != nil {
		return cerebrodb.CerebroAgentWakeup{}, err
	}

	return s.Cerebro.CreateCerebroAgentWakeup(ctx, cerebrodb.CreateCerebroAgentWakeupParams{
		WorkspaceID:     workspaceID,
		AgentID:         req.AgentID,
		IssueID:         req.IssueID,
		Prompt:          req.Prompt,
		TriggerType:     req.TriggerType,
		FireAt:          req.FireAt,
		WatchIssueID:    req.WatchIssueID,
		WatchStatus:     req.WatchStatus,
		CreatedByID:     req.CreatedByID,
		OriginCommentID: req.OriginCommentID,
	})
}

func (s *Service) List(ctx context.Context, workspaceID pgtype.UUID, agentID pgtype.UUID, issueID pgtype.UUID, state pgtype.Text, limit int32) ([]cerebrodb.CerebroAgentWakeup, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return s.Cerebro.ListCerebroAgentWakeups(ctx, cerebrodb.ListCerebroAgentWakeupsParams{
		WorkspaceID: workspaceID,
		AgentID:     agentID,
		IssueID:     issueID,
		State:       state,
		Limit:       limit,
	})
}

func (s *Service) Get(ctx context.Context, workspaceID, id pgtype.UUID) (cerebrodb.CerebroAgentWakeup, error) {
	row, err := s.Cerebro.GetCerebroAgentWakeup(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return cerebrodb.CerebroAgentWakeup{}, ErrNotFound
		}
		return cerebrodb.CerebroAgentWakeup{}, err
	}
	if util.UUIDToString(row.WorkspaceID) != util.UUIDToString(workspaceID) {
		return cerebrodb.CerebroAgentWakeup{}, ErrNotFound
	}
	return row, nil
}

func (s *Service) Cancel(ctx context.Context, workspaceID, id pgtype.UUID) (cerebrodb.CerebroAgentWakeup, error) {
	row, err := s.Cerebro.CancelCerebroAgentWakeup(ctx, cerebrodb.CancelCerebroAgentWakeupParams{ID: id, WorkspaceID: workspaceID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return cerebrodb.CerebroAgentWakeup{}, ErrNotFound
		}
		return cerebrodb.CerebroAgentWakeup{}, err
	}
	return row, nil
}

func (s *Service) CancelByIssueID(ctx context.Context, issueID pgtype.UUID) error {
	return s.Cerebro.CancelPendingWakeupsByIssueID(ctx, issueID)
}

func (s *Service) ClaimDueTime(ctx context.Context, limit int32) ([]cerebrodb.CerebroAgentWakeup, error) {
	return s.Cerebro.ClaimDueTimeWakeups(ctx, limit)
}

func (s *Service) ClaimIssueStatus(ctx context.Context, issueID pgtype.UUID, status string, limit int32) ([]cerebrodb.CerebroAgentWakeup, error) {
	return s.Cerebro.ClaimPendingIssueStatusWakeups(ctx, cerebrodb.ClaimPendingIssueStatusWakeupsParams{
		WatchIssueID: issueID,
		WatchStatus:  pgtype.Text{String: status, Valid: true},
		RowLimit:     limit,
	})
}

func (s *Service) ClaimGithubCI(ctx context.Context, issueIDs []pgtype.UUID, limit int32) ([]cerebrodb.CerebroAgentWakeup, error) {
	if len(issueIDs) == 0 {
		return nil, nil
	}
	return s.Cerebro.ClaimPendingGithubCIWakeups(ctx, cerebrodb.ClaimPendingGithubCIWakeupsParams{
		IssueIds: issueIDs,
		RowLimit: limit,
	})
}

func (s *Service) Dispatch(ctx context.Context, row cerebrodb.CerebroAgentWakeup) {
	// TECH-3176: if the trigger type was disabled for this workspace after the
	// wakeup was claimed, do not fire. Release it back to pending so it resumes
	// the moment the type is re-enabled.
	if !s.triggerTypeEnabled(ctx, row.WorkspaceID, row.TriggerType) {
		if err := s.Cerebro.ReleaseWakeupToPending(context.Background(), row.ID); err != nil {
			slog.Error("cerebro wakeup release-on-disabled failed", "wakeup_id", util.UUIDToString(row.ID), "error", err)
		}
		return
	}
	if err := s.dispatch(ctx, row); err != nil {
		slog.Error("cerebro wakeup dispatch failed", "wakeup_id", util.UUIDToString(row.ID), "error", err)
		_ = s.Cerebro.MarkWakeupFailed(context.Background(), cerebrodb.MarkWakeupFailedParams{
			ID:      row.ID,
			Failure: pgtype.Text{String: truncateFailure(err.Error()), Valid: true},
		})
	}
}

func (s *Service) dispatch(ctx context.Context, row cerebrodb.CerebroAgentWakeup) error {
	issue, err := s.Queries.GetIssue(ctx, row.IssueID)
	if err != nil {
		return fmt.Errorf("load issue: %w", err)
	}
	if util.UUIDToString(issue.WorkspaceID) != util.UUIDToString(row.WorkspaceID) {
		return fmt.Errorf("issue workspace mismatch")
	}

	// Phase 1: clean up hanging running/dispatched tasks for this agent+issue
	// so the new wakeup task can actually be claimed by the daemon.
	if cancelled, cancelErr := s.Queries.CancelAgentTasksByIssueAndAgent(ctx, db.CancelAgentTasksByIssueAndAgentParams{
		IssueID: issue.ID,
		AgentID: row.AgentID,
	}); cancelErr != nil {
		slog.Warn("cerebro wakeup: could not cancel hanging tasks",
			"wakeup_id", util.UUIDToString(row.ID),
			"agent_id", util.UUIDToString(row.AgentID),
			"issue_id", util.UUIDToString(issue.ID),
			"error", cancelErr,
		)
	} else if len(cancelled) > 0 {
		slog.Info("cerebro wakeup: cancelled hanging tasks before dispatch",
			"wakeup_id", util.UUIDToString(row.ID),
			"cancelled", len(cancelled),
		)
	}

	// Postpone if the agent has no runtime or the runtime is currently offline.
	agent, agentErr := s.Queries.GetAgent(ctx, row.AgentID)
	if agentErr != nil {
		return fmt.Errorf("load agent: %w", agentErr)
	}
	if !agent.RuntimeID.Valid {
		slog.Info("cerebro wakeup postponed: agent has no runtime",
			"wakeup_id", util.UUIDToString(row.ID),
			"agent_id", util.UUIDToString(row.AgentID),
		)
		return s.postpone(ctx, row)
	}
	rt, rtErr := s.Queries.GetAgentRuntime(ctx, agent.RuntimeID)
	if rtErr != nil || rt.Status != "online" {
		slog.Info("cerebro wakeup postponed: agent runtime offline",
			"wakeup_id", util.UUIDToString(row.ID),
			"agent_id", util.UUIDToString(row.AgentID),
			"runtime_id", util.UUIDToString(agent.RuntimeID),
		)
		return s.postpone(ctx, row)
	}

	// TECH-3487: anchor the wakeup note inside the conversation that scheduled
	// it. Posting the note as a reply under the original thread root makes the
	// agent's forced reply (the comment guard requires parent == trigger comment)
	// resolve into that same thread, so the answer lands where the request was
	// made instead of in a new orphaned root note. parentID stays zero (root
	// note, pre-TECH-3487 behavior) when there is no still-valid origin thread.
	parentID := s.resolveWakeupParent(ctx, row, issue.ID)

	comment, err := s.Queries.CreateComment(ctx, db.CreateCommentParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		AuthorType:  "system",
		AuthorID:    util.MustParseUUID("00000000-0000-0000-0000-000000000000"),
		Content:     buildWakeupCommentContent(row.TriggerType, row.Prompt),
		Type:        commentTypeWakeup,
		ParentID:    parentID,
	})
	if err != nil {
		return fmt.Errorf("create wakeup comment: %w", err)
	}
	if _, err := s.Tasks.EnqueueTaskForMention(ctx, issue, row.AgentID, comment.ID); err != nil {
		return fmt.Errorf("enqueue agent task: %w", err)
	}
	if err := s.Cerebro.MarkWakeupDispatched(ctx, row.ID); err != nil {
		return fmt.Errorf("mark dispatched: %w", err)
	}

	// Phase 1: increment postpone counter and notify the issue owner if the
	// loop threshold is reached.
	go s.checkPostponeLimit(context.Background(), row, issue)

	if s.Bus != nil {
		s.Bus.Publish(events.Event{
			Type:        protocol.EventCommentCreated,
			WorkspaceID: util.UUIDToString(row.WorkspaceID),
			ActorType:   "system",
			Payload: map[string]any{
				"comment": map[string]any{
					"id":          util.UUIDToString(comment.ID),
					"issue_id":    util.UUIDToString(comment.IssueID),
					"content":     comment.Content,
					"type":        comment.Type,
					"author_type": "system",
					"created_at":  comment.CreatedAt,
				},
			},
		})
	}
	return nil
}

// resolveWakeupParent returns the comment the wakeup note should be posted under
// so it threads into the conversation that scheduled the wakeup (TECH-3487).
// It returns the zero UUID — meaning "post as a root note" — when the wakeup has
// no recorded origin, or the origin comment has since been deleted or moved to a
// different issue. The returned anchor is normalized to the thread root, matching
// how the daemon derives the triggering thread (handler/daemon.go), so the note
// sits one level under the root and the agent's reply lands in the same thread.
func (s *Service) resolveWakeupParent(ctx context.Context, row cerebrodb.CerebroAgentWakeup, issueID pgtype.UUID) pgtype.UUID {
	if !row.OriginCommentID.Valid {
		return pgtype.UUID{}
	}
	origin, err := s.Queries.GetComment(ctx, row.OriginCommentID)
	if err != nil {
		return pgtype.UUID{} // origin deleted — fall back to a root note
	}
	if util.UUIDToString(origin.IssueID) != util.UUIDToString(issueID) {
		return pgtype.UUID{} // origin belongs to another issue — do not cross threads
	}
	if origin.ParentID.Valid {
		return origin.ParentID // normalize to the thread root
	}
	return origin.ID
}

// buildWakeupCommentContent encodes the trigger sub-type as a leading inline
// tag the frontend strips for display, followed by the agent's own note. The
// tag lets the wakeup note render its type ("Time-based wakeup") and keeps the
// note itself as the expandable body. The agent receives this same string as
// its task trigger summary, where the short tag is harmless.
func buildWakeupCommentContent(triggerType, prompt string) string {
	return "[wakeup:" + triggerType + "] " + prompt
}

// selfWakeupLimits resolves the per-workspace self-wakeup limits, falling back
// to the defaults when no settings row exists or the lookup fails (never block
// a wakeup on a transient settings read).
func (s *Service) selfWakeupLimits(ctx context.Context, workspaceID pgtype.UUID) (maxPerIssue int, minInterval time.Duration) {
	maxPerIssue = defaultMaxSelfWakeupsPerIssue
	minInterval = time.Duration(defaultMinWakeupIntervalMin) * time.Minute
	if s.Cerebro == nil {
		return
	}
	settings, err := s.Cerebro.GetCerebroWorkspaceSettings(ctx, workspaceID)
	if err != nil {
		return
	}
	if settings.WakeupMaxSelfPerIssue > 0 {
		maxPerIssue = int(settings.WakeupMaxSelfPerIssue)
	}
	if settings.WakeupMinIntervalMinutes > 0 {
		minInterval = time.Duration(settings.WakeupMinIntervalMinutes) * time.Minute
	}
	return
}

// enforceSelfWakeupLimits caps how many wakeups an agent may stack on one issue
// and the minimum gap between two time-based wakeups. Errors here surface to the
// agent as a 400 from the create handler, so the messages are plain and actionable.
func (s *Service) enforceSelfWakeupLimits(ctx context.Context, workspaceID pgtype.UUID, req CreateRequest) error {
	maxPerIssue, minInterval := s.selfWakeupLimits(ctx, workspaceID)

	count, err := s.Cerebro.CountActiveWakeupsForAgentIssue(ctx, cerebrodb.CountActiveWakeupsForAgentIssueParams{
		WorkspaceID: workspaceID,
		AgentID:     req.AgentID,
		IssueID:     req.IssueID,
	})
	if err == nil && int(count) >= maxPerIssue {
		return fmt.Errorf("wakeup limit reached: this agent already has %d wakeups on this issue (max %d per issue)", count, maxPerIssue)
	}

	// The minimum gap only applies to time wakeups (the only type with a
	// schedulable fire time). status/CI wakeups fire on external events.
	if req.TriggerType == TriggerTime && req.FireAt.Valid && minInterval > 0 {
		lastFire, err := s.Cerebro.MaxActiveTimeWakeupFireAtForAgentIssue(ctx, cerebrodb.MaxActiveTimeWakeupFireAtForAgentIssueParams{
			WorkspaceID: workspaceID,
			AgentID:     req.AgentID,
			IssueID:     req.IssueID,
		})
		if err == nil && lastFire.Valid {
			earliest := lastFire.Time.Add(minInterval)
			if req.FireAt.Time.Before(earliest) {
				return fmt.Errorf("wakeups must be at least %d minutes apart: this agent already has a wakeup at %s on this issue", int(minInterval.Minutes()), lastFire.Time.UTC().Format(time.RFC3339))
			}
		}
	}
	return nil
}

func (s *Service) postpone(ctx context.Context, row cerebrodb.CerebroAgentWakeup) error {
	newFireAt := pgtype.Timestamptz{Time: time.Now().Add(postponeDelay), Valid: true}
	if err := s.Cerebro.PostponeWakeup(ctx, cerebrodb.PostponeWakeupParams{
		ID:     row.ID,
		FireAt: newFireAt,
	}); err != nil {
		return fmt.Errorf("postpone wakeup: %w", err)
	}
	return nil
}

// checkPostponeLimit increments the postpone counter and sends an inbox
// notification to the issue owner when the limit is reached. Called after
// a successful dispatch so it never blocks the dispatch path.
func (s *Service) checkPostponeLimit(ctx context.Context, row cerebrodb.CerebroAgentWakeup, issue db.Issue) {
	count, err := s.Cerebro.IncrementWakeupPostpones(ctx, row.ID)
	if err != nil {
		slog.Warn("cerebro wakeup: increment postpones failed",
			"wakeup_id", util.UUIDToString(row.ID), "error", err)
		return
	}
	if count < WakeupMaxConsecutivePostpones {
		return
	}
	// Threshold reached — notify the issue owner and reset the counter.
	s.sendPostponeNotification(ctx, row, issue, count)
	if resetErr := s.Cerebro.ResetWakeupPostpones(ctx, row.ID); resetErr != nil {
		slog.Warn("cerebro wakeup: reset postpones failed",
			"wakeup_id", util.UUIDToString(row.ID), "error", resetErr)
	}
}

// sendPostponeNotification sends an inbox item to the member who owns the
// issue (assignee if member, else creator) warning that the wakeup has
// fired consecutiveCount times without resolution.
func (s *Service) sendPostponeNotification(ctx context.Context, row cerebrodb.CerebroAgentWakeup, issue db.Issue, consecutiveCount int32) {
	// Find the human recipient: assignee if member, else creator.
	var recipientID pgtype.UUID
	if issue.AssigneeID.Valid && issue.AssigneeType.Valid && issue.AssigneeType.String == "member" {
		recipientID = issue.AssigneeID
	} else if issue.CreatorID.Valid && issue.CreatorType == "member" {
		recipientID = issue.CreatorID
	}
	if !recipientID.Valid {
		return // no human owner to notify
	}

	details, _ := json.Marshal(map[string]any{
		"wakeup_id":         util.UUIDToString(row.ID),
		"issue_id":          util.UUIDToString(issue.ID),
		"consecutive_count": consecutiveCount,
		"prompt":            row.Prompt,
		"max_postpones":     WakeupMaxConsecutivePostpones,
	})
	_, err := s.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
		WorkspaceID:   row.WorkspaceID,
		RecipientType: "member",
		RecipientID:   recipientID,
		Type:          "wakeup_loop",
		Severity:      "action_required",
		IssueID:       issue.ID,
		Title:         fmt.Sprintf(`Wakeup loop på "%s"`, issue.Title),
		Body: pgtype.Text{
			String: fmt.Sprintf(
				`Agenten har modtaget dette wakeup %d gange i træk uden at afslutte: "%s". Tjek hvad der blokerer.`,
				consecutiveCount, row.Prompt,
			),
			Valid: true,
		},
		ActorType: pgtype.Text{String: "system", Valid: true},
		ActorID:   pgtype.UUID{},
		Details:   details,
		Route:     "inbox",
	})
	if err != nil {
		slog.Warn("cerebro wakeup: postpone inbox notification failed",
			"wakeup_id", util.UUIDToString(row.ID),
			"issue_id", util.UUIDToString(issue.ID),
			"error", err,
		)
	}
}

func (s *Service) validateIssueAndAgent(ctx context.Context, workspaceID, issueID, agentID pgtype.UUID) error {
	if err := s.validateIssue(ctx, workspaceID, issueID); err != nil {
		return err
	}
	agent, err := s.Queries.GetAgent(ctx, agentID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("agent not found")
		}
		return fmt.Errorf("load agent: %w", err)
	}
	if util.UUIDToString(agent.WorkspaceID) != util.UUIDToString(workspaceID) {
		return fmt.Errorf("agent not found")
	}
	if agent.ArchivedAt.Valid {
		return fmt.Errorf("agent is archived")
	}
	return nil
}

func (s *Service) validateIssue(ctx context.Context, workspaceID, issueID pgtype.UUID) error {
	issue, err := s.Queries.GetIssue(ctx, issueID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("issue not found")
		}
		return fmt.Errorf("load issue: %w", err)
	}
	if util.UUIDToString(issue.WorkspaceID) != util.UUIDToString(workspaceID) {
		return fmt.Errorf("issue not found")
	}
	return nil
}

func truncateFailure(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= 1000 {
		return s
	}
	return s[:1000]
}

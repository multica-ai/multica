// Package channels owns the cerebro-only "channel listen mode" feature: a
// per-(channel × agent) toggle deciding whether an agent participating in a
// chat-channel reacts to every comment ('always') or only when explicitly
// @-mentioned ('mention_only'). The default for any agent subscribed to a
// channel is 'always' — rows in cerebro_channel_agent_settings only need to
// exist when the user has flipped an agent into mention_only mode.
package channels

import (
	"context"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ListenModeAlways means the agent reacts to every comment in the channel.
const ListenModeAlways = "always"

// ListenModeMentionOnly means the agent only reacts when @-mentioned. Equivalent
// to today's pre-cerebro behaviour for non-assigned agents.
const ListenModeMentionOnly = "mention_only"

// EventChannelListenModeChanged is the WS event broadcast on listen-mode flip.
// Restricted to channel participants (audience set by caller in the HTTP path).
const EventChannelListenModeChanged = "channel:listen_mode_changed"

// EventChannelArchived is the WS event published when a member archives a
// channel. Per-user state, so the audience is restricted to the acting user's
// own sessions — other channel members do not need to see it.
const EventChannelArchived = "cerebro_channel_archived"

// EventChannelUnarchived is the WS event published when a member unarchives
// a channel. Same audience contract as EventChannelArchived.
const EventChannelUnarchived = "cerebro_channel_unarchived"

// Service ties together the cerebro listen-mode store, the upstream issue/
// agent/subscriber queries, and the task-enqueue service so the comment
// handler can dispatch listen-always agents in one call.
type Service struct {
	CerebroQueries *cerebrodb.Queries
	Queries        *db.Queries
	TaskService    *service.TaskService
	Bus            *events.Bus
}

// New constructs a Service. Callers wire it from the router. Registers a
// re-surface listener on the bus so that any new inbox_item for an archived
// channel/dm clears the per-user archive flag and notifies the user's WS
// sessions.
func New(cerebroQueries *cerebrodb.Queries, queries *db.Queries, taskSvc *service.TaskService, bus *events.Bus) *Service {
	s := &Service{
		CerebroQueries: cerebroQueries,
		Queries:        queries,
		TaskService:    taskSvc,
		Bus:            bus,
	}
	s.registerArchiveResurfaceListener()
	return s
}

// EnqueueChannelListenerTasks enqueues a task for every agent subscribed to
// the channel whose listen_mode is 'always' and that is not already covered
// by the assignee or @mention paths. Safe to call on any issue — non-channel
// kinds and non-member authors short-circuit immediately.
//
// The function is best-effort: a single agent that fails to enqueue logs a
// warning but does not block the rest. The caller does not check an error.
func (s *Service) EnqueueChannelListenerTasks(
	ctx context.Context,
	issue db.Issue,
	comment db.Comment,
	parentComment *db.Comment,
	authorType, authorID string,
) {
	if issue.Kind != "channel" && issue.Kind != "dm" {
		return
	}
	// Restrict to member-authored comments. Letting agent comments fan out to
	// every other listening agent in the same channel turns into a cascade —
	// any agent reply triggers every other agent, which triggers another agent
	// reply, and so on. @mention is the explicit opt-in for agent-to-agent.
	if authorType != "member" {
		return
	}

	// @all is a broadcast — the existing assignee path treats it as a
	// suppress-trigger signal. Listen-mode follows the same rule so the
	// member can address all humans in a busy channel without firing every
	// agent at once.
	commentMentions := util.ParseMentions(comment.Content)
	if util.HasMentionAll(commentMentions) {
		return
	}

	// Pre-collect agent IDs already addressed via @mention so we don't
	// double-enqueue them here. Mirrors the "inherit parent mentions" rule
	// in the upstream comment handler — if a member follows up in a thread
	// they started by @mentioning an agent, those mentions still count.
	mentionedAgentIDs := make(map[string]bool)
	for _, m := range commentMentions {
		if m.Type == "agent" {
			mentionedAgentIDs[m.ID] = true
		}
	}
	if len(commentMentions) == 0 && parentComment != nil && parentComment.AuthorType == "member" {
		for _, m := range util.ParseMentions(parentComment.Content) {
			if m.Type == "agent" {
				mentionedAgentIDs[m.ID] = true
			}
		}
	}

	subs, err := s.Queries.ListIssueSubscribers(ctx, issue.ID)
	if err != nil {
		slog.Warn("listen-mode: list subscribers failed", "channel_id", util.UUIDToString(issue.ID), "error", err)
		return
	}

	for _, sub := range subs {
		if sub.UserType != "agent" {
			continue
		}
		agentID := util.UUIDToString(sub.UserID)

		// Already handled by assignee path — its dedup guard covers the rest.
		if issue.AssigneeType.Valid && issue.AssigneeType.String == "agent" &&
			issue.AssigneeID.Valid && util.UUIDToString(issue.AssigneeID) == agentID {
			continue
		}
		// Already handled by mention path.
		if mentionedAgentIDs[agentID] {
			continue
		}

		// Listen-mode lookup. Default 'always' when no row exists.
		mode := ListenModeAlways
		got, err := s.CerebroQueries.GetChannelAgentListenMode(ctx, cerebrodb.GetChannelAgentListenModeParams{
			ChannelID: issue.ID,
			AgentID:   sub.UserID,
		})
		if err == nil {
			mode = got
		} else if !errors.Is(err, pgx.ErrNoRows) {
			slog.Warn("listen-mode: lookup failed", "channel_id", util.UUIDToString(issue.ID), "agent_id", agentID, "error", err)
			continue
		}
		if mode != ListenModeAlways {
			continue
		}

		// Validate the agent is reachable: not archived, has a runtime.
		// Mirrors the gate in the mention path so we don't queue tasks no
		// runtime will ever pick up.
		agent, err := s.Queries.GetAgent(ctx, sub.UserID)
		if err != nil || agent.ArchivedAt.Valid || !agent.RuntimeID.Valid {
			continue
		}

		// Dedup: same as the mention path. Coalesces rapid-fire comments.
		hasPending, err := s.Queries.HasPendingTaskForIssueAndAgent(ctx, db.HasPendingTaskForIssueAndAgentParams{
			IssueID: issue.ID,
			AgentID: sub.UserID,
		})
		if err != nil || hasPending {
			continue
		}

		if _, err := s.TaskService.EnqueueTaskForMention(ctx, issue, sub.UserID, comment.ID); err != nil {
			slog.Warn("listen-mode: enqueue failed", "channel_id", util.UUIDToString(issue.ID), "agent_id", agentID, "error", err)
		}
	}
}

// SetListenMode upserts the listen-mode for a single (channel, agent) pair.
// 'always' values are stored explicitly so the read path can return the
// override deterministically; setting back to default also writes a row
// rather than deleting, to keep behaviour observable from the UI without
// a magic absence-of-row meaning.
func (s *Service) SetListenMode(ctx context.Context, channelID, agentID pgtype.UUID, mode string) error {
	return s.CerebroQueries.UpsertChannelAgentListenMode(ctx, cerebrodb.UpsertChannelAgentListenModeParams{
		ChannelID:  channelID,
		AgentID:    agentID,
		ListenMode: mode,
	})
}

// ListListenModes returns every explicit listen-mode row for a channel.
// Agents subscribed to the channel without a row default to 'always'; the
// frontend applies that default when a row is missing.
func (s *Service) ListListenModes(ctx context.Context, channelID pgtype.UUID) ([]cerebrodb.ListChannelAgentListenModesRow, error) {
	return s.CerebroQueries.ListChannelAgentListenModes(ctx, channelID)
}

// ArchiveChannel marks (channelID, userID) as archived. Idempotent — calling
// it on an already-archived channel just refreshes archived_at.
func (s *Service) ArchiveChannel(ctx context.Context, channelID, userID pgtype.UUID) error {
	return s.CerebroQueries.ArchiveChannelForUser(ctx, cerebrodb.ArchiveChannelForUserParams{
		ChannelID: channelID,
		UserID:    userID,
	})
}

// UnarchiveChannel removes the archive flag for (channelID, userID).
// Idempotent — deleting a missing row is not an error.
func (s *Service) UnarchiveChannel(ctx context.Context, channelID, userID pgtype.UUID) error {
	return s.CerebroQueries.UnarchiveChannelForUser(ctx, cerebrodb.UnarchiveChannelForUserParams{
		ChannelID: channelID,
		UserID:    userID,
	})
}

// GetChannelArchivedAt returns the archived_at timestamp for (channelID, userID).
// Returns ok=false (no error) when the channel is not archived for the user.
func (s *Service) GetChannelArchivedAt(ctx context.Context, channelID, userID pgtype.UUID) (pgtype.Timestamptz, bool, error) {
	ts, err := s.CerebroQueries.GetChannelArchivedAtForUser(ctx, cerebrodb.GetChannelArchivedAtForUserParams{
		ChannelID: channelID,
		UserID:    userID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return pgtype.Timestamptz{}, false, nil
		}
		return pgtype.Timestamptz{}, false, err
	}
	return ts, true, nil
}

// FilterArchivedChannels removes channels archived by userID from rows. When
// includeArchived is true the input is returned unchanged so the "Show
// archived" view can still reach the rows. Failures fall through to the
// unfiltered list so a transient DB error doesn't hide the inbox entirely.
func (s *Service) FilterArchivedChannels(
	ctx context.Context,
	userID pgtype.UUID,
	rows []db.ListChannelsForUserRow,
	includeArchived bool,
) []db.ListChannelsForUserRow {
	if includeArchived || len(rows) == 0 {
		return rows
	}
	archived, err := s.CerebroQueries.ListArchivedChannelsForUser(ctx, userID)
	if err != nil {
		slog.Warn("channel-list: list archived failed", "user_id", util.UUIDToString(userID), "error", err)
		return rows
	}
	if len(archived) == 0 {
		return rows
	}
	skip := make(map[string]bool, len(archived))
	for _, a := range archived {
		skip[util.UUIDToString(a.ChannelID)] = true
	}
	out := rows[:0]
	for _, row := range rows {
		if !skip[util.UUIDToString(row.ID)] {
			out = append(out, row)
		}
	}
	return out
}

// registerArchiveResurfaceListener subscribes to inbox:new events. When a new
// inbox item lands for a member-recipient on a channel/dm that the recipient
// has archived, the archive row is deleted and a cerebro_channel_unarchived
// event is published to the recipient's own WS sessions. Best-effort: any
// failure is logged but does not bubble — re-surface is a UX nicety, not a
// correctness gate.
func (s *Service) registerArchiveResurfaceListener() {
	if s.Bus == nil {
		return
	}
	s.Bus.Subscribe(protocol.EventInboxNew, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		item, ok := payload["item"].(map[string]any)
		if !ok {
			return
		}
		recipientType, _ := item["recipient_type"].(string)
		if recipientType != "member" {
			return
		}
		recipientIDStr, _ := item["recipient_id"].(string)
		issueIDPtr, _ := item["issue_id"].(*string)
		if recipientIDStr == "" || issueIDPtr == nil || *issueIDPtr == "" {
			return
		}
		recipientID, err := util.ParseUUID(recipientIDStr)
		if err != nil {
			return
		}
		channelID, err := util.ParseUUID(*issueIDPtr)
		if err != nil {
			return
		}
		s.maybeUnarchiveOnInbox(context.Background(), channelID, recipientID, e.WorkspaceID, e.ActorType, e.ActorID)
	})
}

// maybeUnarchiveOnInbox re-surfaces a channel/dm in the recipient's inbox
// when a new inbox_item lands for it. No-op for non-channel issues or when
// the recipient hasn't archived the channel.
func (s *Service) maybeUnarchiveOnInbox(
	ctx context.Context,
	channelID, userID pgtype.UUID,
	workspaceID, actorType, actorID string,
) {
	issue, err := s.Queries.GetIssue(ctx, channelID)
	if err != nil {
		return
	}
	if issue.Kind != "channel" && issue.Kind != "dm" {
		return
	}
	_, archived, err := s.GetChannelArchivedAt(ctx, channelID, userID)
	if err != nil || !archived {
		return
	}
	if err := s.UnarchiveChannel(ctx, channelID, userID); err != nil {
		slog.Warn("re-surface: unarchive failed",
			"channel_id", util.UUIDToString(channelID),
			"user_id", util.UUIDToString(userID),
			"error", err)
		return
	}
	userIDStr := util.UUIDToString(userID)
	s.Bus.Publish(events.Event{
		Type:        EventChannelUnarchived,
		WorkspaceID: workspaceID,
		ActorType:   actorType,
		ActorID:     actorID,
		Payload: map[string]any{
			"channel_id": util.UUIDToString(channelID),
			"user_id":    userIDStr,
		},
		AudienceUserIDs: []string{userIDStr},
	})
}

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

// AgentTriggerGateFn gates listen-mode triggered agents through the cerebro
// group-permission allowlist (JEH-1727). The router wires it to a closure
// that resolves the comment author's viewer (admin flag + group IDs) and
// calls grouppermissions.CanUseAgent; nil = no gate (upstream-only fixtures,
// upstream sync tests). Treat ownerID as optional — pgtype.UUID{} skips the
// owner-exemption path.
type AgentTriggerGateFn func(ctx context.Context, workspaceID, userID, agentID, ownerID pgtype.UUID) (bool, error)

// Service ties together the cerebro listen-mode store, the upstream issue/
// agent/subscriber queries, and the task-enqueue service so the comment
// handler can dispatch listen-always agents in one call.
type Service struct {
	CerebroQueries *cerebrodb.Queries
	Queries        *db.Queries
	TaskService    *service.TaskService
	Bus            *events.Bus
	// AgentTriggerGate is the cerebro group-permission allowlist applied to
	// listen-mode triggers. Set by the router after construction. JEH-1727 —
	// closes the trigger-path hole exposed by the Filip-can-trigger-Mia case.
	AgentTriggerGate AgentTriggerGateFn
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

	// JEH-1727: parse the author's user UUID once so the gate doesn't re-parse
	// for every agent in the channel. Empty/invalid stays as zero-value UUID;
	// the gate refuses on a zero UUID, which is the conservative behaviour for
	// a comment without a resolvable author.
	authorUUID, _ := util.ParseUUID(authorID)

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

		// JEH-1727: cerebro group-permission allowlist. The listen-mode path
		// used to enqueue every subscribed-always agent without consulting the
		// commenter's group access, which let Filip wake Mia even when his
		// effective groups didn't include her. Mirrors the gate in
		// enqueueMentionedAgentTasks (handler/comment.go).
		if s.AgentTriggerGate != nil {
			allowed, err := s.AgentTriggerGate(ctx, issue.WorkspaceID, authorUUID, sub.UserID, agent.OwnerID)
			if err != nil {
				slog.Warn("listen-mode: agent gate failed", "channel_id", util.UUIDToString(issue.ID), "agent_id", agentID, "error", err)
				continue
			}
			if !allowed {
				continue
			}
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
		s.MaybeUnarchiveForUser(context.Background(), channelID, recipientID, e.WorkspaceID, e.ActorType, e.ActorID)
	})
}

// MaybeUnarchiveForUser re-surfaces a channel/dm in the user's inbox when a
// signal arrives that they want it back — a new inbox_item lands (re-surface
// listener), or the user explicitly reopens the DM via the new-message modal
// (JEH-1046). No-op for non-channel issues or when the user hasn't archived
// the channel. Best-effort: failures are logged but never bubble.
func (s *Service) MaybeUnarchiveForUser(
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

// SubscriberReasonMentionPromote is the issue_subscriber.reason stamped on
// participants added by the DM-to-channel promotion path. We reuse the
// existing 'mentioned' enum value (see migrations/015_issue_subscriber.up.sql)
// to avoid widening the CHECK constraint for a single new code path —
// semantically the entity joined the channel because they were mentioned.
const SubscriberReasonMentionPromote = "mentioned"

// PromoteDMOnMention promotes a DM to a multi-party channel when a comment
// mentions a member or agent that is not already a participant (JEH-1131).
// Once promoted, the DM peer's "is this still a DM" semantics no longer
// hold — `GetDMByMembers` requires exactly two member subscribers, so the
// next attempt to reopen the DM with the same peer will create a fresh DM
// instead of returning this (now-channel) row, which matches the intuition
// that "1:1 with Sara" and "Sara + Mia + a question" are different threads.
//
// Best-effort: every failure path logs and returns. The promotion is a UX
// nicety on top of the existing mention-enqueue flow and must not block
// comment creation if something downstream is unavailable.
//
// Mention parsing mirrors enqueueMentionedAgentTasks: a reply with no
// explicit mentions inherits the thread root's mentions when both author
// and parent author are members (see shouldInheritParentMentions). @all is
// skipped — broadcasting to "everyone" is not a request to expand the
// participant set. Self-mentions (the author mentioning themselves, somehow)
// are filtered the same way.
func (s *Service) PromoteDMOnMention(
	ctx context.Context,
	issue db.Issue,
	comment db.Comment,
	parentComment *db.Comment,
	workspaceID, actorType, actorID string,
) {
	if issue.Kind != "dm" {
		return
	}
	if s.Bus == nil {
		return
	}

	mentions := util.ParseMentions(comment.Content)
	// Mirror the comment-handler inheritance rule: replies with no explicit
	// mentions, authored by a member in a thread started by a member, inherit
	// the root's mentions. We only need to inherit when there are no own
	// mentions and the parent is a member-authored comment.
	if len(mentions) == 0 && parentComment != nil &&
		parentComment.AuthorType == "member" && comment.AuthorType == "member" {
		mentions = util.ParseMentions(parentComment.Content)
	}
	if len(mentions) == 0 || util.HasMentionAll(mentions) {
		return
	}

	// Pull the current participant list once — we use it both to decide
	// which mentions are "new" and (post-add) to build the title.
	subs, err := s.Queries.ListIssueSubscribers(ctx, issue.ID)
	if err != nil {
		slog.Warn("dm-promote: list subscribers failed",
			"channel_id", util.UUIDToString(issue.ID), "error", err)
		return
	}
	current := make(map[string]bool, len(subs))
	for _, sub := range subs {
		current[sub.UserType+":"+util.UUIDToString(sub.UserID)] = true
	}

	type newSub struct {
		userType string
		userID   pgtype.UUID
	}
	var toAdd []newSub
	for _, m := range mentions {
		if m.Type != "agent" && m.Type != "member" {
			continue // skip @all and issue refs
		}
		// Filter self-mentions: a member mentioning themselves shouldn't
		// expand the participant set.
		if m.Type == comment.AuthorType && m.ID == util.UUIDToString(comment.AuthorID) {
			continue
		}
		userUUID, err := util.ParseUUID(m.ID)
		if err != nil {
			continue
		}
		key := m.Type + ":" + m.ID
		if current[key] {
			continue
		}
		// Dedupe within this comment so two mentions of the same agent in
		// one message don't try to insert twice (AddIssueSubscriber is
		// idempotent, but the broadcast loop below would over-publish).
		current[key] = true
		toAdd = append(toAdd, newSub{userType: m.Type, userID: userUUID})
	}
	if len(toAdd) == 0 {
		return
	}

	// Subscribe the new participants first so the channel state is coherent
	// the moment kind flips: every subscriber the frontend resolves from
	// participants[] is already in the row. AddIssueSubscriber is idempotent
	// (INSERT ... ON CONFLICT DO NOTHING) so this is safe to retry on a
	// partial earlier promotion.
	for _, ns := range toAdd {
		if err := s.Queries.AddIssueSubscriber(ctx, db.AddIssueSubscriberParams{
			IssueID:  issue.ID,
			UserType: ns.userType,
			UserID:   ns.userID,
			Reason:   SubscriberReasonMentionPromote,
		}); err != nil {
			slog.Warn("dm-promote: add subscriber failed",
				"channel_id", util.UUIDToString(issue.ID),
				"user_type", ns.userType,
				"user_id", util.UUIDToString(ns.userID),
				"error", err)
		}
	}

	// Build the auto-generated title from the full (post-add) participant
	// list. Names are joined with ", " in subscribed-at order so the title
	// is stable across calls and reads like "Jesper, Mia, Fætta". Empty
	// list (shouldn't happen — we just added rows) falls back to a generic
	// label so the channel isn't anonymous in the inbox.
	titleArg := pgtype.Text{}
	if issue.Title == "" {
		names, err := s.Queries.ListChannelParticipantNames(ctx, db.ListChannelParticipantNamesParams{
			IssueID:     issue.ID,
			WorkspaceID: issue.WorkspaceID,
		})
		if err != nil {
			slog.Warn("dm-promote: list participant names failed",
				"channel_id", util.UUIDToString(issue.ID), "error", err)
		} else {
			joined := joinNames(names)
			if joined != "" {
				titleArg = pgtype.Text{String: joined, Valid: true}
			}
		}
	}

	promoted, err := s.Queries.PromoteDMToChannel(ctx, db.PromoteDMToChannelParams{
		ID:    issue.ID,
		Title: titleArg,
	})
	if err != nil {
		// ErrNoRows = already promoted (kind != 'dm' before our UPDATE).
		// Still publish subscriber:added events below so the late-arriving
		// participants are visible in the inbox.
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Warn("dm-promote: promote failed",
				"channel_id", util.UUIDToString(issue.ID), "error", err)
			return
		}
		promoted = issue
		promoted.Kind = "channel"
	}

	// Audience: every member subscriber (now includes any freshly-added
	// members). Agent subscribers don't receive WS events.
	audience := s.memberSubscriberIDs(ctx, promoted.ID)

	// channel:updated tells every connected member-client to refetch the
	// channel list — the prefix handler in use-realtime-sync.ts invalidates
	// channelKeys.list, so the inbox swaps "DM with Sara" for the new
	// channel title and the side panel re-renders kind-specific UI.
	s.Bus.Publish(events.Event{
		Type:        protocol.EventChannelUpdated,
		WorkspaceID: workspaceID,
		ActorType:   actorType,
		ActorID:     actorID,
		Payload: map[string]any{
			"channel_id":    util.UUIDToString(promoted.ID),
			"kind":          promoted.Kind,
			"title":         promoted.Title,
			"promoted_from": "dm",
		},
		AudienceUserIDs: audience,
	})

	// subscriber:added for each new participant — let the participants
	// panel and any other subscriber-aware UI react without a separate
	// channel-list refetch. Member additions get the same audience; agents
	// just need the listen-mode panel to know they're in.
	for _, ns := range toAdd {
		s.Bus.Publish(events.Event{
			Type:        protocol.EventSubscriberAdded,
			WorkspaceID: workspaceID,
			ActorType:   actorType,
			ActorID:     actorID,
			Payload: map[string]any{
				"issue_id":  util.UUIDToString(promoted.ID),
				"user_type": ns.userType,
				"user_id":   util.UUIDToString(ns.userID),
				"reason":    SubscriberReasonMentionPromote,
			},
			AudienceUserIDs: audience,
		})
	}
}

// memberSubscriberIDs returns the user-id strings of every member subscriber
// of an issue. Used as the WS audience when a channel event must reach the
// participants (and only the participants).
func (s *Service) memberSubscriberIDs(ctx context.Context, issueID pgtype.UUID) []string {
	subs, err := s.Queries.ListIssueSubscribers(ctx, issueID)
	if err != nil {
		return []string{}
	}
	out := make([]string, 0, len(subs))
	for _, sub := range subs {
		if sub.UserType == "member" {
			out = append(out, util.UUIDToString(sub.UserID))
		}
	}
	return out
}

// joinNames formats a participant-name list as "A, B, C". Trims empty
// names so a missing display name doesn't produce ", , B".
func joinNames(rows []db.ListChannelParticipantNamesRow) string {
	out := ""
	for _, r := range rows {
		if r.DisplayName == "" {
			continue
		}
		if out == "" {
			out = r.DisplayName
		} else {
			out += ", " + r.DisplayName
		}
	}
	return out
}

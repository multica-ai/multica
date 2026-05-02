package channel

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// MessageService owns channel_message reads and writes. Like ChannelService
// it has no platform integrations — handlers do mention parsing, task queue
// fan-out, and inbox writes around the service's plain data calls.
type MessageService struct {
	Queries *db.Queries
}

// NewMessageService constructs a MessageService.
func NewMessageService(q *db.Queries) *MessageService {
	return &MessageService{Queries: q}
}

// DefaultPageLimit and MaxPageLimit are the page-size guardrails for List.
// MaxPageLimit guards memory and frontend rendering; the wire protocol clamps
// at this value, callers don't have to.
const (
	DefaultPageLimit int32 = 50
	MaxPageLimit     int32 = 200
)

// Create inserts a new message into a channel. The author's membership is
// NOT verified here — handlers do that as part of authorization. This keeps
// the service usable from background jobs (e.g. retention sweep posting an
// admin notice) without an extra query.
func (s *MessageService) Create(ctx context.Context, p CreateMessageParams) (db.ChannelMessage, error) {
	if err := validateActorType(p.Author.Type); err != nil {
		return db.ChannelMessage{}, err
	}
	if p.Content == "" {
		return db.ChannelMessage{}, fmt.Errorf("%w: content must not be empty", ErrInvalid)
	}

	args := db.CreateChannelMessageParams{
		ChannelID:  p.ChannelID,
		AuthorType: p.Author.Type,
		AuthorID:   p.Author.ID,
		Content:    p.Content,
	}
	if p.ParentMessageID != nil {
		args.ParentMessageID = *p.ParentMessageID
	}
	if len(p.Metadata) > 0 {
		args.Metadata = p.Metadata
	}
	return s.Queries.CreateChannelMessage(ctx, args)
}

// Get returns a single message by id. Returns ErrNotFound if absent.
func (s *MessageService) Get(ctx context.Context, id pgtype.UUID) (db.ChannelMessage, error) {
	m, err := s.Queries.GetChannelMessage(ctx, id)
	return m, translateNotFound(err)
}

// List returns messages in a channel, newest first, with cursor-based
// pagination. The default view excludes thread replies; pass
// IncludeThreaded=true for the full stream (used by search and sidecars).
//
// The Limit field is clamped to [1, MaxPageLimit]; 0 falls back to the
// default. BeforeCreatedAt nil returns the newest page.
func (s *MessageService) List(ctx context.Context, p ListMessagesParams) ([]db.ChannelMessage, error) {
	limit := p.Limit
	if limit <= 0 {
		limit = DefaultPageLimit
	}
	if limit > MaxPageLimit {
		limit = MaxPageLimit
	}
	before := pgtype.Timestamptz{}
	if p.BeforeCreatedAt != nil {
		before = *p.BeforeCreatedAt
	}

	if p.IncludeThreaded {
		return s.Queries.ListChannelMessagesIncludingThreads(ctx, db.ListChannelMessagesIncludingThreadsParams{
			ChannelID:       p.ChannelID,
			BeforeCreatedAt: before,
			Limit:           limit,
		})
	}
	return s.Queries.ListChannelMessages(ctx, db.ListChannelMessagesParams{
		ChannelID:       p.ChannelID,
		BeforeCreatedAt: before,
		Limit:           limit,
	})
}

// UpdateContent edits the body of an existing message. Phase 5 contract:
// only the original author may edit, the caller must already be the
// resolved actor (member or agent) — admins cannot edit someone else's
// message even if they could delete it. Sets edited_at = now() so the
// UI can render an "(edited)" hint.
//
// Returns ErrForbidden when the actor isn't the author, ErrNotFound
// when the message doesn't exist, ErrInvalid when content is empty,
// ErrChannelClosed when the message has already been soft-deleted.
func (s *MessageService) UpdateContent(ctx context.Context, messageID pgtype.UUID, author Actor, content string) (db.ChannelMessage, error) {
	if content == "" {
		return db.ChannelMessage{}, fmt.Errorf("%w: content must not be empty", ErrInvalid)
	}
	existing, err := s.Get(ctx, messageID)
	if err != nil {
		return db.ChannelMessage{}, err
	}
	if existing.DeletedAt.Valid {
		return db.ChannelMessage{}, ErrChannelClosed
	}
	if existing.AuthorType != author.Type || existing.AuthorID.Bytes != author.ID.Bytes {
		return db.ChannelMessage{}, ErrForbidden
	}
	updated, err := s.Queries.UpdateChannelMessage(ctx, db.UpdateChannelMessageParams{
		ID:      messageID,
		Content: content,
	})
	return updated, translateNotFound(err)
}

// IsChannelAdmin reports whether the given actor has admin role on
// the given channel. Used by Delete to allow channel admins to
// remove others' messages (the spec calls out moderation as a
// distinct role from authorship).
func (s *MessageService) IsChannelAdmin(ctx context.Context, channelID pgtype.UUID, actor Actor) (bool, error) {
	mem, err := s.Queries.GetChannelMembership(ctx, db.GetChannelMembershipParams{
		ChannelID:  channelID,
		MemberType: actor.Type,
		MemberID:   actor.ID,
	})
	if err != nil {
		// pgx.ErrNoRows here means "not a member" — treat as not-admin.
		return false, nil
	}
	return mem.Role == RoleAdmin, nil
}

// Delete soft-deletes a message. Allowed when the actor is the
// original author OR holds RoleAdmin on the channel. The deletion
// reason is stamped into the row so audit / moderation tooling can
// distinguish self-deletes from admin removals.
//
// Returns ErrForbidden when neither condition holds, ErrNotFound when
// the message doesn't exist. Already-deleted messages are silently
// idempotent (returns the existing row's state).
func (s *MessageService) Delete(ctx context.Context, messageID pgtype.UUID, actor Actor) (db.ChannelMessage, error) {
	existing, err := s.Get(ctx, messageID)
	if err != nil {
		return db.ChannelMessage{}, err
	}
	if existing.DeletedAt.Valid {
		// Idempotent: already soft-deleted.
		return existing, nil
	}
	isAuthor := existing.AuthorType == actor.Type && existing.AuthorID.Bytes == actor.ID.Bytes
	reason := DeletedByUser
	if !isAuthor {
		isAdmin, err := s.IsChannelAdmin(ctx, existing.ChannelID, actor)
		if err != nil {
			return db.ChannelMessage{}, err
		}
		if !isAdmin {
			return db.ChannelMessage{}, ErrForbidden
		}
		reason = DeletedByAdmin
	}
	if err := s.Queries.SoftDeleteChannelMessage(ctx, db.SoftDeleteChannelMessageParams{
		ID:              messageID,
		DeletionReason:  pgtype.Text{String: reason, Valid: true},
	}); err != nil {
		return db.ChannelMessage{}, err
	}
	// Return the post-delete state.
	return s.Get(ctx, messageID)
}

// ListThread returns the replies under a parent message in chronological
// order. Soft-deleted replies are excluded.
func (s *MessageService) ListThread(ctx context.Context, parentID pgtype.UUID) ([]db.ChannelMessage, error) {
	return s.Queries.ListThreadReplies(ctx, parentID)
}

// Count returns the number of non-deleted messages in a channel.
func (s *MessageService) Count(ctx context.Context, channelID pgtype.UUID) (int64, error) {
	return s.Queries.CountChannelMessages(ctx, channelID)
}

// MentionTriggerCandidate is one entry in the result of
// SelectAgentsForMention — an agent the caller should enqueue a task
// for. Carries enough context (agent record + the parsed mention) that
// the handler can build a clean log line and call EnqueueTaskForChannelMention
// without re-loading the agent.
type MentionTriggerCandidate struct {
	AgentID pgtype.UUID
	Agent   db.Agent
}

// SelectAgentsForMentionParams is the input shape for the selector.
type SelectAgentsForMentionParams struct {
	ChannelID pgtype.UUID
	// ChannelKind is "channel" | "dm". DMs trigger every agent member
	// implicitly even without an @mention — the whole conversation is
	// addressed at the agent participant(s).
	ChannelKind string
	Content     string
	Author      Actor
	// DedupWindowSeconds is how recent a pending task counts as a
	// "duplicate"; passed through to the SQL query. The handler reads
	// this from CHANNEL_AGENT_DEDUP_WINDOW_SECONDS env (default 30).
	DedupWindowSeconds float64
	// MentionParser parses @member / @agent mentions out of markdown.
	// Injected so the channel package doesn't import server/internal/util
	// (sidecar portability — see Phase 1 service-handler split note).
	// Caller passes util.ParseMentions in production.
	MentionParser func(content string) []ParsedMention
}

// ParsedMention is the surface area the channel package uses from the
// caller's mention parser. Mirrors util.Mention without requiring the
// import. Type values: "member" | "agent" | "issue" | "all".
type ParsedMention struct {
	Type string
	ID   string
}

// SelectAgentsForMention returns the agents that should be triggered
// by a freshly-posted message. Applies, in order:
//
//  1. Parse @-mentions from the markdown content.
//  2. Filter to type=="agent" mentions only — Phase 1's inbox writes
//     handle @member.
//  3. Self-mention guard: if the message was authored by an agent,
//     skip mentions of that same agent (cycle protection).
//  4. Channel-membership filter: agents must already be members of
//     the channel — joining is a deliberate admin action, mentions
//     of non-members render but produce no task. This is the spec's
//     "agents-as-channel-members" model.
//  5. Reachability filter: the agent must be non-archived and have a
//     runtime attached. (Pre-flight here so the handler doesn't enqueue
//     a task that will fail at the daemon.)
//  6. Dedup guard: if the same agent already has an in-flight task for
//     this channel within DedupWindowSeconds, skip — a rapid burst of
//     @-mentions shouldn't enqueue a task per message.
//
// The order matters: cheap filters first, then the network-touching
// queries (membership + agent + dedup), so a typical "no @mentions in
// the message" message exits after step 1.
func (s *MessageService) SelectAgentsForMention(ctx context.Context, p SelectAgentsForMentionParams) ([]MentionTriggerCandidate, error) {
	if p.MentionParser == nil {
		return nil, fmt.Errorf("%w: MentionParser required", ErrInvalid)
	}
	mentions := p.MentionParser(p.Content)
	// In a DM, every member message addresses every agent member of the
	// DM implicitly — explicit @mention is redundant. We only fan out for
	// member-authored messages: agent-authored messages auto-triggering
	// other agents in a DM would create endless ping-pong loops.
	autoTriggerDM := p.ChannelKind == KindDM && p.Author.Type == ActorMember
	if len(mentions) == 0 && !autoTriggerDM {
		return nil, nil
	}

	// Dedup mention list at the (type, id) tuple level so a message that
	// @-mentions the same agent twice produces one trigger.
	seen := make(map[string]struct{}, len(mentions))
	// Tracks agents already added (mention or DM-auto) so the DM-auto
	// fanout doesn't duplicate an agent that was just selected by an
	// explicit @mention in the same message.
	added := make(map[string]struct{}, len(mentions))
	candidates := make([]MentionTriggerCandidate, 0)
	for _, m := range mentions {
		if m.Type != ActorAgent {
			continue
		}
		// Self-mention guard.
		if p.Author.Type == ActorAgent && m.ID == uuidString(p.Author.ID) {
			continue
		}
		key := m.Type + ":" + m.ID
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}

		agentUUID, err := parseUUIDString(m.ID)
		if err != nil {
			// Malformed mention id — skip silently rather than failing
			// the whole message post.
			continue
		}

		// Membership: agent must be in the channel.
		if _, err := s.Queries.GetChannelMembership(ctx, db.GetChannelMembershipParams{
			ChannelID:  p.ChannelID,
			MemberType: ActorAgent,
			MemberID:   agentUUID,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			return nil, fmt.Errorf("check membership: %w", err)
		}

		// Reachability.
		agent, err := s.Queries.GetAgent(ctx, agentUUID)
		if err != nil || agent.ArchivedAt.Valid || !agent.RuntimeID.Valid {
			continue
		}

		// Dedup against in-flight tasks for this channel.
		hasPending, err := s.Queries.HasPendingChannelMentionForAgent(ctx, db.HasPendingChannelMentionForAgentParams{
			AgentID:       agentUUID,
			ChannelID:     uuidString(p.ChannelID),
			WindowSeconds: p.DedupWindowSeconds,
		})
		if err != nil {
			return nil, fmt.Errorf("check dedup: %w", err)
		}
		if hasPending {
			continue
		}

		added[m.ID] = struct{}{}
		candidates = append(candidates, MentionTriggerCandidate{
			AgentID: agentUUID,
			Agent:   agent,
		})
	}

	// DM auto-trigger fanout. In a DM with one or more agents, every
	// member-authored message implicitly addresses every agent member
	// — the user shouldn't have to @-mention an agent in a 1:1 DM with
	// it. Skipped when the author is itself an agent (avoids loops) or
	// when the channel is a regular shared channel (existing @mention
	// gating is the right behavior there).
	if autoTriggerDM {
		members, err := s.Queries.ListChannelMembers(ctx, p.ChannelID)
		if err != nil {
			return nil, fmt.Errorf("list channel members for DM auto-trigger: %w", err)
		}
		for _, m := range members {
			if m.MemberType != ActorAgent {
				continue
			}
			agentIDStr := uuidString(m.MemberID)
			if _, dup := added[agentIDStr]; dup {
				continue
			}
			// Author guard: an agent's own DM message shouldn't
			// re-trigger itself (and the autoTriggerDM gate already
			// excludes agent-authored messages, but defensive double-
			// check survives future refactors).
			if p.Author.Type == ActorAgent && agentIDStr == uuidString(p.Author.ID) {
				continue
			}

			agent, err := s.Queries.GetAgent(ctx, m.MemberID)
			if err != nil || agent.ArchivedAt.Valid || !agent.RuntimeID.Valid {
				slog.Info("SelectAgentsForMention: dm-auto skip unreachable agent",
					"agent_id", agentIDStr,
					"err", err,
					"archived", agent.ArchivedAt.Valid,
					"has_runtime", agent.RuntimeID.Valid,
				)
				continue
			}

			hasPending, err := s.Queries.HasPendingChannelMentionForAgent(ctx, db.HasPendingChannelMentionForAgentParams{
				AgentID:       m.MemberID,
				ChannelID:     uuidString(p.ChannelID),
				WindowSeconds: p.DedupWindowSeconds,
			})
			if err != nil {
				return nil, fmt.Errorf("check dedup (dm-auto): %w", err)
			}
			if hasPending {
				slog.Info("SelectAgentsForMention: dm-auto skip dedup-window pending", "agent_id", agentIDStr)
				continue
			}

			slog.Info("SelectAgentsForMention: dm-auto candidate selected", "agent_id", agentIDStr)
			added[agentIDStr] = struct{}{}
			candidates = append(candidates, MentionTriggerCandidate{
				AgentID: m.MemberID,
				Agent:   agent,
			})
		}
	}

	return candidates, nil
}

// parseUUIDString is a lightweight UUID parser kept inside the channel
// package to avoid a dependency on internal/util (sidecar portability).
func parseUUIDString(s string) (pgtype.UUID, error) {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		return pgtype.UUID{}, err
	}
	return u, nil
}

// SoftDeleteOldMessages drains messages older than `before` from one
// channel in batches of `batchSize`. Returns the number of messages
// soft-deleted in this batch; zero means the channel is drained. Phase 2
// uses this from the retention cron.
func (s *MessageService) SoftDeleteOldMessages(ctx context.Context, channelID pgtype.UUID, before time.Time, batchSize int32) (int64, error) {
	if batchSize <= 0 {
		batchSize = 1000
	}
	return s.Queries.SoftDeleteOldChannelMessages(ctx, db.SoftDeleteOldChannelMessagesParams{
		ChannelID: channelID,
		CreatedAt: pgtype.Timestamptz{Time: before, Valid: true},
		Limit:     batchSize,
	})
}

// RetentionSweepStats reports per-run aggregate counts. The cron loop logs
// these so operators can see "did anything happen?" at a glance.
type RetentionSweepStats struct {
	ChannelsScanned int
	MessagesDeleted int64
}

// RunRetentionSweep performs the daily retention pass: iterate every
// non-archived channel whose effective retention is finite, soft-delete
// messages older than the cutoff in batches of `batchSize`. Idempotent —
// already-deleted rows are filtered by the underlying query's WHERE.
//
// `now` is injectable so tests can pin a deterministic cutoff. Production
// callers pass time.Now().UTC().
//
// Failure mode: if a single channel's batched delete errors, we log and
// continue — one bad workspace shouldn't stall the rest of the sweep.
// The error is captured in stats so callers can decide whether to alert.
func (s *MessageService) RunRetentionSweep(ctx context.Context, now time.Time, batchSize int32) (RetentionSweepStats, error) {
	candidates, err := s.Queries.ListChannelsWithRetention(ctx)
	if err != nil {
		return RetentionSweepStats{}, fmt.Errorf("retention sweep: list candidates: %w", err)
	}
	if batchSize <= 0 {
		batchSize = 1000
	}

	var stats RetentionSweepStats
	for _, c := range candidates {
		if c.EffectiveDays <= 0 {
			// Defensive: the SQL filter excludes <=0, but a future schema
			// change to allow 0 ("immediate") shouldn't accidentally wipe
			// a workspace mid-deploy. Skip rather than interpret.
			continue
		}
		stats.ChannelsScanned++
		cutoff := now.Add(-time.Duration(c.EffectiveDays) * 24 * time.Hour)

		// Drain the channel in batches. We cap the inner loop at 100 rounds
		// (= 100 × batchSize messages) per channel per run so a single
		// pathological channel can't monopolize one sweep — rare, but
		// retention can be flipped from "forever" to "30 days" on a
		// chatty long-lived channel and produce a huge candidate set.
		for round := 0; round < 100; round++ {
			n, err := s.SoftDeleteOldMessages(ctx, c.ChannelID, cutoff, batchSize)
			if err != nil {
				return stats, fmt.Errorf("retention sweep: channel %s: %w", uuidString(c.ChannelID), err)
			}
			stats.MessagesDeleted += n
			if n < int64(batchSize) {
				break
			}
		}
	}
	return stats, nil
}

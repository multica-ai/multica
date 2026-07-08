package lark

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

const (
	// defaultHistoryLimit is the page size used when the caller asks for none.
	defaultHistoryLimit = 20
	// maxHistoryLimit caps a single page (Lark's own im/v1/messages cap is 50)
	// so a pull can't dump an unbounded transcript into the agent's context.
	maxHistoryLimit = 50
)

// historyQueries is the slice of the channel store the reader needs. *ChannelStore
// satisfies it; tests inject a fake.
type historyQueries interface {
	GetLarkChatSessionBindingBySession(ctx context.Context, chatSessionID pgtype.UUID) (ChatSessionBinding, error)
	GetLarkInstallation(ctx context.Context, id pgtype.UUID) (Installation, error)
}

// History reads a Lark (Feishu) conversation on demand — the pull side of the
// unified `multica chat history` (chat overview) and `multica chat thread [id]`
// (one 话题/topic) commands (MUL-4166), the same contract Slack's reader serves.
// A Lark session spans the whole chat (channel_chat_id is the real chat_id), so
// the overview is the chat's recent messages and a thread read drills into one
// topic. The chat is resolved server-side from the binding and never taken from
// the agent, so a thread id is only a within-chat locator. Sessions with no
// Feishu binding (web-only, or a different channel) return
// channel.ErrNoChannelSession.
type History struct {
	q      historyQueries
	creds  CredentialsResolver
	client APIClient
	logger *slog.Logger
}

// NewHistory builds the reader over the channel store, the app-secret decrypter
// (*InstallationService at wiring time), and the Lark HTTP client.
func NewHistory(q historyQueries, creds CredentialsResolver, client APIClient, logger *slog.Logger) *History {
	if logger == nil {
		logger = slog.Default()
	}
	return &History{q: q, creds: creds, client: client, logger: logger}
}

// larkTarget is the resolved per-session read context: the installation's
// transport credentials plus the session's pinned chat, its own topic root, and
// the bot's identity (used to mark the bot's own turns as assistant).
type larkTarget struct {
	creds        InstallationCredentials
	chatID       string
	threadRoot   string // the session's current topic (empty when not in one)
	ownAppID     string
	ownBotOpenID string
}

// resolve maps a chat_session to its Lark chat + transport credentials. The chat
// is server-derived here and never accepted from the caller — that is the
// security boundary for `multica chat thread <id>` (the agent supplies only a
// within-chat topic locator).
func (h *History) resolve(ctx context.Context, chatSessionID pgtype.UUID) (larkTarget, error) {
	binding, err := h.q.GetLarkChatSessionBindingBySession(ctx, chatSessionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return larkTarget{}, channel.ErrNoChannelSession
		}
		return larkTarget{}, fmt.Errorf("lookup feishu chat binding: %w", err)
	}
	inst, err := h.q.GetLarkInstallation(ctx, binding.InstallationID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return larkTarget{}, channel.ErrNoChannelSession
		}
		return larkTarget{}, fmt.Errorf("load feishu installation: %w", err)
	}
	if inst.Status != string(InstallationActive) {
		return larkTarget{}, channel.ErrNoChannelSession // revoked install: nothing to read
	}
	if h.creds == nil {
		return larkTarget{}, errors.New("lark history: credentials resolver missing")
	}
	secret, err := h.creds.DecryptAppSecret(inst)
	if err != nil {
		return larkTarget{}, fmt.Errorf("decrypt app_secret: %w", err)
	}
	creds := InstallationCredentials{
		AppID:     inst.AppID,
		AppSecret: secret,
		Region:    RegionOrDefault(inst.Region),
	}
	if inst.TenantKey.Valid {
		creds.TenantKey = inst.TenantKey.String
	}
	t := larkTarget{
		creds:        creds,
		chatID:       binding.ChannelChatID,
		ownAppID:     inst.AppID,
		ownBotOpenID: inst.BotOpenID,
	}
	if binding.LastThreadID.Valid {
		t.threadRoot = binding.LastThreadID.String
	}
	return t, nil
}

// ChannelOverview returns the chat's recent messages (oldest-first). A message
// that belongs to a topic is tagged with its thread_id so the agent can drill
// in with `multica chat thread <thread_id>`. Lark's list endpoint carries no
// per-topic reply count, so ReplyCount is left unset. Backs `multica chat
// history`.
func (h *History) ChannelOverview(ctx context.Context, chatSessionID pgtype.UUID, opts channel.HistoryOptions) (channel.HistoryPage, error) {
	t, err := h.resolve(ctx, chatSessionID)
	if err != nil {
		return channel.HistoryPage{}, err
	}
	res, err := h.client.ListContainerMessages(ctx, t.creds, ListContainerParams{
		ContainerType: larkContainerTypeChat,
		ContainerID:   t.chatID,
		PageSize:      clampHistoryLimit(opts.Limit),
		PageToken:     opts.Before,
	})
	if err != nil {
		return channel.HistoryPage{}, readError("read feishu chat", err)
	}
	page := h.normalizePage(ctx, t, res.Messages, true)
	page.ChannelType = string(channel.TypeFeishu)
	page.NextCursor = res.PageToken
	return page, nil
}

// Thread returns one topic's messages (oldest-first). threadID empty reads the
// topic the session is in (the agent's own topic); a non-empty id reads that
// specific topic — but always within the session's pinned chat. A chat with no
// topic (or an unrecoverable root) falls back to the chat's linear recent
// messages. Backs `multica chat thread [id]`.
func (h *History) Thread(ctx context.Context, chatSessionID pgtype.UUID, threadID string, opts channel.HistoryOptions) (channel.HistoryPage, error) {
	t, err := h.resolve(ctx, chatSessionID)
	if err != nil {
		return channel.HistoryPage{}, err
	}
	limit := clampHistoryLimit(opts.Limit)
	ts := threadID
	if ts == "" {
		ts = t.threadRoot // the session's own topic
	}

	params := ListContainerParams{PageSize: limit, PageToken: opts.Before}
	if ts == "" {
		// No topic to read (a chat without topics, or a root we could not
		// recover): fall back to the chat's linear conversation.
		params.ContainerType = larkContainerTypeChat
		params.ContainerID = t.chatID
	} else {
		params.ContainerType = larkContainerTypeThread
		params.ContainerID = ts
	}
	res, err := h.client.ListContainerMessages(ctx, t.creds, params)
	if err != nil {
		return channel.HistoryPage{}, readError("read feishu thread", err)
	}
	page := h.normalizePage(ctx, t, res.Messages, false)
	page.ChannelType = string(channel.TypeFeishu)
	page.ThreadID = ts
	page.NextCursor = res.PageToken
	return page, nil
}

// readError classifies a Lark list-messages failure. A missing-scope error
// (e.g. im:message.group_msg for group history) is permanent and actionable, so
// it becomes a channel.HistoryUnavailableError — the handler answers it as a
// 200 + note so the agent proceeds without history and reports the real,
// fixable cause instead of retrying a "transient" 5xx forever (MUL-4166). Every
// other failure (transport, 5xx, unexpected code) stays a plain error → 502.
func readError(op string, err error) error {
	if IsMissingPermission(err) {
		hint := "read this chat's messages"
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.Msg != "" {
			hint = apiErr.Msg // Lark's message names the exact scope, e.g. "need scope: im:message.group_msg"
		}
		return &channel.HistoryUnavailableError{Reason: fmt.Sprintf(
			"Lark channel history is unavailable: the connected Feishu app is missing a required permission (%s). "+
				"Grant that scope in the Feishu app's permissions, then re-publish and re-authorize the app. "+
				"For now, answer using the current message without channel history.", hint)}
	}
	return fmt.Errorf("%s: %w", op, err)
}

func clampHistoryLimit(n int) int {
	if n <= 0 {
		return defaultHistoryLimit
	}
	if n > maxHistoryLimit {
		return maxHistoryLimit
	}
	return n
}

// normalizePage turns raw Lark messages into a normalized, oldest-first page: it
// sorts by create time, batch-resolves display names, labels senders, maps
// roles (the bot's own app messages are assistant turns), and flattens bodies.
// When overview is true, a message that belongs to a topic is tagged with its
// thread_id so the agent can drill in with `multica chat thread <id>`. The page
// cursor is set by the caller from Lark's page_token, not derived here.
func (h *History) normalizePage(ctx context.Context, t larkTarget, raw []LarkMessage, overview bool) channel.HistoryPage {
	sorted := make([]LarkMessage, len(raw))
	copy(sorted, raw)
	sort.SliceStable(sorted, func(i, j int) bool {
		return parseLarkMillis(sorted[i].CreateTime) < parseLarkMillis(sorted[j].CreateTime)
	})

	names := h.resolveNames(ctx, t.creds, sorted)
	labeler := newSpeakerLabeler(names)

	out := make([]channel.HistoryMessage, 0, len(sorted))
	for i := range sorted {
		m := sorted[i]
		text := flattenLarkMessage(m)
		if text == "" {
			continue // unrenderable / unknown-type marker: no readable body
		}
		own := isOwnBotMessage(m, t.ownAppID, t.ownBotOpenID)
		role := channel.HistoryRoleUser
		if own {
			role = channel.HistoryRoleAssistant
		}
		hm := channel.HistoryMessage{
			ID:       m.MessageID,
			Author:   labeler.label(m),
			AuthorID: m.SenderID,
			Role:     role,
			Text:     text,
			TS:       m.CreateTime,
		}
		// The chat overview tags topic messages so the agent can open them.
		// Lark's list carries no reply count, so ReplyCount stays unset.
		if overview && m.ThreadID != "" {
			hm.ThreadID = m.ThreadID
		}
		out = append(out, hm)
	}
	return channel.HistoryPage{Messages: out}
}

// isOwnBotMessage reports whether a message was sent by THIS installation's bot
// (an assistant turn). Lark records a bot send as an app sender whose id is the
// app_id; some paths surface the bot's open_id instead, so both are accepted.
func isOwnBotMessage(m LarkMessage, ownAppID, ownBotOpenID string) bool {
	if m.SenderType != "app" || m.SenderID == "" {
		return false
	}
	return (ownAppID != "" && m.SenderID == ownAppID) ||
		(ownBotOpenID != "" && m.SenderID == ownBotOpenID)
}

// resolveNames batch-resolves human senders' display names, best-effort. A
// failure (restricted contact scope, transport error) yields a nil map so the
// labeler falls back to positional "User N" rather than blocking the read.
func (h *History) resolveNames(ctx context.Context, creds InstallationCredentials, msgs []LarkMessage) map[string]string {
	ids := senderOpenIDs(msgs)
	if len(ids) == 0 {
		return nil
	}
	names, err := h.client.BatchGetUsers(ctx, creds, ids)
	if err != nil {
		h.logger.WarnContext(ctx, "lark history: user name resolution failed", "ids", len(ids), "error", err)
		return nil
	}
	return names
}

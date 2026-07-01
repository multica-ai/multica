package slack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/slack-go/slack"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ErrNoSlackSession reports that the chat session has no Slack channel binding —
// it is a Feishu or web-only session. Callers surface it as an empty (not
// failed) read so the unified `multica chat history` / `multica chat thread`
// commands answer gracefully on a non-Slack conversation.
var ErrNoSlackSession = errors.New("slack: session has no slack channel binding")

const (
	// defaultHistoryLimit is the page size used when the caller asks for none.
	defaultHistoryLimit = 20
	// maxHistoryLimit caps a single page so a pull can't dump an unbounded
	// transcript into the agent's context.
	maxHistoryLimit = 50
)

// historyQueries is the slice of generated queries the reader needs.
type historyQueries interface {
	GetChannelChatSessionBindingBySession(ctx context.Context, arg db.GetChannelChatSessionBindingBySessionParams) (db.ChannelChatSessionBinding, error)
	GetChannelInstallation(ctx context.Context, arg db.GetChannelInstallationParams) (db.ChannelInstallation, error)
}

// historyClient is the slice of the slack-go Web API the reader calls. The real
// *slack.Client satisfies it; tests inject a fake.
type historyClient interface {
	GetConversationHistoryContext(ctx context.Context, params *slack.GetConversationHistoryParameters) (*slack.GetConversationHistoryResponse, error)
	GetConversationRepliesContext(ctx context.Context, params *slack.GetConversationRepliesParameters) ([]slack.Message, bool, string, error)
	GetUsersInfoContext(ctx context.Context, users ...string) (*[]slack.User, error)
}

// History reads a Slack conversation on demand — the pull side of the unified
// `multica chat history` (channel overview) and `multica chat thread [id]`
// (one thread) commands (MUL-3871). Both are scoped to the session's OWN
// channel: the channel is resolved server-side from the binding and never taken
// from the agent, so a thread id is only a within-channel locator. Sessions with
// no Slack binding return ErrNoSlackSession.
type History struct {
	q         historyQueries
	decrypt   Decrypter
	logger    *slog.Logger
	newClient func(botToken string) historyClient
}

// NewHistory builds the reader over the generated queries and the bot-token
// decrypter (box.Open at wiring time).
func NewHistory(q historyQueries, decrypt Decrypter, logger *slog.Logger) *History {
	if logger == nil {
		logger = slog.Default()
	}
	h := &History{q: q, decrypt: decrypt, logger: logger}
	h.newClient = func(botToken string) historyClient {
		// Only the bot token is needed to read; the app-level token is for the
		// inbound Socket Mode connection (slack_channel.go).
		return slack.New(botToken)
	}
	return h
}

// slackTarget is the resolved per-session read context: a bot-token client plus
// the session's pinned channel and its own thread root.
type slackTarget struct {
	client     historyClient
	channelID  string
	threadRoot string // the session's own thread (empty for a DM)
	botUserID  string
}

// resolve maps a chat_session to its Slack channel + bot client. The channel is
// server-derived here and never accepted from the caller — that is the security
// boundary for `multica chat thread <id>` (the agent supplies only a
// within-channel thread locator).
func (h *History) resolve(ctx context.Context, chatSessionID pgtype.UUID) (slackTarget, error) {
	binding, err := h.q.GetChannelChatSessionBindingBySession(ctx, db.GetChannelChatSessionBindingBySessionParams{
		ChatSessionID: chatSessionID,
		ChannelType:   string(TypeSlack),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return slackTarget{}, ErrNoSlackSession
		}
		return slackTarget{}, fmt.Errorf("lookup slack chat binding: %w", err)
	}
	inst, err := h.q.GetChannelInstallation(ctx, db.GetChannelInstallationParams{
		ID:          binding.InstallationID,
		ChannelType: string(TypeSlack),
	})
	if err != nil {
		return slackTarget{}, fmt.Errorf("load slack installation: %w", err)
	}
	if inst.Status != "active" {
		return slackTarget{}, ErrNoSlackSession // revoked install: nothing to read
	}
	creds, err := decodeCredentials(inst.Config, h.decrypt)
	if err != nil {
		return slackTarget{}, fmt.Errorf("decode slack credentials: %w", err)
	}
	channelID, threadRoot := historyTarget(binding)
	return slackTarget{
		client:     h.newClient(creds.BotToken),
		channelID:  channelID,
		threadRoot: threadRoot,
		botUserID:  creds.BotUserID,
	}, nil
}

// ChannelOverview returns the channel's recent top-level messages (oldest-first),
// each thread tagged with its id + reply count. It does NOT expand thread
// contents — it is the table of contents the agent reads to find a thread, then
// drills into with `multica chat thread <id>`. Backs `multica chat history`.
func (h *History) ChannelOverview(ctx context.Context, chatSessionID pgtype.UUID, opts channel.HistoryOptions) (channel.HistoryPage, error) {
	t, err := h.resolve(ctx, chatSessionID)
	if err != nil {
		return channel.HistoryPage{}, err
	}
	limit := clampHistoryLimit(opts.Limit)
	resp, err := t.client.GetConversationHistoryContext(ctx, &slack.GetConversationHistoryParameters{
		ChannelID: t.channelID,
		Latest:    opts.Before,
		Inclusive: false,
		Limit:     limit,
	})
	if err != nil {
		return channel.HistoryPage{}, fmt.Errorf("read slack channel: %w", err)
	}
	page := normalizePage(ctx, t.client, h.logger, resp.Messages, t.botUserID, limit, true)
	page.ChannelType = string(TypeSlack)
	return page, nil
}

// Thread returns one thread's messages (oldest-first). threadID empty reads the
// thread the session is in (the agent's own thread); a non-empty id reads that
// specific thread — but always within the session's pinned channel. A DM (no
// threads) reads its linear conversation. Backs `multica chat thread [id]`.
func (h *History) Thread(ctx context.Context, chatSessionID pgtype.UUID, threadID string, opts channel.HistoryOptions) (channel.HistoryPage, error) {
	t, err := h.resolve(ctx, chatSessionID)
	if err != nil {
		return channel.HistoryPage{}, err
	}
	limit := clampHistoryLimit(opts.Limit)
	ts := threadID
	if ts == "" {
		ts = t.threadRoot // the session's own thread
	}

	var raw []slack.Message
	if ts == "" {
		// No thread to read (a DM, or a group whose root could not be recovered):
		// fall back to the channel's linear conversation.
		resp, herr := t.client.GetConversationHistoryContext(ctx, &slack.GetConversationHistoryParameters{
			ChannelID: t.channelID,
			Latest:    opts.Before,
			Inclusive: false,
			Limit:     limit,
		})
		if herr != nil {
			return channel.HistoryPage{}, fmt.Errorf("read slack thread: %w", herr)
		}
		raw = resp.Messages
	} else {
		msgs, _, _, rerr := t.client.GetConversationRepliesContext(ctx, &slack.GetConversationRepliesParameters{
			ChannelID: t.channelID,
			Timestamp: ts,
			Latest:    opts.Before,
			Inclusive: false,
			Limit:     limit,
		})
		if rerr != nil {
			return channel.HistoryPage{}, fmt.Errorf("read slack thread: %w", rerr)
		}
		raw = msgs
	}
	page := normalizePage(ctx, t.client, h.logger, raw, t.botUserID, limit, false)
	page.ChannelType = string(TypeSlack)
	page.ThreadID = ts
	return page, nil
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

// historyTarget recovers the real channel id and the session's own thread root
// from the binding. The channel_chat_id may be a composite "channel:threadRoot"
// isolation key, so the real channel id is read from the binding config
// (slackBindingConfig). The thread root is the recorded reply thread
// (last_thread_id), falling back to the composite-key suffix; empty for a DM.
func historyTarget(b db.ChannelChatSessionBinding) (channelID, threadRoot string) {
	channelID = b.ChannelChatID
	if len(b.Config) > 0 {
		var cfg slackBindingConfig
		if err := json.Unmarshal(b.Config, &cfg); err == nil && cfg.ChannelID != "" {
			channelID = cfg.ChannelID
		}
	}
	if b.LastThreadID.Valid && b.LastThreadID.String != "" {
		threadRoot = b.LastThreadID.String
	} else if i := strings.IndexByte(b.ChannelChatID, ':'); i >= 0 {
		threadRoot = b.ChannelChatID[i+1:]
	}
	return channelID, threadRoot
}

// normalizePage turns raw Slack messages into a normalized, oldest-first page:
// it resolves display names in one batch, labels senders, maps roles, and
// computes the back-paging cursor. When overview is true, a message that heads a
// thread (reply_count > 0) is tagged with its thread id + reply count so the
// agent can drill in with `multica chat thread <id>`.
func normalizePage(ctx context.Context, client historyClient, logger *slog.Logger, raw []slack.Message, botUserID string, limit int, overview bool) channel.HistoryPage {
	sort.SliceStable(raw, func(i, j int) bool { return slackTSLess(raw[i].Timestamp, raw[j].Timestamp) })

	names := resolveUserNames(ctx, client, logger, raw, botUserID)
	labeler := newHistoryLabeler(names)

	out := make([]channel.HistoryMessage, 0, len(raw))
	for i := range raw {
		m := raw[i]
		if m.Text == "" {
			continue // join/system/edit markers carry no readable body
		}
		own := m.User != "" && m.User == botUserID
		role := channel.HistoryRoleUser
		if own {
			role = channel.HistoryRoleAssistant
		}
		hm := channel.HistoryMessage{
			ID:       m.Timestamp,
			Author:   labeler.label(m, own),
			AuthorID: m.User,
			Role:     role,
			Text:     m.Text,
			TS:       m.Timestamp,
		}
		if overview && m.ReplyCount > 0 {
			hm.ThreadID = m.Timestamp
			hm.ReplyCount = m.ReplyCount
			hm.LatestReply = m.LatestReply
		}
		out = append(out, hm)
	}

	page := channel.HistoryPage{Messages: out}
	// Advertise a cursor only when the platform returned a full page (more may
	// exist older than the oldest message we just returned).
	if len(raw) >= limit && len(out) > 0 {
		page.NextCursor = out[0].TS
	}
	return page
}

// resolveUserNames batch-resolves human senders' display names, best-effort. A
// failure (missing users:read scope, transport error) yields a nil map so the
// labeler falls back to positional "User N" rather than blocking the read.
func resolveUserNames(ctx context.Context, client historyClient, logger *slog.Logger, msgs []slack.Message, botUserID string) map[string]string {
	seen := make(map[string]bool)
	ids := make([]string, 0, len(msgs))
	for i := range msgs {
		u := msgs[i].User
		if u == "" || u == botUserID || seen[u] {
			continue
		}
		seen[u] = true
		ids = append(ids, u)
	}
	if len(ids) == 0 {
		return nil
	}
	users, err := client.GetUsersInfoContext(ctx, ids...)
	if err != nil || users == nil {
		if err != nil {
			logger.WarnContext(ctx, "slack history: user name resolution failed", "ids", len(ids), "error", err)
		}
		return nil
	}
	names := make(map[string]string, len(*users))
	for _, u := range *users {
		if name := slackDisplayName(u); name != "" {
			names[u.ID] = name
		}
	}
	return names
}

// slackDisplayName picks the friendliest available name for a Slack user.
func slackDisplayName(u slack.User) string {
	switch {
	case u.Profile.DisplayName != "":
		return u.Profile.DisplayName
	case u.RealName != "":
		return u.RealName
	default:
		return u.Name
	}
}

// historyLabeler assigns stable, human-readable labels within one page: this bot
// is "Bot"; a resolved human gets their real name; an unresolved human falls
// back to positional "User N"; a third-party bot uses its posted username.
type historyLabeler struct {
	names map[string]string
	seen  map[string]string
	n     int
}

func newHistoryLabeler(names map[string]string) *historyLabeler {
	return &historyLabeler{names: names, seen: make(map[string]string)}
}

func (l *historyLabeler) label(m slack.Message, own bool) string {
	if own {
		return "Bot"
	}
	key := m.User
	if key == "" {
		if m.Username != "" {
			return m.Username
		}
		key = "bot:" + m.BotID
	}
	if lbl, ok := l.seen[key]; ok {
		return lbl
	}
	var lbl string
	if name := l.names[m.User]; name != "" {
		lbl = name
	} else if m.Username != "" {
		lbl = m.Username
	} else {
		l.n++
		lbl = fmt.Sprintf("User %d", l.n)
	}
	l.seen[key] = lbl
	return lbl
}

// slackTSLess orders two Slack timestamps ("secs.micros") chronologically.
func slackTSLess(a, b string) bool {
	return parseSlackTS(a) < parseSlackTS(b)
}

func parseSlackTS(ts string) float64 {
	f, _ := strconv.ParseFloat(ts, 64)
	return f
}

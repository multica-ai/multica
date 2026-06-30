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
// failed) history read so the unified `multica chat history` command answers
// gracefully on a non-Slack conversation.
var ErrNoSlackSession = errors.New("slack: session has no slack channel binding")

const (
	// defaultHistoryLimit is the page size used when the caller asks for none.
	defaultHistoryLimit = 20
	// maxHistoryLimit caps a single page. Slack's own conversations.* limit is
	// far higher; we self-cap so a pull can't dump an unbounded transcript into
	// the agent's context (mirrors the Feishu recent-context clamp).
	maxHistoryLimit = 50
)

// historyQueries is the slice of generated queries the history reader needs.
// *db.Queries satisfies it. It mirrors outboundQueries: resolve the session's
// Slack binding, then load the installation that owns the bot token.
type historyQueries interface {
	GetChannelChatSessionBindingBySession(ctx context.Context, arg db.GetChannelChatSessionBindingBySessionParams) (db.ChannelChatSessionBinding, error)
	GetChannelInstallation(ctx context.Context, arg db.GetChannelInstallationParams) (db.ChannelInstallation, error)
}

// historyClient is the slice of the slack-go Web API the reader calls. The real
// *slack.Client satisfies it; tests inject a fake so the fetch/labeling logic is
// exercised without a live Slack.
type historyClient interface {
	GetConversationHistoryContext(ctx context.Context, params *slack.GetConversationHistoryParameters) (*slack.GetConversationHistoryResponse, error)
	GetConversationRepliesContext(ctx context.Context, params *slack.GetConversationRepliesParameters) ([]slack.Message, bool, string, error)
	GetUsersInfoContext(ctx context.Context, users ...string) (*[]slack.User, error)
}

// History reads a Slack conversation's prior messages on demand — the pull half
// of the unified `multica chat history` tool (MUL-3871). It mirrors Outbound:
// given a chat_session it finds the Slack binding, decrypts the installation's
// bot token, and calls conversations.replies (a real thread) or
// conversations.history (DM / top-level channel context). Sessions with no
// Slack binding return ErrNoSlackSession, so it coexists with Feishu sessions on
// the shared endpoint.
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
		// Only the bot token is needed to read history; the app-level token is
		// for the inbound Socket Mode connection (slack_channel.go).
		return slack.New(botToken)
	}
	return h
}

// Fetch returns one normalized, oldest-first page of the session's Slack
// conversation. It returns ErrNoSlackSession when the session is not Slack-bound
// or its installation is inactive.
func (h *History) Fetch(ctx context.Context, chatSessionID pgtype.UUID, opts channel.HistoryOptions) (channel.HistoryPage, error) {
	binding, err := h.q.GetChannelChatSessionBindingBySession(ctx, db.GetChannelChatSessionBindingBySessionParams{
		ChatSessionID: chatSessionID,
		ChannelType:   string(TypeSlack),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return channel.HistoryPage{}, ErrNoSlackSession
		}
		return channel.HistoryPage{}, fmt.Errorf("lookup slack chat binding: %w", err)
	}
	inst, err := h.q.GetChannelInstallation(ctx, db.GetChannelInstallationParams{
		ID:          binding.InstallationID,
		ChannelType: string(TypeSlack),
	})
	if err != nil {
		return channel.HistoryPage{}, fmt.Errorf("load slack installation: %w", err)
	}
	if inst.Status != "active" {
		return channel.HistoryPage{}, ErrNoSlackSession // revoked install: nothing to read
	}
	creds, err := decodeCredentials(inst.Config, h.decrypt)
	if err != nil {
		return channel.HistoryPage{}, fmt.Errorf("decode slack credentials: %w", err)
	}
	channelID, threadRoot := historyTarget(binding)
	// Resolve the concrete scope to read. The handler resolves "auto" to
	// thread/channel (it knows first-turn vs follow-up); here we additionally
	// degrade "thread" to "channel" when there is no thread to read — a DM, or a
	// group whose root could not be recovered.
	scope := channel.HistoryScopeChannel
	if opts.Scope == channel.HistoryScopeThread &&
		binding.ChatType == string(channel.ChatTypeGroup) && threadRoot != "" {
		scope = channel.HistoryScopeThread
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = defaultHistoryLimit
	}
	if limit > maxHistoryLimit {
		limit = maxHistoryLimit
	}

	fetchThreadTS := ""
	if scope == channel.HistoryScopeThread {
		fetchThreadTS = threadRoot
	}
	client := h.newClient(creds.BotToken)
	raw, err := fetchRaw(ctx, client, channelID, fetchThreadTS, opts.Before, limit)
	if err != nil {
		return channel.HistoryPage{}, fmt.Errorf("read slack history: %w", err)
	}

	page := normalizeHistory(ctx, client, h.logger, raw, creds.BotUserID, limit)
	page.ChannelType = string(TypeSlack)
	page.Scope = scope
	return page, nil
}

// historyTarget recovers the real channel id and the thread root from the
// binding. The channel_chat_id may be a composite "channel:threadRoot"
// isolation key, so the real channel id is read from the binding config
// (slackBindingConfig). The thread root — present for every engaged group
// session, since the bot's first reply opens a thread on the @mention — is the
// recorded reply thread (last_thread_id), falling back to the composite-key
// suffix. It is empty for a DM (no threads).
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

// fetchRaw pulls the most recent `limit` messages older than `before` (exclusive
// when set). A thread read uses conversations.replies anchored on the thread
// root; a channel read uses conversations.history. Both return newest-first;
// ordering is normalized downstream.
func fetchRaw(ctx context.Context, client historyClient, channelID, threadTS, before string, limit int) ([]slack.Message, error) {
	if threadTS != "" {
		msgs, _, _, err := client.GetConversationRepliesContext(ctx, &slack.GetConversationRepliesParameters{
			ChannelID: channelID,
			Timestamp: threadTS,
			Latest:    before,
			Inclusive: false,
			Limit:     limit,
		})
		return msgs, err
	}
	resp, err := client.GetConversationHistoryContext(ctx, &slack.GetConversationHistoryParameters{
		ChannelID: channelID,
		Latest:    before,
		Inclusive: false,
		Limit:     limit,
	})
	if err != nil {
		return nil, err
	}
	return resp.Messages, nil
}

// normalizeHistory turns raw Slack messages into a normalized, oldest-first
// page: it resolves human display names in one batch, labels each sender, maps
// the role, and computes the back-paging cursor.
func normalizeHistory(ctx context.Context, client historyClient, logger *slog.Logger, raw []slack.Message, botUserID string, limit int) channel.HistoryPage {
	// Oldest-first so the transcript reads top-to-bottom like the chat does.
	sort.SliceStable(raw, func(i, j int) bool { return slackTSLess(raw[i].Timestamp, raw[j].Timestamp) })

	names := resolveUserNames(ctx, client, logger, raw, botUserID)
	labeler := newHistoryLabeler(names)

	out := make([]channel.HistoryMessage, 0, len(raw))
	for i := range raw {
		m := raw[i]
		text := m.Text
		if text == "" {
			continue // join/system/edit markers carry no readable body
		}
		own := m.User != "" && m.User == botUserID
		role := channel.HistoryRoleUser
		if own {
			role = channel.HistoryRoleAssistant
		}
		out = append(out, channel.HistoryMessage{
			ID:       m.Timestamp,
			Author:   labeler.label(m, own),
			AuthorID: m.User,
			Role:     role,
			Text:     text,
			TS:       m.Timestamp,
		})
	}

	page := channel.HistoryPage{Messages: out}
	// Only advertise a cursor when the platform returned a full page (more may
	// exist older than the oldest message we just returned).
	if len(raw) >= limit && len(out) > 0 {
		page.NextCursor = out[0].TS
	}
	return page
}

// resolveUserNames batch-resolves the human senders' display names, best-effort.
// A failure (missing users:read scope, transport error) yields a nil map so the
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

// historyLabeler assigns stable, human-readable labels within one page, mirroring
// the Feishu speakerLabeler: this bot is "Bot"; a resolved human gets their real
// name; an unresolved human falls back to positional "User N"; a third-party bot
// uses its posted username.
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
		// A third-party bot (alerting app, …) posts with a bot_id and often a
		// username but no user id; label it by that username when present.
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

// slackTSLess orders two Slack timestamps ("secs.micros") chronologically. Slack
// ts strings are not safely comparable lexicographically across widths, so parse
// them; an unparseable value sorts as 0 (oldest).
func slackTSLess(a, b string) bool {
	return parseSlackTS(a) < parseSlackTS(b)
}

func parseSlackTS(ts string) float64 {
	f, _ := strconv.ParseFloat(ts, 64)
	return f
}

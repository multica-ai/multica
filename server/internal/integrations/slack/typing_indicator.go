package slack

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/slack-go/slack"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// typingEmoji is the Slack reaction name used as the "processing" indicator on
// the user's message while the agent is working. Slack has no animated "typing"
// reaction like Feishu's, so we use the universal 👀 ("seen, on it") convention
// — a built-in emoji present in every workspace. Change this one constant to
// swap the indicator. The installed Slack app needs the reactions:write scope
// for the reaction to land; without it the add simply fails and is logged.
const typingEmoji = "eyes"

// typingIndicatorMaxAge bounds how old an inbound message may be before we skip
// the reaction, so a Socket Mode reconnect that replays old events does not
// stamp "processing" badges onto long-finished conversations. Mirrors Feishu.
const typingIndicatorMaxAge = 2 * time.Minute

// reactionAPI is the minimal Slack reaction surface the indicator needs.
// *slack.Client satisfies it directly; tests inject a fake.
type reactionAPI interface {
	AddReactionContext(ctx context.Context, name string, item slack.ItemRef) error
	RemoveReactionContext(ctx context.Context, name string, item slack.ItemRef) error
}

// typingState is the (channel, message ts) pair needed to remove a reaction.
// Slack removes by emoji name + item ref, so there is no reaction id to store.
type typingState struct {
	ChannelID string
	MessageTS string
}

// TypingIndicatorQueries is the narrow DB surface the manager needs to resolve
// an installation's bot token when clearing a reaction. *db.Queries satisfies it
// (the same two reads the outbound reply subscriber uses).
type TypingIndicatorQueries interface {
	GetChannelChatSessionBindingBySession(ctx context.Context, arg db.GetChannelChatSessionBindingBySessionParams) (db.ChannelChatSessionBinding, error)
	GetChannelInstallation(ctx context.Context, arg db.GetChannelInstallationParams) (db.ChannelInstallation, error)
}

// TypingIndicatorManager owns the "processing" reaction lifecycle for inbound
// Slack messages: it adds a 👀 reaction when a message is ingested and removes
// it when the agent's run finishes (EventChatDone) or fails (EventTaskFailed).
//
// It mirrors lark.TypingIndicatorManager: state is held in memory keyed by
// chat_session_id, the bot token is re-resolved from the DB on clear (never held
// in the map between add and clear), and every failure is logged and swallowed —
// the indicator is best-effort and must never block or fail a real reply.
type TypingIndicatorManager struct {
	q       TypingIndicatorQueries
	decrypt Decrypter
	log     *slog.Logger
	newAPI  func(creds credentials) reactionAPI

	mu     sync.RWMutex
	states map[string][]typingState // key = chat_session_id string
}

// NewTypingIndicatorManager builds a manager over the generated queries and the
// bot-token decrypter. The Slack API client is constructed per call from the
// installation's decrypted bot token (xoxb-), exactly like the outbound sender.
func NewTypingIndicatorManager(q TypingIndicatorQueries, decrypt Decrypter, logger *slog.Logger) *TypingIndicatorManager {
	if logger == nil {
		logger = slog.Default()
	}
	return &TypingIndicatorManager{
		q:       q,
		decrypt: decrypt,
		log:     logger,
		newAPI:  func(c credentials) reactionAPI { return slack.New(c.BotToken) },
		states:  make(map[string][]typingState),
	}
}

// Add reacts to the just-ingested message and records the state under the chat
// session. inst is the resolved installation row whose Config blob carries the
// encrypted bot token. It is synchronous — the Router calls it in a detached,
// time-bounded goroutine. Errors are logged and swallowed.
func (m *TypingIndicatorManager) Add(ctx context.Context, inst db.ChannelInstallation, sessionID pgtype.UUID, channelID, messageTS string) {
	if channelID == "" || messageTS == "" {
		return
	}
	if isMessageTooOld(messageTS) {
		m.log.Debug("slack typing indicator: message too old, skipping",
			"chat_session_id", util.UUIDToString(sessionID), "message_ts", messageTS)
		return
	}
	creds, err := decodeCredentials(inst.Config, m.decrypt)
	if err != nil {
		m.log.Warn("slack typing indicator: decode credentials failed",
			"chat_session_id", util.UUIDToString(sessionID), "err", err)
		return
	}
	if err := m.newAPI(creds).AddReactionContext(ctx, typingEmoji, slack.NewRefToMessage(channelID, messageTS)); err != nil {
		m.log.Warn("slack typing indicator: add reaction failed",
			"chat_session_id", util.UUIDToString(sessionID), "message_ts", messageTS, "err", err)
		return
	}
	key := util.UUIDToString(sessionID)
	m.mu.Lock()
	m.states[key] = append(m.states[key], typingState{ChannelID: channelID, MessageTS: messageTS})
	m.mu.Unlock()
}

// Clear removes every tracked reaction for the chat session and drops the state.
// It re-resolves the installation's bot token from the binding so no decrypted
// token is held in memory between add and clear. Individual remove failures are
// logged but do not abort the loop. Best-effort throughout.
func (m *TypingIndicatorManager) Clear(ctx context.Context, sessionID pgtype.UUID) {
	key := util.UUIDToString(sessionID)
	m.mu.Lock()
	states := m.states[key]
	delete(m.states, key)
	m.mu.Unlock()
	if len(states) == 0 {
		return
	}

	binding, err := m.q.GetChannelChatSessionBindingBySession(ctx, db.GetChannelChatSessionBindingBySessionParams{
		ChatSessionID: sessionID,
		ChannelType:   string(TypeSlack),
	})
	if err != nil {
		// A missing binding means the session is not (or no longer) a Slack
		// target; nothing to clear, and not worth a warning.
		if !errors.Is(err, pgx.ErrNoRows) {
			m.log.Warn("slack typing indicator: lookup binding for clear failed",
				"chat_session_id", key, "err", err)
		}
		return
	}
	inst, err := m.q.GetChannelInstallation(ctx, db.GetChannelInstallationParams{
		ID:          binding.InstallationID,
		ChannelType: string(TypeSlack),
	})
	if err != nil {
		m.log.Warn("slack typing indicator: lookup installation for clear failed",
			"chat_session_id", key, "err", err)
		return
	}
	creds, err := decodeCredentials(inst.Config, m.decrypt)
	if err != nil {
		m.log.Warn("slack typing indicator: decode credentials for clear failed",
			"chat_session_id", key, "err", err)
		return
	}

	api := m.newAPI(creds)
	for _, s := range states {
		if err := api.RemoveReactionContext(ctx, typingEmoji, slack.NewRefToMessage(s.ChannelID, s.MessageTS)); err != nil {
			m.log.Warn("slack typing indicator: remove reaction failed",
				"chat_session_id", key, "message_ts", s.MessageTS, "err", err)
		}
	}
}

// Register subscribes the manager to the task-lifecycle events that end a run so
// the reaction is cleared on both success and failure. The outbound reply
// subscriber only handles EventChatDone, so this is the only path that removes
// the reaction when a run fails. Call once at boot against a fresh bus; register
// it before the outbound subscriber so the reaction clears ahead of the reply on
// EventChatDone (bus delivery is synchronous, in subscription order).
func (m *TypingIndicatorManager) Register(bus *events.Bus) {
	bus.Subscribe(protocol.EventChatDone, m.handleEvent)
	bus.Subscribe(protocol.EventTaskFailed, m.handleEvent)
}

func (m *TypingIndicatorManager) handleEvent(e events.Event) {
	sessionID, ok := chatSessionIDFromEvent(e)
	if !ok {
		// Issue / autopilot tasks carry no chat_session — nothing to clear.
		return
	}
	// Bus delivery is synchronous; bound the reaction calls so a stuck Slack
	// HTTP request cannot wedge the publish call site.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	m.Clear(ctx, sessionID)
}

// chatSessionIDFromEvent recovers the chat session id from a task-lifecycle
// event. EventChatDone sets it on the envelope; EventTaskFailed carries it only
// in the broadcast payload map (chat tasks only), so both are checked.
func chatSessionIDFromEvent(e events.Event) (pgtype.UUID, bool) {
	if e.ChatSessionID != "" {
		if id, err := util.ParseUUID(e.ChatSessionID); err == nil && id.Valid {
			return id, true
		}
	}
	if m, ok := e.Payload.(map[string]any); ok {
		if s, _ := m["chat_session_id"].(string); s != "" {
			if id, err := util.ParseUUID(s); err == nil && id.Valid {
				return id, true
			}
		}
	}
	return pgtype.UUID{}, false
}

// isMessageTooOld reports whether a Slack message ts ("<seconds>.<micros>") is
// older than typingIndicatorMaxAge. A malformed or empty ts is treated as fresh
// (not skipped) — we would rather over-react than drop a real message.
func isMessageTooOld(ts string) bool {
	if ts == "" {
		return false
	}
	secs, err := strconv.ParseFloat(ts, 64)
	if err != nil {
		return false
	}
	return time.Since(time.Unix(0, int64(secs*float64(time.Second)))) > typingIndicatorMaxAge
}

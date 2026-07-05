package dingtalk

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// typingIndicatorMaxAge is how old a callback can be before we skip the
// "processing" emotion. This prevents stale reactions when a Stream
// reconnect redelivers un-ACKed events. Mirrors the Lark manager's bound.
const typingIndicatorMaxAge = 2 * time.Minute

// typingQueries is the narrow DB surface Clear needs to recover the
// installation credentials for a chat session. *db.Queries satisfies it.
type typingQueries interface {
	GetChannelChatSessionBindingBySession(ctx context.Context, arg db.GetChannelChatSessionBindingBySessionParams) (db.ChannelChatSessionBinding, error)
	GetChannelInstallation(ctx context.Context, arg db.GetChannelInstallationParams) (db.ChannelInstallation, error)
}

// typingIndicatorState identifies one "processing" emotion to recall.
type typingIndicatorState struct {
	target EmotionTarget
}

// TypingIndicatorManager owns the "processing" emotion lifecycle for
// inbound DingTalk messages: an ingested message gets the 🤔思考中 text
// emotion; when the agent replies (or the run settles without a task)
// the emotion is recalled. Mirrors lark.TypingIndicatorManager, with the
// reaction addressed by (conversation, message) instead of a reaction id.
//
// Safe for concurrent use; missing or stale state is tolerated (clearing
// a session with no tracked emotion is a no-op).
type TypingIndicatorManager struct {
	messenger *RobotMessenger
	decrypt   Decrypter
	q         typingQueries
	log       *slog.Logger

	mu     sync.Mutex
	states map[string][]typingIndicatorState // key = chat_session_id string
}

// NewTypingIndicatorManager constructs the manager. messenger, decrypt
// and q must be non-nil.
func NewTypingIndicatorManager(messenger *RobotMessenger, decrypt Decrypter, q typingQueries, log *slog.Logger) *TypingIndicatorManager {
	if log == nil {
		log = slog.Default()
	}
	return &TypingIndicatorManager{
		messenger: messenger,
		decrypt:   decrypt,
		q:         q,
		log:       log,
		states:    make(map[string][]typingIndicatorState),
	}
}

// Add attaches the "processing" emotion to the message and records the
// state under the chat session. Synchronous — the caller decides whether
// to detach. Errors are logged and swallowed (the indicator is cosmetic).
//
// createAtMs is the callback's epoch-millisecond send time; callbacks
// older than typingIndicatorMaxAge are skipped so redelivered events do
// not surface misleading "processing" badges.
func (m *TypingIndicatorManager) Add(ctx context.Context, inst db.ChannelInstallation, chatSessionID pgtype.UUID, target EmotionTarget, createAtMs int64) {
	if target.OpenConversationID == "" || target.OpenMsgID == "" {
		return
	}
	if createAtMs > 0 && time.Since(time.UnixMilli(createAtMs)) > typingIndicatorMaxAge {
		m.log.Debug("dingtalk typing indicator: message too old, skipping",
			"chat_session_id", util.UUIDToString(chatSessionID), "open_msg_id", target.OpenMsgID)
		return
	}
	creds, err := decodeChannelCredentials(inst.Config, m.decrypt)
	if err != nil {
		m.log.Warn("dingtalk typing indicator: decode credentials failed",
			"chat_session_id", util.UUIDToString(chatSessionID), "err", err)
		return
	}
	if err := m.messenger.AddEmotionReply(ctx, creds, target); err != nil {
		m.log.Warn("dingtalk typing indicator: add emotion failed",
			"chat_session_id", util.UUIDToString(chatSessionID), "open_msg_id", target.OpenMsgID, "err", err)
		return
	}
	key := util.UUIDToString(chatSessionID)
	m.mu.Lock()
	m.states[key] = append(m.states[key], typingIndicatorState{target: target})
	m.mu.Unlock()
}

// Clear recalls every tracked "processing" emotion for the chat session
// and drops the state. Synchronous so the emotion is gone before the
// agent's reply lands. Individual recall failures are logged, not fatal.
func (m *TypingIndicatorManager) Clear(ctx context.Context, chatSessionID pgtype.UUID) {
	key := util.UUIDToString(chatSessionID)
	m.mu.Lock()
	states := m.states[key]
	delete(m.states, key)
	m.mu.Unlock()

	if len(states) == 0 {
		return
	}

	binding, err := m.q.GetChannelChatSessionBindingBySession(ctx, db.GetChannelChatSessionBindingBySessionParams{
		ChatSessionID: chatSessionID,
		ChannelType:   string(TypeDingtalk),
	})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			m.log.Warn("dingtalk typing indicator: binding lookup for clear failed",
				"chat_session_id", key, "err", err)
		}
		return
	}
	inst, err := m.q.GetChannelInstallation(ctx, db.GetChannelInstallationParams{
		ID:          binding.InstallationID,
		ChannelType: string(TypeDingtalk),
	})
	if err != nil {
		m.log.Warn("dingtalk typing indicator: installation lookup for clear failed",
			"chat_session_id", key, "err", err)
		return
	}
	creds, err := decodeChannelCredentials(inst.Config, m.decrypt)
	if err != nil {
		m.log.Warn("dingtalk typing indicator: decode credentials for clear failed",
			"chat_session_id", key, "err", err)
		return
	}
	for _, s := range states {
		if err := m.messenger.RecallEmotionReply(ctx, creds, s.target); err != nil {
			m.log.Warn("dingtalk typing indicator: recall emotion failed",
				"chat_session_id", key, "open_msg_id", s.target.OpenMsgID, "err", err)
		}
	}
}

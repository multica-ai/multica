package slack

import (
	"context"
	"errors"
	"fmt"
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

// typingState is what removing a reaction needs: the (channel, message ts) pair
// Slack addresses the item by — it removes by emoji name + item ref, so there is
// no reaction id to store — plus the installation whose bot token put the
// reaction there. The installation id is recorded at add time because that is
// the last moment it is certainly resolvable: it is reachable from the session's
// channel_chat_session_binding, and a session delete drops that row while the
// cancel it triggers is still on its way to this manager.
// configSnapshot is the installation's encrypted config as it stood when the
// reaction was added, for the one case where the id is no longer enough: a
// runtime teardown deletes the installation inside the same transaction that
// cancels the tasks (handler/runtime.go,
// DeleteChannelInstallationsBySystemRuntimeAgents), so by the time the cancel
// reaches Clear there is no row to resolve.
//
// A FALLBACK, never the primary — a live lookup picks up a credential rotation
// between add and clear and a snapshot cannot, so it is used only when the row
// is gone. It holds the same encrypted blob the database holds; the bot token
// is still decrypted only for the life of the clear.
type typingState struct {
	ChannelID      string
	MessageTS      string
	InstallationID pgtype.UUID
	configSnapshot []byte
}

// TypingIndicatorQueries is the narrow DB surface the manager needs to resolve
// an installation's bot token when clearing a reaction. *db.Queries satisfies it.
type TypingIndicatorQueries interface {
	GetChannelInstallation(ctx context.Context, arg db.GetChannelInstallationParams) (db.ChannelInstallation, error)
}

// TypingIndicatorManager owns the "processing" reaction lifecycle for inbound
// Slack messages: it adds a 👀 reaction when a message is ingested and removes
// it however the agent's run ends — EventChatDone, EventTaskFailed or
// EventTaskCancelled.
//
// It mirrors lark.TypingIndicatorManager: state is held in memory keyed by
// chat_session_id, the bot token is re-resolved from the DB on clear (only the
// installation id is held in the map between add and clear, never the token),
// and every failure is logged and swallowed — the indicator is best-effort and
// must never block or fail a real reply.
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
	m.states[key] = append(m.states[key], typingState{ChannelID: channelID, MessageTS: messageTS, InstallationID: inst.ID, configSnapshot: inst.Config})
	m.mu.Unlock()
}

// Clear removes every tracked reaction for the chat session and drops the state.
// It re-resolves the bot token from the installation each state recorded, so no
// decrypted token is held in memory between add and clear. Individual failures
// are logged but do not abort the loop. Best-effort throughout.
//
// The installation is read straight from the state rather than looked up through
// the session's binding, because a clear can outlive that binding: deleting a
// chat session drops the binding row inside the same transaction that cancels
// the session's tasks, and the task:cancelled events that reach this manager are
// broadcast after that transaction commits. A binding lookup would miss, and
// since the state has already been taken here, there would be nothing left to
// clear from. Installation rows survive the session.
func (m *TypingIndicatorManager) Clear(ctx context.Context, sessionID pgtype.UUID) {
	key := util.UUIDToString(sessionID)
	m.mu.Lock()
	states := m.states[key]
	delete(m.states, key)
	m.mu.Unlock()
	if len(states) == 0 {
		return
	}

	// One session's reactions normally share an installation, so the resolved
	// clients are memoised; a session rebound to another installation mid-run
	// still clears every reaction through the app that added it. A nil entry
	// records an installation that failed to resolve, so it is not retried once
	// per reaction.
	apis := make(map[string]reactionAPI, 1)
	for _, s := range states {
		instKey := util.UUIDToString(s.InstallationID)
		api, resolved := apis[instKey]
		if !resolved {
			var err error
			api, err = m.apiForInstallation(ctx, s.InstallationID, s.configSnapshot)
			if err != nil {
				m.log.Warn("slack typing indicator: resolve installation for clear failed",
					"chat_session_id", key, "installation_id", instKey, "err", err)
			}
			apis[instKey] = api
		}
		if api == nil {
			continue
		}
		if err := api.RemoveReactionContext(ctx, typingEmoji, slack.NewRefToMessage(s.ChannelID, s.MessageTS)); err != nil {
			m.log.Warn("slack typing indicator: remove reaction failed",
				"chat_session_id", key, "message_ts", s.MessageTS, "err", err)
		}
	}
}

// apiForInstallation loads an installation row and turns its encrypted config
// into a reaction client. The decrypted bot token exists only for the life of
// this call.
func (m *TypingIndicatorManager) apiForInstallation(ctx context.Context, id pgtype.UUID, snapshot []byte) (reactionAPI, error) {
	config := snapshot
	inst, err := m.q.GetChannelInstallation(ctx, db.GetChannelInstallationParams{
		ID:          id,
		ChannelType: string(TypeSlack),
	})
	switch {
	case err == nil:
		config = inst.Config
	case errors.Is(err, pgx.ErrNoRows) && len(snapshot) > 0:
		// The row is gone, which on this path means the runtime teardown
		// deleted it in the transaction that cancelled these tasks. The
		// reaction it added is still on the message and the snapshot is the
		// only thing left that can take it off.
	default:
		return nil, fmt.Errorf("lookup installation: %w", err)
	}
	creds, err := decodeCredentials(config, m.decrypt)
	if err != nil {
		return nil, fmt.Errorf("decode credentials: %w", err)
	}
	return m.newAPI(creds), nil
}

// Register subscribes the manager to every task-lifecycle event that ends a run,
// so the reaction comes off however the run finished. The outbound reply
// subscriber only handles EventChatDone, so this is the only path that removes
// the reaction on the other two endings.
//
// EventTaskCancelled has to be here or a cancelled run leaves the 👀 on the
// user's message for good: a cancellation publishes no chat-done and no
// task-failed, so nothing else would ever take the reaction off.
//
// task:cancelled is broadcast once per cancelled row by CancelTask, the queued
// follow-up cancel behind it, the agent- and issue-level bulk cancels, the
// runtime and member revocations, and deleting the chat session. The delete is
// the one that used to publish nothing: BroadcastCancelledTasks resolved each
// task's workspace through its chat_session, the same row its transaction had
// just deleted, and an event with no workspace is dropped before it reaches the
// bus. It now takes the workspace from its caller.
//
// Two holes are left, and neither is a missing subscription.
//
// Archiving an agent cancels its tasks without broadcasting per row, on the
// grounds that the agent:archived event already invalidates every client's task
// list (handler/agent.go, ArchiveAgent). No client-side list refresh takes a
// reaction off a Slack message, so archiving an agent mid-run leaves the 👀 in
// place.
//
// And an ending that arrives while the reaction is still being added clears
// nothing: Add records its state only after the Slack call returns, so Clear
// finds an empty map, and the reaction lands after it with nothing left to take
// it off. The Router adds on a detached goroutine, so a cancelled or very fast
// run gets there first. This predates task:cancelled — chat-done and task-failed
// race the add the same way — and closing it needs a per-session generation the
// add can check when its call returns, which is its own change.
//
// Call once at boot against a fresh bus; register it before the outbound
// subscriber so the reaction clears ahead of the reply on EventChatDone (bus
// delivery is synchronous, in subscription order).
func (m *TypingIndicatorManager) Register(bus *events.Bus) {
	bus.Subscribe(protocol.EventChatDone, m.handleEvent)
	bus.Subscribe(protocol.EventTaskFailed, m.handleEvent)
	bus.Subscribe(protocol.EventTaskCancelled, m.handleEvent)
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
// Every EventTaskCancelled publisher sets both.
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

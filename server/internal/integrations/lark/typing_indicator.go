package lark

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// typingEmoji is the Lark emoji_type used for the "processing" indicator.
// It renders as a small typing-animation badge on the message.
const typingEmoji = "Typing"

// typingIndicatorMaxAge is how old a message can be before we skip the
// typing indicator. This prevents stale reactions when a WebSocket
// reconnect replays old events. Aligned with OpenClaw's 2-minute bound.
const typingIndicatorMaxAge = 2 * time.Minute

// typingCleanupTimeout bounds best-effort cleanup independently of reply delivery.
const typingCleanupTimeout = 2 * time.Second

// TypingIndicatorState is a process-local view of one durable cleanup record.
// It never owns the only copy of a remote reaction's cleanup anchor.
type TypingIndicatorState struct {
	LedgerID              pgtype.UUID
	MessageID, ReactionID string
	ended                 bool // guarded by mu; Reconcile may end an Add before HTTP returns.
}

// TypingIndicatorQueries is the narrow DB surface the manager needs.
type TypingIndicatorQueries interface {
	typingLedgerQueries
	GetLarkInstallation(ctx context.Context, id pgtype.UUID) (Installation, error)
	IsChannelMessageTypingActive(ctx context.Context, arg db.IsChannelMessageTypingActiveParams) (bool, error)
}

// TypingIndicatorManager owns the "processing" reaction lifecycle for
// inbound Lark messages. When a message is successfully ingested it adds
// a Typing reaction; when the run ends — with a reply, a failure or a
// cancellation — it clears the reaction(s) for that chat session.
//
// The manager is safe for concurrent use. It tolerates missing or
// stale state gracefully: adding a reaction to a message that already
// has one simply appends another state entry; clearing a session with
// no tracked state is a no-op.
type TypingIndicatorManager struct {
	client      APIClient
	credentials CredentialsResolver
	queries     TypingIndicatorQueries
	log         *slog.Logger

	mu     sync.RWMutex
	states map[string][]*TypingIndicatorState // key = chat_session_id string
}

// NewTypingIndicatorManager constructs a manager. All dependencies must
// be non-nil; the manager panics on nil client / credentials / queries.
func NewTypingIndicatorManager(client APIClient, credentials CredentialsResolver, queries TypingIndicatorQueries, log *slog.Logger) *TypingIndicatorManager {
	if log == nil {
		log = slog.Default()
	}
	return &TypingIndicatorManager{
		client:      client,
		credentials: credentials,
		queries:     queries,
		log:         log,
		states:      make(map[string][]*TypingIndicatorState),
	}
}

// Add sends a Typing reaction to the given message and records the state
// under the chat session. chatMessageID is the immutable persisted user input,
// not the platform message ID or the latest task in the session. It is synchronous — the caller decides whether
// to run it in a detached goroutine. Errors are logged and swallowed.
//
// createTime is Lark's epoch-millisecond string (InboundMessage.CreateTime).
// Messages older than typingIndicatorMaxAge are silently skipped so that
// WebSocket replays and stale reconnects do not surface misleading "processing"
// badges on long-finished conversations.
func (m *TypingIndicatorManager) Add(ctx context.Context, inst Installation, chatSessionID pgtype.UUID, messageID string, createTime string, chatMessageID pgtype.UUID) {
	if messageID == "" {
		return
	}
	if isMessageTooOld(createTime) {
		m.log.Debug("lark typing indicator: message too old, skipping",
			"chat_session_id", uuidString(chatSessionID),
			"message_id", messageID,
			"create_time", createTime,
		)
		return
	}
	creds, err := m.resolveCredentials(inst)
	if err != nil {
		m.log.Warn("lark typing indicator: failed to resolve credentials",
			"chat_session_id", uuidString(chatSessionID),
			"message_id", messageID,
			"err", err,
		)
		return
	}

	// The external anchor must be committed before HTTP can create a badge.
	// No registration means no Add: terminal workers must never miss an input.
	snapshot, err := json.Marshal(Installation{ID: inst.ID, WorkspaceID: inst.WorkspaceID, AppID: inst.AppID, AppSecretEncrypted: inst.AppSecretEncrypted, TenantKey: inst.TenantKey, Region: inst.Region})
	if err != nil {
		m.log.Warn("lark typing: encode installation snapshot", "err", err)
		return
	}
	registrationCtx, registrationCancel := context.WithTimeout(context.WithoutCancel(ctx), typingCleanupTimeout)
	row, err := m.queries.RegisterChannelTypingReaction(registrationCtx, db.RegisterChannelTypingReactionParams{
		ID: dbid.NewV7(), WorkspaceID: inst.WorkspaceID, ChatSessionID: chatSessionID,
		ChatMessageID: chatMessageID, InstallationID: inst.ID, ChannelMessageID: messageID, InstallationSnapshot: snapshot,
	})
	registrationCancel()
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			m.log.Warn("lark typing: Add skipped; input unavailable or workspace quota occupied", "workspace_id", uuidString(inst.WorkspaceID))
			return
		}
		m.log.Warn("lark typing: register cleanup anchor", "message_id", messageID, "err", err)
		return
	}
	if row.CleanupRequired {
		skipCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), typingCleanupTimeout)
		defer cancel()
		if err := m.queries.SkipChannelTypingReactionAdd(skipCtx, row.ID); err != nil {
			m.log.Warn("lark typing: acknowledge skipped add", "err", err)
		}
		return
	}

	key := uuidString(chatSessionID)
	state := &TypingIndicatorState{LedgerID: row.ID, MessageID: messageID}
	m.mu.Lock()
	m.states[key] = append(m.states[key], state)
	m.mu.Unlock()

	addCtx, addCancel := context.WithTimeout(ctx, typingCleanupTimeout)
	reactionID, err := m.client.AddMessageReaction(addCtx, AddReactionParams{
		InstallationID: creds,
		MessageID:      messageID,
		EmojiType:      typingEmoji,
	})
	addCancel()
	if err != nil {
		m.mu.Lock()
		m.removeState(key, state)
		m.mu.Unlock()
		finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), typingCleanupTimeout)
		defer cancel()
		// A transport failure does not prove the remote side did not add it.
		// Keep the anchor in the durable retry queue, including across restart.
		if _, finishErr := m.queries.FinishChannelTypingReactionAdd(finishCtx, db.FinishChannelTypingReactionAddParams{ID: row.ID, CleanupRequired: true}); finishErr != nil {
			m.log.Warn("lark typing: record uncertain Add", "err", finishErr)
		}
		m.log.Warn("lark typing indicator: add reaction failed",
			"chat_session_id", uuidString(chatSessionID),
			"message_id", messageID,
			"err", err,
		)
		return
	}

	// The terminal event may have been handled by another process while the
	// remote Add was in flight. Re-read this exact persisted input after the
	// remote write so a committed terminal state retracts the late result.
	// A session's latest
	// task/delivery is not sufficient: debounce can seal several inputs and a
	// newer turn may already exist. Missing/deleted inputs are no longer live.
	checkCtx, checkCancel := context.WithTimeout(context.WithoutCancel(ctx), typingCleanupTimeout)
	active, checkErr := m.queries.IsChannelMessageTypingActive(checkCtx, db.IsChannelMessageTypingActiveParams{
		MessageID: chatMessageID, ChatSessionID: chatSessionID,
		WorkspaceID: inst.WorkspaceID, InstallationID: inst.ID,
		ChannelType: channelTypeFeishu,
	})
	checkCancel()
	if checkErr != nil {
		// An unverified processing badge must not survive indefinitely.
		m.log.Warn("lark typing indicator: input lifecycle lookup failed", "message_id", messageID, "err", checkErr)
	}

	m.mu.Lock()
	ended := state.ended || !active || checkErr != nil
	m.mu.Unlock()
	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), typingCleanupTimeout)
	finished, finishErr := m.queries.FinishChannelTypingReactionAdd(finishCtx, db.FinishChannelTypingReactionAddParams{ID: row.ID, ReactionID: reactionID, CleanupRequired: ended})
	finishCancel()
	if finishErr != nil {
		m.log.Warn("lark typing: record Add result; durable unfinished anchor remains", "err", finishErr)
		row.ReactionID = reactionID
		row.CleanupRequired = true
		finished = row
	}
	m.mu.Lock()
	ended = ended || state.ended || finished.CleanupRequired || finishErr != nil
	if ended {
		m.removeState(key, state)
	} else {
		state.ReactionID = reactionID
	}
	m.mu.Unlock()
	if ended {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), typingCleanupTimeout)
		defer cancel()
		m.cleanupReaction(cleanupCtx, finished)
		return
	}

	m.log.Debug("lark typing indicator: reaction added",
		"chat_session_id", key,
		"message_id", messageID,
		"reaction_id", reactionID,
	)
}

// removeState removes only this Add's entry, preserving later inputs. mu must be held.
func (m *TypingIndicatorManager) removeState(key string, state *TypingIndicatorState) {
	for i, pending := range m.states[key] {
		if pending == state {
			m.states[key] = append(m.states[key][:i], m.states[key][i+1:]...)
			if len(m.states[key]) == 0 {
				delete(m.states, key)
			}
			return
		}
	}
}

// SweepMessage removes every Typing reaction the bot itself put on a message,
// found by listing the message's reactions from Lark rather than by consulting
// the in-memory state. This is the authoritative half of the lifecycle: the
// state map only exists in the process that ran Add, so a restart between add
// and clear or a second replica handling completion leaves the map empty
// while the badge is still on screen. In-flight adds are handled separately
// by the durable pending-Add record. The sweep also retries reactions whose
// exact deletion failed during reconciliation.
//
// Both operator type and App ID must match. Other applications' reactions are
// not ours to delete and must not turn an otherwise successful sweep into a retry.
//
// credentials came from the caller, which resolves them from the same
// installation that added the reaction; errors are logged and swallowed — the
// indicator is best-effort and must never fail a reply. A client that does not
// implement ReactionLister (the stub, minimal fakes) skips the sweep.
func (m *TypingIndicatorManager) SweepMessage(ctx context.Context, creds InstallationCredentials, messageID string) {
	if messageID == "" || creds.AppID == "" {
		return
	}
	lister, ok := m.client.(ReactionLister)
	if !ok {
		m.log.Debug("lark typing indicator: client cannot list reactions, skipping sweep",
			"message_id", messageID,
		)
		return
	}
	reactions, err := lister.ListMessageReactions(ctx, ListMessageReactionsParams{
		InstallationID: creds,
		MessageID:      messageID,
		EmojiType:      typingEmoji,
	})
	if err != nil {
		m.log.Warn("lark typing indicator: sweep list reactions failed",
			"message_id", messageID,
			"err", err,
		)
		return
	}
	for _, r := range reactions {
		if !strings.EqualFold(r.EmojiType, typingEmoji) || r.OperatorType != "app" || r.OperatorID != creds.AppID {
			continue
		}
		if err := m.client.DeleteMessageReaction(ctx, DeleteReactionParams{
			InstallationID: creds,
			MessageID:      messageID,
			ReactionID:     r.ReactionID,
		}); err != nil {
			m.log.Warn("lark typing indicator: sweep delete reaction failed",
				"message_id", messageID,
				"reaction_id", r.ReactionID,
				"err", err,
			)
			continue
		}
		m.log.Debug("lark typing indicator: sweep removed reaction",
			"message_id", messageID,
			"reaction_id", r.ReactionID,
		)
	}
}

// credentialsForInstallation loads an installation row and decrypts its app
// secret. Nil means the clear cannot proceed for that installation; the reason
// is already logged. The decrypted secret exists only from here on, never in the
// state map.
func (m *TypingIndicatorManager) credentialsForInstallation(ctx context.Context, sessionKey string, id pgtype.UUID, snapshot Installation) *InstallationCredentials {
	inst, err := m.queries.GetLarkInstallation(ctx, id)
	switch {
	case err == nil:
	case errors.Is(err, pgx.ErrNoRows) && snapshot.ID.Valid:
		// The row is gone, which on this path means the runtime teardown
		// deleted it in the transaction that cancelled these tasks. The
		// reaction it added is still on the message and this is the only thing
		// left that can take it off.
		m.log.Debug("lark typing indicator: installation gone, clearing from the snapshot taken at add time",
			"chat_session_id", sessionKey,
			"installation_id", uuidString(id),
		)
		inst = snapshot
	default:
		m.log.Warn("lark typing indicator: failed to lookup installation for clear",
			"chat_session_id", sessionKey,
			"installation_id", uuidString(id),
			"err", err,
		)
		return nil
	}
	creds, err := m.resolveCredentials(inst)
	if err != nil {
		m.log.Warn("lark typing indicator: failed to resolve credentials for clear",
			"chat_session_id", sessionKey,
			"installation_id", uuidString(id),
			"err", err,
		)
		return nil
	}
	return &creds
}

func isMessageTooOld(createTime string) bool {
	if createTime == "" {
		return false
	}
	ms, err := strconv.ParseInt(createTime, 10, 64)
	if err != nil {
		return false
	}
	return time.Since(time.UnixMilli(ms)) > typingIndicatorMaxAge
}

func (m *TypingIndicatorManager) resolveCredentials(inst Installation) (InstallationCredentials, error) {
	secret, err := m.credentials.DecryptAppSecret(inst)
	if err != nil {
		return InstallationCredentials{}, err
	}
	creds := InstallationCredentials{
		AppID:     inst.AppID,
		AppSecret: secret,
		Region:    RegionOrDefault(inst.Region),
	}
	if inst.TenantKey.Valid {
		creds.TenantKey = inst.TenantKey.String
	}
	return creds, nil
}

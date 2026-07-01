package dingtalk

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util"
)

// The classic robot API we send through (oToMessages/batchSend) exposes no
// per-message reaction, so a long-running agent turn leaves a mobile user
// staring at silence until the reply lands. (DingTalk's AI-assistant stack does
// offer a reaction capability via ackReactionScope, but the v0.9.1 stream SDK
// does not surface it.) The ack notifier stands in for a typing indicator: on
// ingest it posts a lightweight "working on it" message so the user sees their
// message was received.
//
// It implements engine.TypingNotifier. The engine already gates OnIngested to
// plain ingested chat turns (never /issue, never dropped messages), and calls
// OnSettled when a run enqueues no task.

// ackProcessingText is the stand-in "typing" message. Kept short: it is a real,
// non-retractable chat message, not an ephemeral indicator.
const ackProcessingText = "👀 On it — I'll reply here when it's ready."

// ackCoalesceWindow suppresses duplicate acks for the same session. It sits just
// above the run debounce window so a burst of messages that flush into one run
// yields a single ack, while a genuinely later turn re-acks.
const ackCoalesceWindow = 5 * time.Second

// ackNotifier posts the processing ack and coalesces bursts per session.
type ackNotifier struct {
	client  *Client
	decrypt Decrypter
	logger  *slog.Logger
	window  time.Duration
	now     func() time.Time

	mu      sync.Mutex
	lastAck map[string]time.Time

	// sendText delivers text into the installation's conversation. Nil uses the
	// real Open-API send; tests inject a recorder.
	sendText func(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage, text string) error
}

var _ engine.TypingNotifier = (*ackNotifier)(nil)

// NewAckNotifier builds the ack notifier over the shared outbound client and the
// credential decrypter.
func NewAckNotifier(client *Client, decrypt Decrypter, logger *slog.Logger) *ackNotifier {
	if logger == nil {
		logger = slog.Default()
	}
	return &ackNotifier{
		client:  client,
		decrypt: decrypt,
		logger:  logger,
		window:  ackCoalesceWindow,
		lastAck: make(map[string]time.Time),
	}
}

// OnIngested posts the processing ack unless a recent ack for the same session
// is still within the coalesce window.
func (n *ackNotifier) OnIngested(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage, sessionID pgtype.UUID) {
	if n.suppress(sessionID) {
		return
	}
	send := n.sendText
	if send == nil {
		send = n.realSend
	}
	if err := send(ctx, inst, msg, ackProcessingText); err != nil {
		n.logger.WarnContext(ctx, "dingtalk ack: send failed",
			"installation_id", util.UUIDToString(inst.ID), "error", err)
	}
}

// OnSettled clears the session's dedup entry so its next turn acks immediately.
func (n *ackNotifier) OnSettled(_ context.Context, sessionID pgtype.UUID) {
	key := util.UUIDToString(sessionID)
	if key == "" {
		return
	}
	n.mu.Lock()
	delete(n.lastAck, key)
	n.mu.Unlock()
}

// suppress reports whether an ack for sessionID should be skipped, and otherwise
// records this ack. The check-and-set is atomic so concurrent ingests of one
// burst yield a single ack.
func (n *ackNotifier) suppress(sessionID pgtype.UUID) bool {
	key := util.UUIDToString(sessionID)
	if key == "" {
		return false
	}
	now := n.clock()
	n.mu.Lock()
	defer n.mu.Unlock()
	if last, ok := n.lastAck[key]; ok && now.Sub(last) < n.window {
		return true
	}
	// Prune entries past the window before inserting. OnSettled only fires for
	// runs that enqueue no task, so task-spawning sessions would otherwise leak
	// their entry forever. Stale entries are dead (any later turn re-acks), and
	// this runs only on a cache miss, keeping the map bounded by the sessions
	// seen within one window.
	for k, last := range n.lastAck {
		if now.Sub(last) >= n.window {
			delete(n.lastAck, k)
		}
	}
	n.lastAck[key] = now
	return false
}

func (n *ackNotifier) clock() time.Time {
	if n.now != nil {
		return n.now()
	}
	return time.Now()
}

func (n *ackNotifier) realSend(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage, text string) error {
	_, err := sendInstallationText(ctx, n.client, n.decrypt, inst, targetFromMessage(msg), text)
	return err
}

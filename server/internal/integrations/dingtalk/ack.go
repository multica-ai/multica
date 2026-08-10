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
// It implements engine.TypingNotifier. The engine calls OnIngested after an
// accepted turn has been persisted and scheduled for an agent run. Terminal
// commands such as /issue return their synchronous result without posting this
// non-retractable processing acknowledgement.
//
// Because the ack is a promise rather than a badge, the notifier also tracks
// which sessions are still owed one. Outbound consults that when a run is
// cancelled: a promise nobody can retract has to be withdrawn with a second
// message, and only where a promise was actually made.

// ackProcessingText is the stand-in "typing" message. Kept short: it is a real,
// non-retractable chat message, not an ephemeral indicator.
const ackProcessingText = "👀 On it — I'll reply here when it's ready."

// ackCancelledText withdraws the ack above. Since the ack cannot be retracted,
// the only way to close it is a second message saying the reply is not coming.
// Kept in the ack's own register for the same reason the ack is: it lands in the
// user's conversation, not in a status badge.
const ackCancelledText = "⚠️ That run was cancelled — no reply is coming for it."

// ackCoalesceWindow suppresses duplicate acks for the same session. It sits just
// above the run debounce window so a burst of messages that flush into one run
// yields a single ack, while a genuinely later turn re-acks.
const ackCoalesceWindow = 5 * time.Second

// ackPromise is an ack that has been posted and not yet answered. It carries
// what a withdrawal needs to reach the same conversation: the room the ack went
// into and the installation that sent it. The installation is kept as an id, so
// nothing decrypted is held between the ack and its withdrawal — and unlike the
// session's binding row, an installation row survives the session being deleted.
type ackPromise struct {
	installationID pgtype.UUID
	target         sendTarget
}

// ackNotifier posts the processing ack, coalesces bursts per session, and
// remembers which sessions are still owed a reply so a cancelled run can
// withdraw exactly the promises that are outstanding.
type ackNotifier struct {
	client  *Client
	decrypt Decrypter
	logger  *slog.Logger
	window  time.Duration
	now     func() time.Time

	mu      sync.Mutex
	lastAck map[string]time.Time
	// outstanding holds one promise per session that has been acked and not yet
	// answered. Kept apart from lastAck because the two have different lives:
	// lastAck expires with the coalesce window, while a promise stands until the
	// run it acked produces something.
	outstanding map[string]ackPromise

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
		client:      client,
		decrypt:     decrypt,
		logger:      logger,
		window:      ackCoalesceWindow,
		lastAck:     make(map[string]time.Time),
		outstanding: make(map[string]ackPromise),
	}
}

// OnIngested posts the processing ack unless a recent ack for the same session
// is still within the coalesce window, and records the promise it just made.
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
		return
	}
	key := util.UUIDToString(sessionID)
	if key == "" {
		return
	}
	n.mu.Lock()
	n.outstanding[key] = ackPromise{installationID: inst.ID, target: targetFromMessage(msg)}
	n.mu.Unlock()
}

// OnSettled clears the session's dedup entry so its next turn acks immediately,
// and drops any outstanding promise with it.
//
// The engine calls this when the turn produced no run at all, so no task
// lifecycle event will ever arrive to close the promise. Dropping it keeps this
// path posting nothing, which is what it has always done — the offline and
// archived outcomes get their own notice from the replier — and stops an
// unrelated later cancel on the same session from withdrawing a promise that
// belongs to a round already over.
func (n *ackNotifier) OnSettled(_ context.Context, sessionID pgtype.UUID) {
	key := util.UUIDToString(sessionID)
	if key == "" {
		return
	}
	n.mu.Lock()
	delete(n.lastAck, key)
	delete(n.outstanding, key)
	n.mu.Unlock()
}

// takeOutstandingAck removes the session's unanswered promise and reports
// whether there was one. It is the dedupe for a bulk cancel: cancel is broadcast
// once per task row, so a "cancel all tasks" click or a session delete with
// several queued turns delivers several events for one conversation, and only
// the first of them finds a promise to withdraw.
func (n *ackNotifier) takeOutstandingAck(sessionID pgtype.UUID) (ackPromise, bool) {
	key := util.UUIDToString(sessionID)
	if key == "" {
		return ackPromise{}, false
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	promise, ok := n.outstanding[key]
	if ok {
		delete(n.outstanding, key)
	}
	return promise, ok
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

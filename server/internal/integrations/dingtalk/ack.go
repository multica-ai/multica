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
// which conversation each outstanding promise was made in. Outbound uses that
// to address the withdrawal a cancelled run owes.
//
// One conversation can hold more than one promise at a time. The coalesce
// window below only absorbs a burst; a turn sent after it acks again, so a user
// who asks a second question while the first is still working has two "On it"
// messages standing and is owed two endings. So the promises are held as a
// per-session queue, one entry per ack posted, and each ending discharges one.
//
// Note what that costs. Slack and Lark hold their indicator state in memory
// because there is nowhere else to hold it — nothing in the database says which
// message a reaction sits on. Here the room is also in the session's binding
// row, so keeping the promise in memory is a choice, and it buys the two things
// the binding cannot answer: whether an ack was actually posted, and whether
// something has already answered it. The price is that a process restart
// forgets every outstanding promise and the cancel that follows posts nothing.
// That failure direction is the intended one: the notice cannot be unsent, so
// with no record of what was promised, silence is the safe answer.

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

// ackPromiseMaxAge bounds how long an unanswered promise is kept. Nothing
// expires the run it belongs to, so this is not an indicator timeout: it posts
// nothing and the user sees no difference. It exists only so a session whose run
// never reports any ending — a lost event, a daemon that went away mid-run —
// cannot hold a map entry for the life of the process. A day is far longer than
// any run and far shorter than an uptime.
const ackPromiseMaxAge = 24 * time.Hour

// ackPromise is an ack that has been posted and not yet answered. It carries
// what a withdrawal needs to reach the same conversation: the room the ack went
// into and the installation that sent it. The installation is kept as an id, so
// nothing decrypted is held between the ack and its withdrawal — and unlike the
// session's binding row, an installation row survives the session being deleted.
type ackPromise struct {
	installationID pgtype.UUID
	target         sendTarget
	at             time.Time
}

// ackNotifier posts the processing ack, coalesces bursts per session, and
// remembers how many replies each session is still owed, and in which room. It
// does not decide whether a given cancellation should withdraw one — a session
// can carry a run that never acked here — only whether anything is still owed
// and where it would go. Outbound.withdrawProcessingAck decides.
type ackNotifier struct {
	client  *Client
	decrypt Decrypter
	logger  *slog.Logger
	window  time.Duration
	now     func() time.Time

	mu      sync.Mutex
	lastAck map[string]time.Time
	// outstanding holds, per session, every promise that has been acked and not
	// yet answered, oldest first. Kept apart from lastAck because the two have
	// different lives: lastAck expires with the coalesce window, while a promise
	// stands until the run it acked reports an ending.
	//
	// A queue rather than a single slot because a session can be owed two
	// replies at once — a second turn past the coalesce window acks again while
	// the first is still working. A slot let the second ack overwrite the first,
	// so the first round's promise vanished without anything being said in the
	// room, and then the first round's ending discharged the second round's
	// promise. Depth is what turns those two rounds back into two endings.
	//
	// Which entry a discharge takes does not matter much: every promise in one
	// session names the same room, so they differ only in age. Oldest first
	// keeps the ordering the endings arrive in — a session's turns are
	// serialized — and leaves the youngest to be swept last.
	//
	// Both maps need sweeping for the same reason, at different scales. Entries
	// are removed on the paths that answer them — OnSettled for a turn that
	// enqueued no task, releaseOutstandingAck when the run ends, and
	// takeOutstandingAck when a cancel withdraws it — but those paths do not
	// cover everything, and the sweep is what stands behind the ones they miss
	// rather than a list anybody has to keep complete. A run that never reports
	// an ending is the plain case; a run whose ending arrives after something
	// removed the session's binding is the one that has actually bitten. So
	// recording a promise first drops every entry older than ackPromiseMaxAge,
	// which bounds the map by the sessions acked within one day rather than by
	// the process's whole lifetime.
	outstanding map[string][]ackPromise

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
		outstanding: make(map[string][]ackPromise),
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
	now := n.clock()
	n.mu.Lock()
	n.sweepPromisesLocked(now)
	n.outstanding[key] = append(n.outstanding[key],
		ackPromise{installationID: inst.ID, target: targetFromMessage(msg), at: now})
	n.mu.Unlock()
}

// OnSettled clears the session's dedup entry so its next turn acks immediately,
// and discharges one outstanding promise with it.
//
// The engine calls this when the turn produced no run at all, so no task
// lifecycle event will ever arrive to close the promise that turn made.
// Discharging it keeps this path posting nothing, which is what it has always
// done — the offline and archived outcomes get their own notice from the
// replier — and stops an unrelated later cancel on the same session from
// withdrawing a promise that belongs to a round already over.
//
// One, not all of them: a promise made by an earlier round belongs to a run
// that is still going, and taking it here would leave that run's own
// cancellation with nothing to withdraw.
func (n *ackNotifier) OnSettled(_ context.Context, sessionID pgtype.UUID) {
	key := util.UUIDToString(sessionID)
	if key == "" {
		return
	}
	n.mu.Lock()
	delete(n.lastAck, key)
	n.dischargeOldestLocked(key)
	n.mu.Unlock()
}

// hasOutstandingAck reports whether the session is still owed a reply, without
// consuming anything. Outbound checks this before it spends any query on a
// cancel: most cancelled runs are nothing to do with DingTalk.
func (n *ackNotifier) hasOutstandingAck(sessionID pgtype.UUID) bool {
	key := util.UUIDToString(sessionID)
	if key == "" {
		return false
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.outstanding[key]) > 0
}

// takeOutstandingAck discharges the session's oldest unanswered promise and
// returns it, along with how many promises are still standing in that room
// afterwards. ok is false when there was nothing left to take.
//
// remaining is what holds a bulk cancel to one message. Cancel is broadcast
// once per task row, so a "cancel all tasks" click or a session delete with
// several queued turns delivers several events for one conversation; each takes
// its own promise, so the books stay straight, but only the one that empties
// the room's queue says anything. That used to fall out of there being at most
// one promise to take, which stopped being true once the queue could hold two.
//
// The count is read under the same lock as the take, so two cancels arriving
// together cannot both see an empty room and both post.
func (n *ackNotifier) takeOutstandingAck(sessionID pgtype.UUID) (promise ackPromise, remaining int, ok bool) {
	key := util.UUIDToString(sessionID)
	if key == "" {
		return ackPromise{}, 0, false
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	queue := n.outstanding[key]
	if len(queue) == 0 {
		return ackPromise{}, 0, false
	}
	n.setPromisesLocked(key, queue[1:])
	return queue[0], len(queue) - 1, true
}

// releaseOutstandingAck discharges the session's oldest promise without posting
// anything. The caller uses it when an acked run has reported an ending of its
// own: the promise is discharged whether or not a reply could actually be
// delivered, and leaving it on record would let an unrelated later cancel in
// the same conversation withdraw a promise that no longer stands.
func (n *ackNotifier) releaseOutstandingAck(sessionID pgtype.UUID) {
	key := util.UUIDToString(sessionID)
	if key == "" {
		return
	}
	n.mu.Lock()
	n.dischargeOldestLocked(key)
	n.mu.Unlock()
}

// dischargeOldestLocked drops the session's oldest promise, if it has one.
func (n *ackNotifier) dischargeOldestLocked(key string) {
	if queue := n.outstanding[key]; len(queue) > 0 {
		n.setPromisesLocked(key, queue[1:])
	}
}

// setPromisesLocked stores a session's remaining promises, removing the session
// entirely once it is owed nothing. Removing rather than keeping an empty slice
// is what keeps the map bounded by the sessions currently owed a reply.
func (n *ackNotifier) setPromisesLocked(key string, queue []ackPromise) {
	if len(queue) == 0 {
		delete(n.outstanding, key)
		return
	}
	n.outstanding[key] = queue
}

// sweepPromisesLocked drops every promise older than ackPromiseMaxAge. See the
// outstanding field for why this stands behind the paths that discharge
// promises rather than replacing them.
func (n *ackNotifier) sweepPromisesLocked(now time.Time) {
	for key, queue := range n.outstanding {
		kept := queue[:0]
		for _, p := range queue {
			if now.Sub(p.at) < ackPromiseMaxAge {
				kept = append(kept, p)
			}
		}
		n.setPromisesLocked(key, kept)
	}
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

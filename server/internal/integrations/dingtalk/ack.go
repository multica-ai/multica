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
// The queue counts acks, not runs, and those two can drift apart: an ending can
// arrive while the ack it answers is still being sent, and a run can stop
// reporting endings altogether. Nothing in memory can tell a promise whose run
// is still working from one whose run is over, so the queue is not the authority
// on whether a reply is still coming — agent_task_queue is, and
// Outbound.withdrawProcessingAck asks it. See takeAckForIdleRoom.
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

// sessionAcks is everything one conversation currently owes: the promises whose
// "👀 On it" is really in the room, plus the acks still on their way there.
type sessionAcks struct {
	// promises are the acks posted and not yet answered, oldest first.
	promises []ackPromise

	// sending counts the acks whose send call has not returned. A promise is
	// recorded only once its message is actually in the room, so an ending
	// arriving in that gap finds nothing to discharge.
	sending int

	// discharged counts the endings that arrived in that gap. The send that
	// returns spends one of them instead of recording a promise, so a round
	// already over does not leave behind a promise nothing will ever answer.
	// That is the whole point of the counter: without it the promise landed
	// after its own ending and stood in the room's queue until the day-long
	// sweep, one entry ahead of every promise made afterwards.
	//
	// Never exceeds sending; finishSend clamps it when an ack fails to post.
	discharged int
}

// owed reports whether the conversation is still waiting on anything — a posted
// promise, or an ack in flight that no ending has claimed yet.
func (s *sessionAcks) owed() bool {
	return s != nil && (len(s.promises) > 0 || s.discharged < s.sending)
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
	// outstanding holds, per session, everything the conversation is still owed:
	// the promises already posted, oldest first, and the acks still being sent.
	// Kept apart from lastAck because the two have different lives: lastAck
	// expires with the coalesce window, while a promise stands until the run it
	// acked reports an ending.
	//
	// A queue rather than a single slot because a session can be owed two
	// replies at once — a second turn past the coalesce window acks again while
	// the first is still working. A slot let the second ack overwrite the first,
	// so the first round's promise vanished without anything being said in the
	// room, and then the first round's ending discharged the second round's
	// promise. Depth is what turns those two rounds back into two endings.
	//
	// Which entry a discharge takes does not matter: every promise in one
	// session names the same room, so they differ only in age. Oldest first
	// keeps the ordering the endings arrive in — a session's turns are
	// serialized — and leaves the youngest to be dropped last.
	//
	// Growth is bounded at three scales, because the paths that answer a promise
	// do not cover every way a run can end. Each ending removes one entry
	// (OnSettled for a turn that enqueued no task, releaseOutstandingAck when
	// the run ends, takeAckForIdleRoom when a cancel withdraws it). On top of
	// that, takeAckForIdleRoom closes out every other promise the database's
	// answer covered, which is what stops one lost ending from leaving the room
	// permanently a promise heavy. And recording a promise first drops
	// everything older than ackPromiseMaxAge, so a session whose agent stops
	// reporting endings altogether — and that never sees another cancel to
	// reconcile it — is bounded by a day of turns rather than by the process's
	// whole lifetime.
	outstanding map[string]*sessionAcks

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
		outstanding: make(map[string]*sessionAcks),
	}
}

// OnIngested posts the processing ack unless a recent ack for the same session
// is still within the coalesce window, and records the promise it just made.
//
// The send is announced before it starts and recorded after it returns. The
// promise cannot be recorded up front — the room only holds an "On it" once the
// message is really in it, and a cancel that withdrew a promise nobody had been
// made would post "no reply is coming" ahead of the ack it answers. But the run
// this ack belongs to can end while the send is still in the air, and that
// ending has to land somewhere: announcing the send gives it a place to land, so
// the promise is cancelled out as it is recorded instead of standing in the
// room's queue with its round already over.
func (n *ackNotifier) OnIngested(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage, sessionID pgtype.UUID) {
	if n.suppress(sessionID) {
		return
	}
	send := n.sendText
	if send == nil {
		send = n.realSend
	}
	key := util.UUIDToString(sessionID)
	if key == "" {
		// Nothing to key a promise by, so nothing can be discharged either.
		if err := send(ctx, inst, msg, ackProcessingText); err != nil {
			n.logger.WarnContext(ctx, "dingtalk ack: send failed",
				"installation_id", util.UUIDToString(inst.ID), "error", err)
		}
		return
	}
	n.beginSend(key)
	err := send(ctx, inst, msg, ackProcessingText)
	n.finishSend(key, ackPromise{installationID: inst.ID, target: targetFromMessage(msg)}, err == nil)
	if err != nil {
		n.logger.WarnContext(ctx, "dingtalk ack: send failed",
			"installation_id", util.UUIDToString(inst.ID), "error", err)
	}
}

// beginSend announces an ack that is on its way but not yet in the room.
func (n *ackNotifier) beginSend(key string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.sessionLocked(key).sending++
}

// finishSend closes the window beginSend opened. posted says whether the ack
// really reached the room.
//
// An ending that arrived while the send was in the air is spent here rather than
// leaving a promise behind. A send that posted nothing leaves that ending's
// charge for another ack still in flight, and drops it when there is none: it
// promised nothing, so there is nothing for the charge to answer.
func (n *ackNotifier) finishSend(key string, promise ackPromise, posted bool) {
	now := n.clock()
	n.mu.Lock()
	defer n.mu.Unlock()
	// Sweeping before the append is what bounds the map by a day of acked
	// sessions; this session survives it either way, because its send is still
	// counted until the line below.
	n.sweepPromisesLocked(now)
	s := n.outstanding[key]
	if s == nil {
		return
	}
	s.sending--
	if posted {
		if s.discharged > 0 {
			s.discharged--
		} else {
			promise.at = now
			s.promises = append(s.promises, promise)
		}
	}
	if s.discharged > s.sending {
		s.discharged = s.sending
	}
	n.pruneSessionLocked(key)
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
//
// The turn's own ack may still be in the air when this runs — the router posts
// it on a detached goroutine and settles the turn from the flush — in which case
// the charge is left against that send and the promise is never recorded.
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
//
// An ack still being sent counts. It is about to become a promise, and a cancel
// that walked away here would leave it to land behind its own ending.
func (n *ackNotifier) hasOutstandingAck(sessionID pgtype.UUID) bool {
	key := util.UUIDToString(sessionID)
	if key == "" {
		return false
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.outstanding[key].owed()
}

// takeAckForIdleRoom discharges the session's oldest promise and returns it, for
// a caller that is about to withdraw it out loud, and closes out the rest of the
// room with it.
//
// The caller must first have established from agent_task_queue that the session
// has no chat turn in flight, as of idleAsOf. That is what makes closing out the
// rest right: with nothing running, nothing is left that could answer any of
// them. Two states are cleared at once here.
//
// A promise whose ending never arrived — an event lost, an agent archived out
// from under the run (handler/agent.go cancels its tasks without broadcasting
// per row) — is indistinguishable in memory from one whose run is still working,
// and left on record it makes the room read as owed a reply for as long as a day.
// One promise in that state used to be enough to silence every cancellation
// notice after it, because each later cancel found the room one promise heavier
// than it really was.
//
// And a bulk cancel is broadcast once per task row, so a "cancel all tasks"
// click or a session delete with several queued turns delivers several events
// for one room. The user made one request and is owed one message about it: the
// first event through takes the room's promises together and the rest find it
// empty.
//
// Promises recorded after idleAsOf are kept. The run trigger is debounced behind
// the ack, so a turn ingested while this cancel was reading the database has no
// task row for it to have seen, and the room being idle says nothing about that
// turn.
//
// ok is false when there was nothing to return: either the room was already
// closed out, or the only thing outstanding is an ack whose send has not
// returned, which this call charges so the promise does not land behind its own
// cancellation. Nothing can be said in that second case — the room the ack went
// into is not known until its send returns.
func (n *ackNotifier) takeAckForIdleRoom(sessionID pgtype.UUID, idleAsOf time.Time) (ackPromise, bool) {
	key := util.UUIDToString(sessionID)
	if key == "" {
		return ackPromise{}, false
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	s := n.outstanding[key]
	if !s.owed() {
		return ackPromise{}, false
	}
	if len(s.promises) == 0 {
		s.discharged++
		n.pruneSessionLocked(key)
		return ackPromise{}, false
	}
	// Oldest first, so the promises this cancel may speak for are a prefix.
	cut := 0
	for cut < len(s.promises) && !s.promises[cut].at.After(idleAsOf) {
		cut++
	}
	if cut == 0 {
		// Everything the room holds was promised after this cancel read the
		// database, so none of it is this cancel's to withdraw.
		return ackPromise{}, false
	}
	promise := s.promises[0]
	s.promises = append(s.promises[:0], s.promises[cut:]...)
	n.pruneSessionLocked(key)
	return promise, true
}

// releaseOutstandingAck discharges the session's oldest outstanding ack without
// posting anything. The caller uses it when an acked run has reported an ending
// of its own: the promise is discharged whether or not a reply could actually be
// delivered, and leaving it on record would let an unrelated later cancel in
// the same conversation withdraw a promise that no longer stands.
//
// When the room's own ack is still being sent this charges that send instead, so
// a run that outran its own "On it" does not leave the promise standing behind
// it.
func (n *ackNotifier) releaseOutstandingAck(sessionID pgtype.UUID) {
	key := util.UUIDToString(sessionID)
	if key == "" {
		return
	}
	n.mu.Lock()
	n.dischargeOldestLocked(key)
	n.mu.Unlock()
}

// dischargeOldestLocked answers one of the session's outstanding acks: its
// oldest posted promise, or — when none has landed yet — an ack still being
// sent, charged so that send records nothing when it returns.
func (n *ackNotifier) dischargeOldestLocked(key string) {
	s := n.outstanding[key]
	switch {
	case !s.owed():
		return
	case len(s.promises) > 0:
		s.promises = s.promises[1:]
	default:
		s.discharged++
	}
	n.pruneSessionLocked(key)
}

// sessionLocked returns the session's entry, creating it if this is the first
// thing the conversation owes.
func (n *ackNotifier) sessionLocked(key string) *sessionAcks {
	s := n.outstanding[key]
	if s == nil {
		s = &sessionAcks{}
		n.outstanding[key] = s
	}
	return s
}

// pruneSessionLocked removes a session that owes nothing and has no ack in
// flight. Removing rather than keeping an empty entry is what keeps the map
// bounded by the conversations currently waiting on something.
func (n *ackNotifier) pruneSessionLocked(key string) {
	if s := n.outstanding[key]; s != nil && len(s.promises) == 0 && s.sending == 0 {
		delete(n.outstanding, key)
	}
}

// sweepPromisesLocked drops every promise older than ackPromiseMaxAge. See the
// outstanding field for why this stands behind the paths that discharge
// promises rather than replacing them.
func (n *ackNotifier) sweepPromisesLocked(now time.Time) {
	for key, s := range n.outstanding {
		kept := s.promises[:0]
		for _, p := range s.promises {
			if now.Sub(p.at) < ackPromiseMaxAge {
				kept = append(kept, p)
			}
		}
		s.promises = kept
		n.pruneSessionLocked(key)
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

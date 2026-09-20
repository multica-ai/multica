package wecom

// ws_sender.go — a serialized writer for one WebSocket connection. gorilla
// forbids concurrent writes so every outbound frame goes through the same
// mutex; the ping loop, subscribe handshake, and Send() calls all share
// this writer.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// wsConn is the subset of gorilla's Conn the wecom package uses. Kept
// minimal so tests can inject a fake without embedding all of gorilla's
// surface.
type wsConn interface {
	ReadMessage() (int, []byte, error)
	WriteMessage(messageType int, data []byte) error
	SetReadDeadline(t time.Time) error
	SetWriteDeadline(t time.Time) error
	Close() error
}

// Dialer opens a WebSocket connection to the aibot endpoint. Production
// uses gorilla's default dialer; tests wire a fake pointing at an
// httptest.Server.
type Dialer interface {
	DialContext(ctx context.Context, url string, header http.Header) (wsConn, *http.Response, error)
}

// defaultDialer is the production Dialer. Proxy is set explicitly because a
// zero-valued websocket.Dialer has a nil Proxy and ignores the environment,
// unlike websocket.DefaultDialer — and self-hosted deployments behind a
// corporate egress proxy reach the WeCom endpoint only through
// HTTPS_PROXY.
var defaultDialer Dialer = gorillaDialer{d: &websocket.Dialer{
	HandshakeTimeout: handshakeTimeout,
	Proxy:            http.ProxyFromEnvironment,
}}

type gorillaDialer struct {
	d *websocket.Dialer
}

func (g gorillaDialer) DialContext(ctx context.Context, u string, header http.Header) (wsConn, *http.Response, error) {
	conn, resp, err := g.d.DialContext(ctx, u, header)
	if err != nil {
		return nil, resp, err
	}
	return &gorillaWSConn{Conn: conn}, resp, nil
}

// gorillaWSConn wraps *websocket.Conn so it satisfies wsConn without leaking
// the concrete type into wsConn's method signatures.
type gorillaWSConn struct {
	*websocket.Conn
}

// wsSender serializes writes to one WebSocket connection. Instantiated per
// Connect() call and dropped when the connection ends.
type wsSender struct {
	conn wsConn
	log  *slog.Logger

	// wmu serializes writes — gorilla forbids concurrent ones. A one-slot
	// channel rather than a Mutex because a caller with a deadline has to be
	// able to stop waiting for its turn: a stream frame closing a bubble runs
	// on the bus subscriber's ten-second budget, and queueing behind a 20KB
	// push would spend all of it before the frame ever reached the socket.
	wmu chan struct{}

	// replies holds the callers waiting on a server verdict, keyed by the
	// req_id they wrote. Only the read loop delivers into these, which is why
	// inbound callbacks must not run on it — see the note on sendTextCtx.
	ackMu   sync.Mutex
	replies map[string]*replyWaiter

	// quota holds this connection's aibot_send_msg allowance, per target chat,
	// and retryBackoff is what a throttled push waits before its one retry.
	// One quota per socket is the whole accounting — see rate_limit.go for why
	// that is the right scope and where the numbers come from.
	quota        *sendQuota
	retryBackoff time.Duration

	// waiters holds the STREAM frames whose verdict somebody is standing by
	// for, keyed by the callback req_id the frame echoes. Separate from
	// replies because the two answer different questions and their keys come
	// from different places: a req_id in replies is one we minted for one
	// frame, while a stream's is the server's own and carries a whole turn's
	// frames. Guarded by ackMu.
	waiters map[string]*ackWaiter

	// streams is the per-turn bookkeeping that makes a verdict trustworthy and
	// a sealed bubble final. Guarded by ackMu; entries are created only by a
	// stream frame, so the ordinary pushes that share this connection never
	// touch it.
	streams map[string]*streamAcks

	// ackTimeout is how long a stream frame waits for its verdict. A field
	// rather than the constant so a test can exercise the give-up path without
	// standing still for five seconds.
	ackTimeout time.Duration

	// seq numbers outbound frames in the order they reach the socket.
	// Guarded by the writer slot (wmu), which is the point at which the ping
	// loop, agent replies, inbox pushes and stream frames become ordered — so
	// it is the wire order by construction, and it is what pairs a traced send
	// attempt with its outcome. req_id cannot do that job, because a pong
	// echoes the server's req_id and that may be empty or repeated. It never
	// goes on the wire.
	seq uint64

	// chats serializes whole logical messages per target chat. mu orders one
	// frame write; it is released before the ack wait, which is where an
	// unrelated send used to land between two pieces of one answer.
	//
	// EVERY push the reader sees takes it: text through sendTextCtx and files
	// through sendMedia. Half of that is no rule at all — a picture between
	// "(1/3)" and "(2/3)" is the same unreadable chat as a stray sentence
	// there, and attachment delivery is spawned alongside the answer it came
	// with, so the two are concurrent by construction rather than by
	// coincidence. What it does NOT cover is the upload: that puts nothing in
	// the chat, and holding the chat's turn for a multi-megabyte transfer
	// would queue every other message behind bytes that have not yet become a
	// message.
	chats chatLocks
}

// chatLocks is one lock per target chat, created on demand and dropped when
// the last holder leaves, so a process that has talked to many chats does not
// keep an entry for each of them forever.
//
// Per CHAT rather than per connection on purpose: a second answer to a
// different room has no reason to queue behind this one, and the ping loop
// writes through request/write and never takes a chat lock at all, so it
// cannot be held up by a send.
type chatLocks struct {
	mu    sync.Mutex
	locks map[string]*chatLock
}

type chatLock struct {
	// ch is a mutex that can be waited on with a context: capacity one, a
	// token in it means held.
	ch   chan struct{}
	refs int
}

// acquire blocks until this chat is free or ctx ends. The returned release is
// nil when it returns an error.
//
// The wait is bounded by whoever holds it: a holder is inside at most one
// ackTimeout per piece, and the pieces of one answer are few. A caller on
// context.Background therefore waits rather than interleaving, which is the
// whole point — the alternative is the reader seeing an unrelated message
// wedged into the middle of an answer.
func (c *chatLocks) acquire(ctx context.Context, chatID string) (func(), error) {
	c.mu.Lock()
	if c.locks == nil {
		c.locks = make(map[string]*chatLock)
	}
	l := c.locks[chatID]
	if l == nil {
		l = &chatLock{ch: make(chan struct{}, 1)}
		c.locks[chatID] = l
	}
	l.refs++
	c.mu.Unlock()

	release := func() {
		<-l.ch
		c.mu.Lock()
		l.refs--
		if l.refs == 0 {
			delete(c.locks, chatID)
		}
		c.mu.Unlock()
	}
	drop := func() {
		c.mu.Lock()
		l.refs--
		if l.refs == 0 {
			delete(c.locks, chatID)
		}
		c.mu.Unlock()
	}

	// A free chat is taken without consulting the context at all. select picks
	// at RANDOM among ready cases, so a caller whose context is already dead
	// arriving at a chat nobody holds would otherwise be turned away half the
	// time for a chat nobody was using.
	//
	// It also keeps errChatBusy honest: it is returned only when the chat
	// really was somebody else's and the wait ran out. What the caller gets
	// instead is request's pre-write check, which is the same fact under a
	// different name — both wrap errNotAttempted, so the classifiers cannot
	// tell them apart and do not need to.
	select {
	case l.ch <- struct{}{}:
		return release, nil
	default:
	}
	select {
	case l.ch <- struct{}{}:
		return release, nil
	case <-ctx.Done():
		drop()
		return nil, fmt.Errorf("%w: %w", errChatBusy, ctx.Err())
	}
}

// errNotAttempted marks a send that ended BEFORE any byte could leave this
// process. It is the one mark on this path that means "certainly not
// delivered", and it is the only thing the three classifiers have to test for
// — provablyNotSent (relay_outbound.go), unconfirmedReason (outbound_outcome.go)
// and sendOutcome (outbound_media.go).
//
// It exists because the bare ctx.Err() these paths used to return said the
// opposite. Every classifier reads a context error as "the frame may be in
// front of the person already" — the right reading for a context that ended
// while waiting for a VERDICT (errAckAbandoned), and the exact inversion of
// one that ended before the write. So the direct path filed a message it had
// never sent as "outcome unknown", which is the one outcome nobody may resend;
// the relay settled its claim and stopped offering it; and the media path told
// the user their file might have arrived. The user got nothing and the party
// whose job is to try again was told not to.
//
// Every not-attempted failure WRAPS this rather than carrying its own
// unrelated sentinel, so the classifiers ask one question instead of keeping a
// list in step with this file. Two failures wrap it today: the chat lock's
// wait running out (errChatBusy) and request's pre-write check.
//
// Each of those also wraps ctx.Err(), because the cause is worth having in a
// log line. That is why every classifier has to test for this AHEAD of its
// generic context branch — errors.Is finds context.Canceled in here too.
var errNotAttempted = errors.New("wecom: nothing was written")

// errChatBusy — the wait for this chat's turn ended before the turn came, and
// NOT ONE BYTE went anywhere. The lock is taken before a frame is built, so
// this and request's pre-write check are the two failures on the send path
// that are provably non-deliveries.
var errChatBusy = fmt.Errorf("%w; the wait for this chat's turn ended first", errNotAttempted)

func newWSSender(conn wsConn, log *slog.Logger) *wsSender {
	if log == nil {
		log = slog.Default()
	}
	return &wsSender{
		conn:         conn,
		log:          log,
		wmu:          make(chan struct{}, 1),
		replies:      make(map[string]*replyWaiter),
		waiters:      make(map[string]*ackWaiter),
		streams:      make(map[string]*streamAcks),
		ackTimeout:   ackTimeout,
		quota:        newSendQuota(),
		retryBackoff: sendRetryBackoff,
	}
}

// lockWriter takes the writer, or gives up when ctx does. A caller with no
// deadline of its own — the ping, the subscribe handshake, a proactive push —
// passes context.Background() and waits as long as it takes.
// tryLockWriter takes the writer if it is free, without waiting. It exists for
// the same reason sync.Mutex.TryLock does: a probe that needs to know whether
// somebody else is inside, and must not queue behind them to find out.
func (s *wsSender) tryLockWriter() bool {
	select {
	case s.wmu <- struct{}{}:
		return true
	default:
		return false
	}
}

func (s *wsSender) lockWriter(ctx context.Context) error {
	select {
	case s.wmu <- struct{}{}:
		return nil
	default:
	}
	select {
	case s.wmu <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *wsSender) unlockWriter() { <-s.wmu }

// ackTimeout caps the wait for a verdict. WeCom answers in a few hundred
// milliseconds; past this we assume the ack was lost rather than the frame
// refused, which matters because the two call for opposite responses.
const ackTimeout = 5 * time.Second

// errAckTimeout — the frame went out and no verdict came back. Distinct from a
// refusal: the message may well have been delivered, so a caller retries at
// its own risk rather than reporting failure.
var errAckTimeout = errors.New("wecom: timed out waiting for the server verdict")

// wecomAPIError is a refusal the server stated. Carrying the errcode rather
// than a string is what lets a caller tell a permanent refusal (bad frame,
// bot removed from the chat) from a transient one (rate limited) instead of
// pattern-matching prose.
type wecomAPIError struct {
	Cmd  string
	Code int
	Msg  string
}

func (e *wecomAPIError) Error() string {
	return fmt.Sprintf("wecom: %s rejected errcode=%d errmsg=%s", e.Cmd, e.Code, e.Msg)
}

var (
	// errStreamBusy — a mid-stream frame was skipped because the previous
	// frame on this req_id has not been acked. This is the backpressure the
	// official SDK calls replyStreamNonBlocking: non-final frames yield.
	// Closing frames do not yield either — they queue (awaitAck).
	errStreamBusy = errors.New("wecom: previous stream frame still unacked")

	// errStreamAckTimeout — the frame went out and no verdict came back. The
	// frame may well have landed, so callers weigh a possible duplicate
	// against a possible silence rather than assuming failure.
	errStreamAckTimeout = errors.New("wecom: stream frame ack timed out")

	// errStreamSuperseded — a non-final frame reached the writer after the
	// closing frame had sealed the stream, and was refused there.
	errStreamSuperseded = errors.New("wecom: stream frame superseded by the closing frame")

	// errNoLiveConnection lives in outbound_outcome.go: classifyDrop matches on
	// it to file a delivery under no_live_connection, and two sentinels with
	// the same meaning would split that counter in half depending on which
	// layer raised it.
)

// ackWaiter is one stream frame's standing request for a verdict. seq is where
// the frame sits in its req_id's write order, stamped at the moment it goes on
// the wire — see streamAcks for why a verdict has to be matched rather than
// simply handed to whoever is waiting.
type ackWaiter struct {
	ch  chan ackResult
	seq uint64 // 0 until the frame is written
	// addressable says every frame written before this one on the same req_id
	// had already been answered when this one went out — which is what makes
	// the position of an arriving verdict identify the frame it belongs to.
	//
	// awaitAck is what keeps it true, by not letting a frame out while the
	// server still owes one. This flag is the assertion of that invariant at
	// the point it is relied on: if it is ever false, something wrote past the
	// gate and no verdict on this req_id may be trusted by position.
	addressable bool
	// rewrite marks the one frame the gate lets past an outstanding debt: the
	// SAME closing frame written again. It is what makes such a frame
	// addressable despite the debt — and the two are one decision, not two.
	// Letting the frame out while refusing to match its verdict is strictly
	// worse than refusing it outright: seal then reads errStreamAckTimeout in
	// place of the refusal the server actually sent, and a refusal is the only
	// signal that routes the answer to the plain message. Measured on
	// 2026-09-03 (STRATEGY §6.5): six identical rewrites of a sealed stream all
	// returned errcode 0, so whichever identical write a verdict belongs to it
	// reports the same outcome — the same fact that justifies letting it out.
	rewrite bool
	done    chan struct{} // closed once the waiter has left the table, by verdict or by cancel
	once    sync.Once
}

func newAckWaiter() *ackWaiter {
	return &ackWaiter{ch: make(chan ackResult, 1), done: make(chan struct{})}
}

// resolve marks the waiter as gone from the table. A closing frame parked
// behind it (awaitAck) wakes on this.
func (w *ackWaiter) resolve() { w.once.Do(func() { close(w.done) }) }

// ackResult is one server verdict.
type ackResult struct {
	code int
	msg  string
}

// streamAcks counts one req_id's stream frames in and its verdicts out, and
// remembers when the closing frame has gone.
//
// Both halves exist because a bubble is written to more than once per turn.
// The ack frame carries nothing but the req_id — no stream id, no sequence —
// so a verdict is only identifiable by its position.
//
// MEASURED 2026-09-02 against the live bot: with several frames of one req_id
// in flight, acks do NOT come back in write order — 24 frames written
// back-to-back were answered grouped by outcome, and eleven of twelve
// concurrent refreshes to one stream were refused with errcode 6000 ("data
// version conflict"). So position matching is only sound while AT MOST ONE
// frame of a req_id is on the wire, and that is the rule awaitAck now enforces
// for closing frames as well as refreshes.
//
// The count still matters under that rule, for the one ordering that remains:
// a frame whose caller gave up (cancelAck) may still be answered later, and
// that late verdict must not be handed to the next frame. With one frame in
// flight the worst a mis-ordered late ack can do is make the NEXT frame time
// out, which sends the answer as a plain message — a possible duplicate, never
// a silence.
//
// sealed is the other half: a finished stream is immutable, so a frame that
// lost the race to the answer must never reach the wire behind it. It is kept
// PER STREAM ID, not per req_id, so a frame is refused only as a straggler of
// the stream it actually names. Nothing on this path puts a second stream on
// one req_id today — a round opens one bubble and one ending seals it — and
// keying the seal by req_id would be the wrong shape the moment one does,
// refusing every frame of the new stream as a straggler of the old.
//
// acked counts verdicts that arrived and only those. Nothing ever advances it
// on a caller's behalf — see cancelAck for why a write-off is worse than a
// count that stays short.
type streamAcks struct {
	sent   uint64
	acked  uint64
	sealed map[string]struct{}
	at     time.Time
}

// isSealed reports whether a closing frame has gone out on this stream.
func (st *streamAcks) isSealed(streamID string) bool {
	_, ok := st.sealed[streamID]
	return ok
}

// seal records that a closing frame has gone out on this stream.
func (st *streamAcks) seal(streamID string) {
	if st.sealed == nil {
		st.sealed = make(map[string]struct{})
	}
	st.sealed[streamID] = struct{}{}
}

// streamAcksMax bounds the per-turn bookkeeping on a long-lived connection,
// and reaching it is the ONLY thing that ever retires an entry: nothing runs
// on a timer, and a turn ending does not remove its own row — pruneStreamsLocked
// is called from beginStreamFrameLocked and returns immediately below the cap.
// Age decides which entries a sweep may take once it runs, not when one runs.
//
// So the map holds every req_id this connection has streamed on until the cap
// trips. It is set well past any number of turns one bot can have inside the
// stream window, because a sweep that had to drop live entries would put their
// closing frames out of step, and 2048 of these costs under a hundred
// kilobytes.
const streamAcksMax = 2048

// replyWaiter is one caller parked on one req_id.
type replyWaiter struct{ ch chan replyResult }

// replyResult is a server answer. body is nil for the acks that carry nothing
// but a verdict.
type replyResult struct {
	code int
	msg  string
	body json.RawMessage
}

// routeResponse hands a server response to whoever is waiting for it and
// reports whether anybody was. The read loop calls it for every frame that
// answers one of our writes; an unclaimed ack is not an error, since the
// pushes that do not wait share this connection.
// Order matters: a request waiting on the body is asked first, because those
// req_ids are ours and a stream's are the server's — one lookup settles which
// kind of answer this is without the frame having to say. A stream ack that
// gets routed still reports false, so the read loop keeps logging a non-zero
// errcode the way it always has.
func (s *wsSender) routeResponse(env frameEnvelope) bool {
	if s.deliverReply(env) {
		return true
	}
	s.deliverAck(env.Headers.ReqID, env.ErrCode, env.ErrMsg)
	return false
}

// deliverAck hands a server ack to the stream frame it belongs to. The read
// loop calls it for every anonymous ack frame; acks for anything that is not a
// stream — the heartbeat, an ordinary push — fall straight through.
//
// "The frame it belongs to" is the whole point, and it is not the same as
// "whoever is waiting". A req_id carries a whole turn's frames and its acks say
// nothing about which one they answer, so the count decides: the Nth verdict on
// a req_id belongs to its Nth frame. A verdict for a frame whose caller has
// already given up is dropped here rather than handed to the next one.
func (s *wsSender) deliverAck(reqID string, code int, msg string) {
	if reqID == "" {
		return
	}
	s.ackMu.Lock()
	st, tracked := s.streams[reqID]
	if !tracked {
		s.ackMu.Unlock()
		return // not a stream frame's ack
	}
	st.acked++
	w, ok := s.waiters[reqID]
	if ok && w.addressable && w.seq == st.acked {
		delete(s.waiters, reqID)
	} else {
		ok = false
	}
	s.ackMu.Unlock()
	if !ok {
		return
	}
	select {
	case w.ch <- ackResult{code: code, msg: msg}:
	default:
	}
	w.resolve()
}

// awaitAck registers interest in the verdict for the stream frame about to be
// written, and is where "one frame in flight per req_id" is enforced.
//
// A non-final frame that finds one in flight yields with errStreamBusy. A
// closing frame waits for it to leave the table — by verdict or by its
// caller's own timeout, so the wait is bounded by ackTimeout — and only then
// takes its turn. It used to jump the queue instead, and that was the window
// the live probe showed to be lethal: two frames on the wire are answered in
// whatever order the server likes and the second is refused with 6000 for
// colliding with the first, so a refused answer could read as delivered.
// A FRAME GOES OUT ONLY WHEN THE SERVER OWES NOTHING ON THIS req_id. Two
// things have to be true, and only one of them used to be checked.
//
// Nobody is waiting — the old condition — keeps two callers from reading each
// other's verdict. It is not enough on its own, because a caller that GIVES UP
// leaves the table empty while the server still owes that frame an answer: the
// next frame is then written with two verdicts outstanding, and an ack carries
// only req_id, so which frame an arriving one belongs to is decided by
// position. Position is a guess the moment there is more than one.
//
// It is not a safe guess here. Acks come back grouped by outcome rather than in
// write order — twenty-four frames back-to-back answered as eleven, then
// twelve, then one (STRATEGY §6.4) — so the closing frame's own refusal can
// land before the abandoned opener's late acceptance. Matching by position
// then drops the refusal and hands the acceptance to the closing frame, which
// reports the answer as delivered and never falls back. Nobody receives it.
//
// So the second condition: every frame already written has been answered. An
// abandoned frame still blocks, until its verdict arrives or the caller's own
// budget ends — and a caller that runs out gets errStreamBusy or its context
// error, both of which leave the answer to the plain-message path rather than
// to a guess.
func (s *wsSender) awaitAck(ctx context.Context, reqID string, finish, rewrite bool) (*ackWaiter, error) {
	waitStart := time.Now()
	for {
		s.ackMu.Lock()
		prev, taken := s.waiters[reqID]
		st, tracked := s.streams[reqID]
		owed := tracked && st.acked < st.sent && !rewrite
		if !taken && !owed {
			w := newAckWaiter()
			w.rewrite = rewrite
			s.waiters[reqID] = w
			s.ackMu.Unlock()
			return w, nil
		}
		s.ackMu.Unlock()
		if !finish {
			return nil, errStreamBusy
		}
		if taken {
			select {
			case <-prev.done:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			continue
		}
		// Owed but unwaited: the abandoned frame's verdict is in flight and
		// nothing signals its arrival, so this is the one place a short poll
		// is the honest mechanism.
		//
		// Bounded, and deliberately not by the caller's whole budget. A
		// verdict that has not come back within one ack wait is not coming,
		// and spending the rest of the budget here costs the plain message
		// that is the answer's remaining route. Giving up returns
		// errStreamBusy, which is provably-not-sent: nothing was written, so
		// the fallback is free to send the answer exactly once.
		if time.Since(waitStart) > ackTimeout {
			return nil, errStreamBusy
		}
		select {
		case <-time.After(ackOwedPoll):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// ackOwedPoll is how often awaitAck re-checks whether an abandoned frame's
// verdict has landed. Short enough not to add a perceptible pause to the one
// turn in which it happens, long enough not to spin.
const ackOwedPoll = 10 * time.Millisecond

// cancelAck retires a waiter whose caller has stopped waiting, for either of
// the two reasons a caller stops: its own budget ran out, or the full ack
// timeout elapsed with nothing on the wire.
//
// Both leave the count alone, and that is the whole attribution rule: acked
// counts verdicts that ARRIVED, never verdicts we gave up on. Advancing it to
// cover a frame nobody is waiting for would hand that frame's real verdict —
// which lands a moment later, because the server does answer every frame — to
// whoever wrote next, and every frame after it in the turn as well. The one
// that pays is the closing frame: a stale "accepted" makes a refused answer
// read as delivered, so the caller never falls back and the reply is sent
// nowhere at all. Leaving the count short costs a turn whose verdict is truly
// lost its bubble, since every later frame then times out — and an ack timeout
// is exactly the signal that sends the answer as a plain message.
func (s *wsSender) cancelAck(reqID string, w *ackWaiter) {
	s.ackMu.Lock()
	if cur, ok := s.waiters[reqID]; ok && cur == w {
		delete(s.waiters, reqID)
	}
	s.ackMu.Unlock()
	w.resolve()
}

// beginStreamFrameLocked reserves the next place in a req_id's write order for
// the frame that is about to go out, and refuses a non-final frame once the
// closing frame has been written. Caller holds the writer, which is what makes
// the refusal airtight: the seal and the write it fences are decided inside the
// same critical section, so a later frame can never slip between the two and
// land on top of the answer.
func (s *wsSender) beginStreamFrameLocked(reqID, streamID string, w *ackWaiter, finish bool) bool {
	s.ackMu.Lock()
	defer s.ackMu.Unlock()
	st, ok := s.streams[reqID]
	if !ok {
		s.pruneStreamsLocked()
		st = &streamAcks{at: time.Now()}
		s.streams[reqID] = st
	}
	if st.isSealed(streamID) && !finish {
		return false
	}
	// Read before the increment: every earlier frame answered means acked has
	// caught up with sent.
	clean := st.acked == st.sent
	st.sent++
	if finish {
		st.seal(streamID)
	}
	if w != nil {
		w.seq = st.sent
		w.addressable = clean || w.rewrite
	}
	return true
}

// abortStreamFrameLocked gives back the place reserved for a frame that never
// reached the socket, so one failed write does not put every later verdict on
// this req_id out of step. Establishing that it never reached the socket is
// the caller's job — see the errWriteAttempted check at the one call site. The seal is not given back: a turn whose closing
// frame failed is over either way, and the caller has already fallen back to a
// plain message. Caller holds the writer.
func (s *wsSender) abortStreamFrameLocked(reqID string) {
	s.ackMu.Lock()
	defer s.ackMu.Unlock()
	if st, ok := s.streams[reqID]; ok && st.sent > 0 {
		st.sent--
	}
}

// pruneStreamsLocked retires turns the protocol has already forgotten. A sealed
// entry has to outlive its last ack — it is what stops a straggling frame from
// reopening a bubble the answer already closed — so age is the only thing that
// retires it. Caller holds s.ackMu.
func (s *wsSender) pruneStreamsLocked() {
	if len(s.streams) < streamAcksMax {
		return
	}
	now := time.Now()
	for k, st := range s.streams {
		// SETTLED, NOT SEALED, IS THE CONDITION. Requiring a seal kept the
		// counters of every turn whose closing frame never went out — the gate
		// refused it while a verdict was owed, or the answer fell back — for
		// the life of the connection, and after the seal/fallback rules those
		// are not rare.
		//
		// What actually has to be true is that no verdict is still coming for
		// this req_id: with acked == sent the server owes nothing, so nothing
		// can arrive later to be matched against counters that have been
		// reset. An entry still owed one stays, however old, because that is
		// the misattribution the sequence numbers exist to prevent.
		//
		// Age is not sufficient on its own, and it is worth saying why the
		// shorter argument fails: a req_id outlives its stream. The platform
		// ends a STREAM at ten minutes (doc 101463) but a new stream id on the
		// same req_id is accepted — measured 2026-08-09, STRATEGY §6.1 — so
		// "there will be no next frame" is not something age can establish.
		if st.acked >= st.sent && now.Sub(st.at) > streamMaxAge {
			delete(s.streams, k)
			continue
		}
		// The other way an entry stops protecting anything: cancelAck leaves a
		// debt on purpose, so a turn whose verdict never comes is never
		// settled and would be kept for the life of the connection.
		//
		// What retires it is not age by itself — a req_id outlives its stream,
		// and a new stream id on the same req_id is accepted (measured
		// 2026-08-09, STRATEGY §6.1). It is OUR OWN reach that ends: the round
		// store evicts a handle at streamMaxAge, so past that nothing can
		// hand seal a handle for this req_id, and a closer already holding one
		// is bounded by streamCloseTimeout. Past both, no frame can be written
		// here again and the counters guard nothing.
		if now.Sub(st.at) > streamMaxAge+streamCloseTimeout {
			delete(s.streams, k)
		}
	}
	// Whatever is left is young, which means it may still be a live turn.
	// Those may not be thrown away. A live turn whose counters are gone has its next frame
	// stamped from zero: a stale verdict for an earlier frame then matches the
	// closing one, the refusal that closing frame actually got is never seen,
	// and the answer is reported delivered while it went nowhere — the exact
	// misattribution the sequence numbers exist to prevent.
	//
	// A live turn is also the OLDEST entry by construction — its opening frame
	// is minutes older than the burst that filled the map — so "evict oldest
	// first" would pick exactly the wrong ones.
	//
	// The map can therefore exceed streamAcksMax under sustained load. That is
	// the right trade: the cap is a memory bound on bookkeeping that is small
	// per entry and self-clears as turns end, and losing a user's answer to
	// save a few kilobytes is not a trade anyone would make on purpose. The
	// warning is what says it is happening.
	if len(s.streams) >= streamAcksMax {
		s.log.Warn("wecom: stream bookkeeping over its cap and every entry is live or young; keeping them",
			"entries", len(s.streams), "cap", streamAcksMax)
	}
}

// awaitReply registers interest in the response for the frame about to be
// written. false means the req_id is already spoken for — with minted ids
// that is a collision we would rather fail on than silently cross wires.
func (s *wsSender) awaitReply(reqID string) (*replyWaiter, bool) {
	s.ackMu.Lock()
	defer s.ackMu.Unlock()
	if _, taken := s.replies[reqID]; taken {
		return nil, false
	}
	w := &replyWaiter{ch: make(chan replyResult, 1)}
	s.replies[reqID] = w
	return w, true
}

// cancelReply retires a waiter. Called on every exit path including the happy
// one — a request is one frame and one answer, so the entry is never useful
// twice, and leaving it would leak an entry per send.
func (s *wsSender) cancelReply(reqID string, w *replyWaiter) {
	s.ackMu.Lock()
	defer s.ackMu.Unlock()
	if cur, ok := s.replies[reqID]; ok && cur == w {
		delete(s.replies, reqID)
	}
}

// deliverReply hands a response to the request that asked for it, if there is
// one, and reports whether it was taken.
func (s *wsSender) deliverReply(env frameEnvelope) bool {
	if env.Headers.ReqID == "" {
		return false
	}
	s.ackMu.Lock()
	w, ok := s.replies[env.Headers.ReqID]
	if ok {
		delete(s.replies, env.Headers.ReqID)
	}
	s.ackMu.Unlock()
	if !ok {
		return false
	}
	// Buffered channel, and the entry is removed above, so this never blocks
	// and never delivers twice.
	select {
	case w.ch <- replyResult{code: env.ErrCode, msg: env.ErrMsg, body: env.Body}:
	default:
	}
	return true
}

// request writes one frame under a req_id of our own and waits for the whole
// answer. A non-nil error is either a *wecomAPIError carrying the server's
// errcode, errAckTimeout, or a transport failure.
func (s *wsSender) request(ctx context.Context, cmd string, body map[string]any) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		// Marked, for the same reason the wait below is marked and the
		// opposite fact. Nothing has been minted, registered or built at this
		// point, so this is proof the peer saw nothing — and a bare ctx.Err()
		// here is indistinguishable from the one twenty lines down, which
		// proves the opposite. A caller that cannot tell them apart has to
		// read both the same way, and either reading is wrong for one of them.
		return nil, fmt.Errorf("%w: %w", errNotAttempted, err)
	}
	reqID := newReqID()
	w, ok := s.awaitReply(reqID)
	if !ok {
		return nil, fmt.Errorf("wecom: %s req_id %s is already awaiting a response", cmd, reqID)
	}
	defer s.cancelReply(reqID, w)

	if err := s.write(map[string]any{
		"cmd":     cmd,
		"headers": frameHeaders{ReqID: reqID},
		"body":    body,
	}); err != nil {
		return nil, err
	}

	timer := time.NewTimer(ackTimeout)
	defer timer.Stop()
	select {
	case res := <-w.ch:
		if res.code != 0 {
			return nil, &wecomAPIError{Cmd: cmd, Code: res.code, Msg: res.msg}
		}
		return res.body, nil
	case <-timer.C:
		return nil, errAckTimeout
	case <-ctx.Done():
		// Marked, because this is not the same fact as the context error at
		// the top of this function. That one is raised before anything is
		// written; this one is raised after s.write returned without error,
		// which means WriteMessage completed and the bytes are gone. A caller
		// that cannot tell the two apart has to guess about a frame that may
		// be in front of the person right now.
		return nil, fmt.Errorf("%w: %w", errAckAbandoned, ctx.Err())
	}
}

// respondStream writes one frame of a streaming reply and waits for the
// server's verdict. ctx bounds the whole thing — the wait for the writer, the
// write itself and the wait for the ack — because the callers here run on a bus
// subscriber's own budget and none of those three is otherwise bounded by
// anything the caller chose.
//
// reqID is not ours to choose: every frame of one stream must echo the req_id
// of the aibot_msg_callback that opened the turn, or the server refuses it
// (846605). streamID is ours — reuse it to replace the bubble's body, and set
// finish once the content is final.
func (s *wsSender) respondStream(ctx context.Context, reqID, streamID, content string, finish bool) error {
	return s.respondStreamFrame(ctx, reqID, streamID, content, finish, false)
}

// respondStreamRewrite writes a frame this sender has already written once —
// seal's retry of a closing frame whose verdict never came back.
//
// It is the one write allowed past the owed-verdict gate, and safely so: an
// identical frame is not a second frame. Re-writing a sealed stream was
// measured against the live tenant on 2026-09-03 — six frames onto an
// already-sealed stream, same content and different, all errcode 0
// (STRATEGY §6.5) — so whichever of the identical writes a verdict belongs to,
// it reports the same outcome. Blocking the retry instead would leave a
// written frame unanswered and the answer resent as a plain message, which is
// the duplicate this whole path exists to avoid.
func (s *wsSender) respondStreamRewrite(ctx context.Context, reqID, streamID, content string, finish bool) error {
	return s.respondStreamFrame(ctx, reqID, streamID, content, finish, true)
}

func (s *wsSender) respondStreamFrame(ctx context.Context, reqID, streamID, content string, finish, rewrite bool) error {
	if reqID == "" {
		return errors.New("wecom: stream frame requires the callback req_id")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	body, err := respondStreamBody(streamID, content, finish)
	if err != nil {
		return err
	}

	w, err := s.awaitAck(ctx, reqID, finish, rewrite)
	if err != nil {
		return err
	}
	if err := s.writeStreamFrame(ctx, reqID, streamID, w, finish, map[string]any{
		"cmd":     cmdRespondMsg,
		"headers": frameHeaders{ReqID: reqID},
		"body":    body,
	}); err != nil {
		s.cancelAck(reqID, w)
		return err
	}

	timer := time.NewTimer(s.ackTimeout)
	defer timer.Stop()
	select {
	case res := <-w.ch:
		if res.code != 0 {
			return &streamError{Code: res.code, Msg: res.msg}
		}
		return nil
	case <-timer.C:
		s.cancelAck(reqID, w)
		return errStreamAckTimeout
	case <-ctx.Done():
		s.cancelAck(reqID, w)
		return errStreamAckTimeout
	}
}

// write marshals frame to JSON and pushes it under the writer mutex. Used by
// the callers with nobody waiting on them — the ping, the subscribe handshake,
// a proactive push — which wait for their turn however long it takes. The
// caller must not hold sendMu on wecomChannel — nothing here reaches back
// into the Channel.
func (s *wsSender) write(frame map[string]any) error {
	payload, err := json.Marshal(frame)
	if err != nil {
		return fmt.Errorf("wecom: marshal frame: %w", err)
	}
	// Extract the trace fields before taking the writer. Extraction is the
	// expensive half (a regexp redaction pass and a rune-wise cut over the
	// message body) and needs no ordering guarantee; what runs inside the
	// writer is a nil check when tracing is off, and two log lines when it is
	// on, against a socket write already in the same section.
	t := traceOutFields(s.log, frame)
	ctx := context.Background()
	if err := s.lockWriter(ctx); err != nil {
		return err
	}
	defer s.unlockWriter()
	return s.writeLocked(ctx, payload, t)
}

// writeStreamFrame is write() for a stream frame: the same serialized push,
// with the turn's bookkeeping done inside the writer's own critical section so
// two frames of one turn cannot interleave. A frame that arrives after the
// closing frame is dropped here rather than sent — the bubble is sealed, and a
// frame the server might still accept would paint the placeholder back over
// the answer.
//
// Unlike write() this one honours a deadline, at both places a write can stall:
// waiting for the writer and waiting for the socket.
func (s *wsSender) writeStreamFrame(ctx context.Context, reqID, streamID string, w *ackWaiter, finish bool, frame map[string]any) error {
	payload, err := json.Marshal(frame)
	if err != nil {
		return fmt.Errorf("wecom: marshal frame: %w", err)
	}
	t := traceOutFields(s.log, frame)
	if err := s.lockWriter(ctx); err != nil {
		return err
	}
	defer s.unlockWriter()
	if !s.beginStreamFrameLocked(reqID, streamID, w, finish) {
		return errStreamSuperseded
	}
	if err := s.writeLocked(ctx, payload, t); err != nil {
		// Only a frame that provably never reached the socket gives its place
		// back. errWriteAttempted means WriteMessage was entered, so the peer
		// may have taken the bytes and may yet answer them; handing the place
		// back would let that verdict settle the NEXT frame — the exact
		// misattribution the counters exist to prevent. The cost of keeping
		// the place is a debt the owed gate makes visible, which is the
		// recoverable side of the trade.
		if !errors.Is(err, errWriteAttempted) {
			s.abortStreamFrameLocked(reqID)
		}
		return err
	}
	return nil
}

// writeLocked pushes one already-marshalled frame. Caller holds the writer.
//
// The socket deadline is the sooner of the connection's own writeDeadline and
// whatever the caller gave itself. A frame is a few kilobytes, so a socket that
// cannot take one inside a caller's budget is congested rather than busy, and
// the Supervisor's reconnect is the designed answer to that.
// t carries the frame's trace fields, extracted by the caller before it took
// the writer; nil when tracing is off. Both lines are emitted from in here, so
// the recorded order is the wire order by construction — a line taken outside
// the writer is only correlated with it, because a goroutine can emit its line
// and be descheduled before it gets its turn, and the log then names the wrong
// frame as first.
func (s *wsSender) writeLocked(ctx context.Context, payload []byte, t *outTrace) error {
	s.seq++
	seq := s.seq
	traceOutAttempt(s.log, seq, t)

	deadline := time.Now().Add(writeDeadline)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	stage := traceStageDeadline
	attempted := false
	err := s.conn.SetWriteDeadline(deadline)
	if err == nil {
		stage = traceStageWrite
		attempted = true
		err = s.conn.WriteMessage(websocket.TextMessage, payload)
	}
	traceOutResult(s.log, seq, t, stage, err)
	if err != nil && attempted {
		return fmt.Errorf("%w: %w", errWriteAttempted, err)
	}
	return err
}

// errWriteAttempted marks a failure raised by the socket write itself, as
// opposed to one raised before any byte could leave: a marshal error, or a
// deadline the connection refused to set.
//
// The distinction is the caller's, not this function's. Once WriteMessage has
// been entered, the frame may have reached the peer and been acknowledged at
// the TCP layer before the local side surfaced a failure — a half-closed
// connection reports "broken pipe" to the writer for bytes the reader already
// has. So a failure past this point is not proof of non-delivery, and a caller
// that treats it as one will either deny a delivery that happened or resend a
// frame WeCom already acted on.
//
// Nothing before the write carries this: those failures are provably local,
// and a caller may report them as definite.
var errWriteAttempted = errors.New("wecom: frame write attempted")

// errAckAbandoned — the frame went out and the caller's context ended before a
// verdict came back. errAckTimeout's sibling: the same fact about the wire, a
// different reason the verdict is missing. It wraps the context error rather
// than replacing it, so every errors.Is(err, context.Canceled) reader keeps
// working and the outcome still files as "interrupted".
//
// It exists because request raises a context error in two places that mean
// opposite things — the check ahead of the write, where nothing left this
// process (errNotAttempted), and the wait after it, where the peer may already
// hold the frame. Until the two marks, they differed only in the line that
// raised them, which is not something a caller can see. A caller weighing a
// cancellation against another outcome it already holds then has to read every
// cancellation the same way, and either one of those readings is wrong.
// sendMsgFrame is that caller: it holds a refusal WeCom stated for a first
// frame, and must not let it speak for a second one that is already on the
// wire.
var errAckAbandoned = errors.New("wecom: the wait for the verdict was cut short after the frame went out")

// sendText pushes an aibot_send_msg (proactive push) with plain text to a
// specific chat. Callers pass channel.ChatType so the aibot chat_type int
// (1=single, 2=group) is decided at the wecom-side boundary, not the
// engine's. Used by OutboundReplier and Outbound.
func (s *wsSender) sendText(chatID string, chatTypeInt int, content string) error {
	return s.sendTextCtx(context.Background(), chatID, chatTypeInt, content)
}

// sendTextCtx is sendText that reads the server's verdict. Before this, a push
// was fire-and-forget: a frame WeCom refused — over the size cap, addressed to
// a chat the bot is no longer in, rate limited — returned nil, so the caller
// recorded a delivery that never happened and the operator saw only
// unattributed ack lines go past.
//
// Safe to block here only because inbound callbacks no longer run on the read
// loop (wecom_channel.go): the read loop is the sole deliverer of acks, so a
// send that waited for one from inside a callback would have waited on itself.
// It is also where a long answer is cut into pieces the server will accept.
// That belongs here rather than at any one call site because a body past the
// cap is refused WHOLE: every caller that pushes plain text — the agent's
// reply, an inbox card, a relayed frame — would otherwise have to remember the
// rule, and the one that forgot would lose its message silently.
//
// A piece that fails stops the rest: the pieces after it are the tail of an
// answer whose head did not arrive, and sending them alone would read as the
// bot replying to nothing.
//
// A failure past the FIRST piece is wrapped in errPartiallySent, because the
// caller's question — may this send be tried again? — has a different answer
// once part of the answer is in the chat.
func (s *wsSender) sendTextCtx(ctx context.Context, chatID string, chatTypeInt int, content string) error {
	pieces := splitForWire(content)
	// Held for every send, not only a split one: a single-frame push from
	// another caller — an inbox card, the file this same answer produced
	// (sendMedia takes the same lock), the unsupported-type notice — is
	// exactly what used to arrive between piece one and piece two, and with
	// two long answers in flight at once the (n/total) counters could not be
	// matched back to their own text.
	//
	// A caller whose context ends while queued here gets errChatBusy, which
	// wraps errNotAttempted: nothing has been built yet, let alone written,
	// and the classifiers have to be able to tell that from a context that
	// ended while waiting for a verdict.
	release, err := s.chats.acquire(ctx, chatID)
	if err != nil {
		return err
	}
	defer release()
	for i, piece := range pieces {
		if err := s.sendOneTextCtx(ctx, chatID, chatTypeInt, piece); err != nil {
			if i > 0 {
				return fmt.Errorf("%w: %w", errPartiallySent, err)
			}
			return err
		}
	}
	return nil
}

// errPartiallySent marks a long answer whose LATER piece failed after an
// earlier one was accepted by the server.
//
// It exists for one caller decision. Everything else on this path asks "did
// this frame reach the peer", and for the failing piece the honest answer may
// still be no — but the SEND is not the frame. splitForWire cuts one answer
// into several aibot_send_msg frames, and by the time piece two fails, piece
// one is already in the user's chat. A caller that reads the failure as "this
// send put nothing on the wire" and retries the whole content prints the first
// piece a second time, which is the one outcome a retry exists to avoid.
//
// So this is deliberately NOT a claim about the failing frame — provablyNotSent
// asks about the send as a whole, and this answers that question.
var errPartiallySent = errors.New("wecom: an earlier piece of this answer was already accepted")

// sendOneTextCtx writes exactly one aibot_send_msg frame and reads its ack.
// Nothing here may exceed the cap: splitForWire is the only thing standing
// between an agent's answer and a 45002 refusal.
func (s *wsSender) sendOneTextCtx(ctx context.Context, chatID string, chatTypeInt int, content string) error {
	body, err := sendMsgTextBody(chatID, chatTypeInt, content)
	if err != nil {
		return err
	}
	return s.sendMsgFrame(ctx, chatID, body)
}

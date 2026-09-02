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
	mu   sync.Mutex
	log  *slog.Logger

	// replies holds the callers waiting on a server verdict, keyed by the
	// req_id they wrote. Only the read loop delivers into these, which is why
	// inbound callbacks must not run on it — see the note on sendTextCtx.
	ackMu   sync.Mutex
	replies map[string]*replyWaiter

	// waiters holds the STREAM frames whose verdict somebody is standing by
	// for, keyed by the callback req_id the frame echoes. Separate from
	// replies because the two answer different questions and their keys come
	// from different places: a req_id in replies is one we minted for one
	// frame, while a stream's is the server's own and carries a whole turn's
	// frames. Guarded by ackMu.
	waiters map[string]*ackWaiter

	// streams is the per-turn bookkeeping that makes a verdict trustworthy and
	// a sealed stream final. Guarded by ackMu; entries are created only by a
	// stream frame, so the ordinary pushes that share this connection never
	// touch it.
	streams map[string]*streamAcks

	// ackTimeout is how long a stream frame waits for its verdict. A field
	// rather than the constant so a test can exercise the give-up path without
	// standing still for five seconds.
	ackTimeout time.Duration

	// seq numbers outbound frames in the order they reach the socket.
	// Guarded by mu, so it is the wire order by construction, and it is what
	// pairs a traced send attempt with its outcome — req_id cannot do that
	// job, because a pong echoes the server's req_id and that may be empty
	// or repeated. It never goes on the wire.
	seq uint64
}

func newWSSender(conn wsConn, log *slog.Logger) *wsSender {
	if log == nil {
		log = slog.Default()
	}
	return &wsSender{
		conn:       conn,
		log:        log,
		replies:    make(map[string]*replyWaiter),
		waiters:    make(map[string]*ackWaiter),
		streams:    make(map[string]*streamAcks),
		ackTimeout: ackTimeout,
	}
}

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
	ch   chan ackResult
	seq  uint64        // 0 until the frame is written
	done chan struct{} // closed once the waiter has left the table, by verdict or by cancel
	once sync.Once
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
// MEASURED 2026-09-02 against the live bot (STRATEGY §6.4): with several
// frames of one req_id in flight, acks do NOT come back in write order — 24
// frames written back-to-back were answered grouped by outcome, and eleven of
// twelve concurrent refreshes to one stream were refused with errcode 6000
// ("data version conflict"). So position matching is only sound while AT MOST
// ONE frame of a req_id is on the wire, and that is the rule awaitAck now
// enforces for closing frames as well as refreshes.
//
// The count still matters under that rule, for the one ordering that remains:
// a frame whose caller gave up (cancelAck) may still be answered later, and
// that late verdict must not be handed to the next frame. With one frame in
// flight the worst a mis-ordered late ack can do is make the NEXT frame time
// out, which sends the answer as a plain message — a possible duplicate, never
// a silence.
//
// sealed is the other half: a finished stream is immutable, so a frame that
// lost the race to the answer must never reach the wire behind it.
//
// acked counts verdicts that arrived and only those. Nothing ever advances it
// on a caller's behalf — see cancelAck for why a write-off is worse than a
// count that stays short.
type streamAcks struct {
	sent   uint64
	acked  uint64
	sealed bool
	at     time.Time
}

// streamMaxAge is how long the server keeps a stream writable, measured on
// the live bot: frames past ten minutes from the first one are refused with
// errcodeStreamExpired. Here it only decides which sealed bookkeeping entries a
// sweep may retire.
const streamMaxAge = 10 * time.Minute

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
//
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
	if ok && w.seq == st.acked {
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
func (s *wsSender) awaitAck(ctx context.Context, reqID string, finish bool) (*ackWaiter, error) {
	for {
		s.ackMu.Lock()
		prev, taken := s.waiters[reqID]
		if !taken {
			w := newAckWaiter()
			s.waiters[reqID] = w
			s.ackMu.Unlock()
			return w, nil
		}
		s.ackMu.Unlock()
		if !finish {
			return nil, errStreamBusy
		}
		select {
		case <-prev.done:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

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
func (s *wsSender) beginStreamFrameLocked(reqID string, w *ackWaiter, finish bool) bool {
	s.ackMu.Lock()
	defer s.ackMu.Unlock()
	st, ok := s.streams[reqID]
	if !ok {
		s.pruneStreamsLocked()
		st = &streamAcks{at: time.Now()}
		s.streams[reqID] = st
	}
	if st.sealed && !finish {
		return false
	}
	st.sent++
	if finish {
		st.sealed = true
	}
	if w != nil {
		w.seq = st.sent
	}
	return true
}

// abortStreamFrameLocked gives back the place reserved for a frame that never
// reached the socket, so one failed write does not put every later verdict on
// this req_id out of step. The seal is not given back: a turn whose closing
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
		if st.sealed && now.Sub(st.at) > streamMaxAge {
			delete(s.streams, k)
		}
	}
	// Whatever is left is either sealed and young, or still open. Neither may
	// be thrown away. A live turn whose counters are gone has its next frame
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

// request writes one frame under a req_id of our own and waits for the whole
// answer. A non-nil error is either a *wecomAPIError carrying the server's
// errcode, errAckTimeout, or a transport failure.
func (s *wsSender) request(ctx context.Context, cmd string, body map[string]any) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
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
		return nil, ctx.Err()
	}
}

// write marshals frame to JSON and pushes it under the writer mutex. The
// caller must not hold sendMu on wecomChannel — nothing here reaches back
// into the Channel.
func (s *wsSender) write(frame map[string]any) error {
	payload, err := json.Marshal(frame)
	if err != nil {
		return fmt.Errorf("wecom: marshal frame: %w", err)
	}
	// Extract the trace fields out here, emit them in there. This mutex is
	// the point at which the ping loop, agent replies and inbox pushes become
	// ordered, so a record taken inside it matches the wire by construction,
	// while one taken outside is only correlated with it — a goroutine can
	// emit its line and be descheduled before it takes the mutex, and the log
	// then names the wrong frame as first. Extraction is the expensive half
	// (a regexp redaction pass and a rune-wise cut over the message body) and
	// needs no such guarantee, so it stays out here; what runs under the
	// mutex is a nil check when tracing is off, and two log lines when it is
	// on, against a socket write that is already in the same section.
	t := traceOutFields(s.log, frame)

	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeLocked(context.Background(), payload, t)
}

// writeLocked pushes one already-marshalled frame. Caller holds s.mu.
//
// The socket deadline is the sooner of the connection's own writeDeadline and
// whatever the caller gave itself. A frame is a few kilobytes, so a socket that
// cannot take one inside a caller's budget is congested rather than busy, and
// the Supervisor's reconnect is the designed answer to that.
func (s *wsSender) writeLocked(ctx context.Context, payload []byte, t *outTrace) error {
	s.seq++
	traceOutAttempt(s.log, s.seq, t)

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
	traceOutResult(s.log, s.seq, t, stage, err)
	if err != nil && attempted {
		return fmt.Errorf("%w: %w", errWriteAttempted, err)
	}
	return err
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

	w, err := s.awaitAck(ctx, reqID, finish)
	if err != nil {
		return err
	}
	if err := s.writeStreamFrame(ctx, reqID, w, finish, map[string]any{
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

// writeStreamFrame is write() for a stream frame: the same serialized push,
// with the turn's bookkeeping done inside the writer's own critical section so
// two frames of one turn cannot interleave. A frame that arrives after the
// closing frame is dropped here rather than sent — the stream is sealed, and a
// frame the server might still accept would paint the placeholder back over
// the answer.
//
// Unlike write() this one honours the caller's deadline at the socket.
func (s *wsSender) writeStreamFrame(ctx context.Context, reqID string, w *ackWaiter, finish bool, frame map[string]any) error {
	payload, err := json.Marshal(frame)
	if err != nil {
		return fmt.Errorf("wecom: marshal frame: %w", err)
	}
	t := traceOutFields(s.log, frame)
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.beginStreamFrameLocked(reqID, w, finish) {
		return errStreamSuperseded
	}
	if err := s.writeLocked(ctx, payload, t); err != nil {
		s.abortStreamFrameLocked(reqID)
		return err
	}
	return nil
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
func (s *wsSender) sendTextCtx(ctx context.Context, chatID string, chatTypeInt int, content string) error {
	body, err := sendMsgTextBody(chatID, chatTypeInt, content)
	if err != nil {
		return err
	}
	_, err = s.request(ctx, cmdSendMsg, body)
	return err
}

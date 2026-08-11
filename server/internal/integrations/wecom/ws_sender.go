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
	return &wsSender{conn: conn, log: log, replies: make(map[string]*replyWaiter)}
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
func (s *wsSender) routeResponse(env frameEnvelope) bool {
	return s.deliverReply(env)
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
	s.seq++
	traceOutAttempt(s.log, s.seq, t)

	stage := traceStageDeadline
	err = s.conn.SetWriteDeadline(time.Now().Add(writeDeadline))
	if err == nil {
		stage = traceStageWrite
		err = s.conn.WriteMessage(websocket.TextMessage, payload)
	}
	traceOutResult(s.log, s.seq, t, stage, err)
	return err
}

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

package wecom

// subscribe_error_test.go — a handshake the server refused and a handshake
// that never reached the server call for opposite responses. One needs a
// person to go fix the installation; the other usually fixes itself. Callers
// (the Supervisor's logging today, a metrics split tomorrow) can only tell
// them apart if the rejection is identifiable.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestSubscribeRejectionIsIdentifiable(t *testing.T) {
	// The shape subscribe() produces on a non-zero errcode.
	err := fmt.Errorf("%w: errcode=%d errmsg=%s", errSubscribeRejected, 40001, "invalid credential")
	if !errors.Is(err, errSubscribeRejected) {
		t.Fatal("a refused handshake is not identifiable, so it cannot be told apart from a network failure")
	}
	// The diagnosis must survive: the errcode is what names WHICH credential
	// problem it is.
	if got := err.Error(); got == "" || !contains(got, "40001") || !contains(got, "invalid credential") {
		t.Errorf("error text lost the server's diagnosis: %q", got)
	}
}

func TestTransportFailuresAreNotRejections(t *testing.T) {
	for _, err := range []error{
		&net.OpError{Op: "dial", Err: errors.New("connection refused")},
		errors.New("wecom: subscribe read: i/o timeout"),
		fmt.Errorf("wecom: send subscribe: %w", errors.New("broken pipe")),
	} {
		if errors.Is(err, errSubscribeRejected) {
			t.Errorf("%v was classified as a credential rejection — an operator would go chase a tenant whose bot is fine", err)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// ackingConn answers the subscribe frame with a chosen errcode, echoing the
// req_id the way the server does.
type ackingConn struct {
	errCode int
	errMsg  string
	sent    chan []byte
	reqID   string
}

func (c *ackingConn) WriteMessage(_ int, data []byte) error {
	var env frameEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return err
	}
	c.reqID = env.Headers.ReqID
	return nil
}

func (c *ackingConn) ReadMessage() (int, []byte, error) {
	payload, err := json.Marshal(frameEnvelope{
		Headers: frameHeaders{ReqID: c.reqID},
		ErrCode: c.errCode,
		ErrMsg:  c.errMsg,
	})
	if err != nil {
		return 0, nil, err
	}
	return websocket.TextMessage, payload, nil
}

func (c *ackingConn) SetReadDeadline(time.Time) error  { return nil }
func (c *ackingConn) SetWriteDeadline(time.Time) error { return nil }
func (c *ackingConn) Close() error                     { return nil }

// TestSubscribeWrapsAServerRefusal drives the real subscribe() so the test
// fails if the wrapping is ever dropped — asserting on the sentinel alone
// would keep passing while the caller could no longer identify a rejection.
func TestSubscribeWrapsAServerRefusal(t *testing.T) {
	conn := &ackingConn{errCode: 40001, errMsg: "invalid credential"}
	c := &wecomChannel{botID: "bot-1", secret: "s"}

	err := c.subscribe(context.Background(), conn, newWSSender(conn, nil), slog.Default())
	if err == nil {
		t.Fatal("a refused subscribe returned success")
	}
	if !errors.Is(err, errSubscribeRejected) {
		t.Fatalf("subscribe() does not mark a server refusal as one: %v — the caller cannot tell it from a network failure", err)
	}
}

// The other side: an accepted handshake must not be reported as a rejection.
func TestSubscribeSucceedsOnZeroErrcode(t *testing.T) {
	conn := &ackingConn{errCode: 0}
	c := &wecomChannel{botID: "bot-1", secret: "s"}
	if err := c.subscribe(context.Background(), conn, newWSSender(conn, nil), slog.Default()); err != nil {
		t.Fatalf("an accepted subscribe failed: %v", err)
	}
}

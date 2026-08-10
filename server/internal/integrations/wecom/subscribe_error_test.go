package wecom

// subscribe_error_test.go — a handshake the server refused and a handshake
// that never reached the server call for opposite responses. One needs a
// person to go fix the installation; the other usually fixes itself. Callers
// (the Supervisor's logging today, a metrics split tomorrow) can only tell
// them apart if the rejection is identifiable.
//
// Every test here drives the real subscribe(). Building an error with
// fmt.Errorf and then asserting errors.Is on it tests errors.Is, not the
// production code: misclassifying a transport failure inside subscribe() would
// leave such a test green.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// quietLogger keeps the unrecognized-errcode warning out of the test output;
// the log is not what any of these assert on.
func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// ackingConn answers the subscribe frame with a chosen errcode, echoing the
// req_id the way the server does.
type ackingConn struct {
	errCode int
	errMsg  string
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

// failingConn is the transport half: the frame never goes out, or the ack
// never comes back.
type failingConn struct {
	writeErr error
	readErr  error
}

func (c *failingConn) WriteMessage(int, []byte) error    { return c.writeErr }
func (c *failingConn) ReadMessage() (int, []byte, error) { return 0, nil, c.readErr }
func (c *failingConn) SetReadDeadline(time.Time) error   { return nil }
func (c *failingConn) SetWriteDeadline(time.Time) error  { return nil }
func (c *failingConn) Close() error                      { return nil }

// TestSubscribeClassifiesAnAckLikeTheCredentialProbe pins the connect path to
// the same verdicts the install-time probe gives the same errcodes
// (credential_probe_test.go's table). The whole point of the split is that a
// throttle is not a credential failure; a connect-vs-auth metric built on a
// classification that disagreed with the probe's would page somebody about a
// tenant whose bot is fine.
func TestSubscribeClassifiesAnAckLikeTheCredentialProbe(t *testing.T) {
	for _, tc := range []struct {
		name    string
		errCode int
		errMsg  string
		want    error
	}{
		{name: "invalid secret is a rejection", errCode: 40001, errMsg: "invalid secret", want: ErrCredentialsRejected},
		{name: "invalid corpid is a rejection", errCode: 40013, errMsg: "invalid corpid", want: ErrCredentialsRejected},
		{name: "rate limited is not a rejection", errCode: 45009, errMsg: "api freq out of limit", want: ErrCredentialsUnverifiable},
		{name: "concurrency limited is not a rejection", errCode: 45033, errMsg: "api concurrency out of limit", want: ErrCredentialsUnverifiable},
		{name: "system error is not a rejection", errCode: -1, errMsg: "system busy", want: ErrCredentialsUnverifiable},
		{name: "unknown code is not a rejection", errCode: 999999, errMsg: "something new", want: ErrCredentialsUnverifiable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn := &ackingConn{errCode: tc.errCode, errMsg: tc.errMsg}
			c := &wecomChannel{botID: "bot-1", secret: "s"}

			err := c.subscribe(context.Background(), conn, newWSSender(conn, quietLogger()), quietLogger())
			if err == nil {
				t.Fatal("a refused subscribe returned success")
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("subscribe() classified errcode %d as %v, want %v", tc.errCode, err, tc.want)
			}
			other := ErrCredentialsRejected
			if tc.want == ErrCredentialsRejected {
				other = ErrCredentialsUnverifiable
			}
			if errors.Is(err, other) {
				t.Fatalf("subscribe() classified errcode %d as both answers at once: %v", tc.errCode, err)
			}
			// The diagnosis must survive: the errcode is what names WHICH
			// problem it is.
			if got := err.Error(); !strings.Contains(got, strconv.Itoa(tc.errCode)) || !strings.Contains(got, tc.errMsg) {
				t.Errorf("error text lost the server's diagnosis: %q", got)
			}
		})
	}
}

// TestSubscribeTransportFailuresAreNotRejections is the other direction, and
// it has to run subscribe() to hold: classifying a read failure as a rejection
// inside subscribe() is exactly the drift this is here to catch.
func TestSubscribeTransportFailuresAreNotRejections(t *testing.T) {
	readFailed := errors.New("i/o timeout")
	writeFailed := &net.OpError{Op: "write", Err: errors.New("broken pipe")}

	for _, tc := range []struct {
		name string
		conn *failingConn
		want error
	}{
		{name: "the ack never comes back", conn: &failingConn{readErr: readFailed}, want: readFailed},
		{name: "the frame never goes out", conn: &failingConn{writeErr: writeFailed}, want: writeFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &wecomChannel{botID: "bot-1", secret: "s"}

			err := c.subscribe(context.Background(), tc.conn, newWSSender(tc.conn, quietLogger()), quietLogger())
			if err == nil {
				t.Fatal("a transport failure returned success")
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("subscribe() dropped the underlying transport error: %v", err)
			}
			if errors.Is(err, ErrCredentialsRejected) {
				t.Errorf("subscribe() called a transport failure a credential rejection: %v — an operator would go chase a tenant whose bot is fine", err)
			}
		})
	}
}

// The third case: an accepted handshake must not be reported as either.
func TestSubscribeSucceedsOnZeroErrcode(t *testing.T) {
	conn := &ackingConn{errCode: 0}
	c := &wecomChannel{botID: "bot-1", secret: "s"}
	if err := c.subscribe(context.Background(), conn, newWSSender(conn, quietLogger()), quietLogger()); err != nil {
		t.Fatalf("an accepted subscribe failed: %v", err)
	}
}

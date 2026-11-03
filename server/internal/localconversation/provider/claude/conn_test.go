package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// pipeAdapter is the adapter side of a Conn under test.
type pipeAdapter struct {
	t       *testing.T
	toConn  *io.PipeWriter
	fromCon *bufio.Scanner
}

func newPipeConn(t *testing.T) (*Conn, *pipeAdapter, *outputs) {
	t.Helper()
	connReads, adapterWrites := io.Pipe()
	adapterReads, connWrites := io.Pipe()
	got := &outputs{}
	c := NewConn(connReads, connWrites, got.add)
	t.Cleanup(func() { adapterWrites.Close(); connWrites.Close() })
	return c, &pipeAdapter{t: t, toConn: adapterWrites, fromCon: bufio.NewScanner(adapterReads)}, got
}

func (a *pipeAdapter) send(line string) {
	a.t.Helper()
	if _, err := a.toConn.Write([]byte(line + "\n")); err != nil {
		a.t.Fatal(err)
	}
}

func (a *pipeAdapter) next() map[string]any {
	a.t.Helper()
	if !a.fromCon.Scan() {
		a.t.Fatalf("no request: %v", a.fromCon.Err())
	}
	var m map[string]any
	if err := json.Unmarshal(a.fromCon.Bytes(), &m); err != nil {
		a.t.Fatal(err)
	}
	return m
}

type outputs struct {
	mu  sync.Mutex
	all []Output
}

func (o *outputs) add(out Output) { o.mu.Lock(); o.all = append(o.all, out); o.mu.Unlock() }

func (o *outputs) wait(t *testing.T, n int) []Output {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		o.mu.Lock()
		if len(o.all) >= n {
			got := append([]Output(nil), o.all...)
			o.mu.Unlock()
			return got
		}
		o.mu.Unlock()
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("got fewer than %d outputs", n)
	return nil
}

func TestConnRequestCarriesVersionAndID(t *testing.T) {
	t.Parallel()

	c, a, _ := newPipeConn(t)
	result := make(chan json.RawMessage, 1)
	go func() {
		data, err := c.Request(context.Background(), "set_model", map[string]any{"model": "claude-opus-5-5"})
		if err != nil {
			t.Error(err)
		}
		result <- data
	}()
	req := a.next()
	if req["version"] != float64(1) || req["type"] != "set_model" || req["model"] != "claude-opus-5-5" || req["id"] == "" {
		t.Fatalf("request = %v", req)
	}
	a.send(`{"version":1,"type":"result","id":"` + req["id"].(string) + `","data":{"ok":true}}`)
	if got := string(<-result); got != `{"ok":true}` {
		t.Fatalf("result = %s", got)
	}
}

func TestConnReturnsStructuredErrors(t *testing.T) {
	t.Parallel()

	c, a, _ := newPipeConn(t)
	errCh := make(chan error, 1)
	go func() {
		_, err := c.Request(context.Background(), "send", map[string]any{"request_id": "r", "content": []any{}})
		errCh <- err
	}()
	req := a.next()
	a.send(`{"version":1,"type":"error","id":"` + req["id"].(string) + `","code":"not_started","message":"start first","dispatch":"not_dispatched"}`)
	var adapterErr *AdapterError
	if err := <-errCh; !errors.As(err, &adapterErr) || adapterErr.Code != "not_started" || adapterErr.Dispatch != "not_dispatched" || adapterErr.Message != "start first" {
		t.Fatalf("err = %#v", err)
	}
}

func TestConnRoutesUnsolicitedFramesToHandler(t *testing.T) {
	t.Parallel()

	c, a, got := newPipeConn(t)
	a.send(`{"version":1,"type":"ready","sdk_version":"0.3.310"}`)
	ready, err := c.WaitReady(context.Background())
	if err != nil || ready.SDKVersion != "0.3.310" {
		t.Fatalf("ready = %+v, %v", ready, err)
	}
	a.send(`{"version":1,"type":"sdk_message","query_generation":2,"message":{"type":"assistant","message":{"content":[]}}}`)
	a.send(`{"version":1,"type":"error","code":"query_ended","message":"ended"}`)
	a.send(`{"version":1,"type":"interaction_request","interaction_id":"ix","kind":"permission","request":{}}`)

	outs := got.wait(t, 3)
	if outs[0].Type != "sdk_message" || outs[0].QueryGeneration != 2 || !strings.Contains(string(outs[0].Message), `"assistant"`) {
		t.Fatalf("sdk output = %+v", outs[0])
	}
	if outs[1].Type != "error" || outs[1].Error.Code != "query_ended" {
		t.Fatalf("error output = %+v", outs[1])
	}
	if outs[2].Type != "interaction_request" || !strings.Contains(string(outs[2].Interaction), `"ix"`) {
		t.Fatalf("interaction output = %+v", outs[2])
	}
}

func TestConnFailsOnProtocolViolations(t *testing.T) {
	t.Parallel()

	for name, line := range map[string]string{
		"wrong version": `{"version":2,"type":"ready","sdk_version":"x"}`,
		"unknown type":  `{"version":1,"type":"surprise"}`,
		"not json":      `{"version":1,`,
		"bad sdk frame": `{"version":1,"type":"sdk_message","message":"text"}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c, a, _ := newPipeConn(t)
			pending := make(chan error, 1)
			go func() {
				_, err := c.Request(context.Background(), "close", nil)
				pending <- err
			}()
			a.next()
			a.send(line)
			<-c.Done()
			if !errors.Is(c.Err(), ErrProtocol) {
				t.Fatalf("Err = %v, want ErrProtocol", c.Err())
			}
			if err := <-pending; !errors.Is(err, ErrProtocol) {
				t.Fatalf("pending request err = %v", err)
			}
		})
	}
}

func TestConnFailsPendingWhenOutputEnds(t *testing.T) {
	t.Parallel()

	c, a, _ := newPipeConn(t)
	errCh := make(chan error, 1)
	go func() {
		_, err := c.Request(context.Background(), "close", nil)
		errCh <- err
	}()
	a.next()
	a.toConn.Close()
	if err := <-errCh; !errors.Is(err, ErrClosed) {
		t.Fatalf("err = %v, want ErrClosed", err)
	}
	if _, err := c.Request(context.Background(), "close", nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("request after close = %v", err)
	}
}

func TestConnRequestHonorsContext(t *testing.T) {
	t.Parallel()

	c, a, _ := newPipeConn(t)
	go a.next()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := c.Request(ctx, "interrupt", nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
}

func TestClaimReasonReadsTheSDKReason(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		err  error
		want string
	}{
		{&AdapterError{Code: CodeNotClaimed, Message: "option_not_applied: model"}, "option_not_applied"},
		{&AdapterError{Code: CodeNotClaimed, Message: "project_settings_not_claimable"}, "project_settings_not_claimable"},
		{&AdapterError{Code: "sdk_error", Message: "option_not_applied: model"}, ""},
		{errors.New("not_claimed: closed"), ""},
	} {
		if got := ClaimReason(c.err); got != c.want {
			t.Errorf("ClaimReason(%v) = %q, want %q", c.err, got, c.want)
		}
	}
}

func TestMCPSetFailedOnlyOnReportedErrors(t *testing.T) {
	t.Parallel()

	if MCPSetFailed(json.RawMessage(`{"added":["multica"],"removed":[],"errors":{}}`)) {
		t.Error("no errors reported as a failure")
	}
	if !MCPSetFailed(json.RawMessage(`{"added":[],"removed":[],"errors":{"multica":"refused"}}`)) {
		t.Error("an unbound server was not reported")
	}
}

package wecom

// send_verdict_test.go — a push used to return nil as soon as the bytes left
// the process. WeCom's answer comes back in a separate ack frame, so a frame
// the server refused was indistinguishable from one it accepted.

import (
	"errors"
	"testing"
	"time"
)

func TestSendTextReportsAServerRefusal(t *testing.T) {
	conn := &recordingConn{refuseCode: 45009, refuseMsg: "rate limit"}
	sender := conn.autoAck(newWSSender(conn, nil))
	// 45009 is a throttle, so this send is retried once (rate_limit.go). The
	// retry is not what this test is about, but the two seconds it waits for
	// by default would be: shortened, so the assertion below is the only thing
	// the test spends time on.
	sender.retryBackoff = time.Millisecond

	err := sender.sendText("CHAT", chatTypeSingleInt, "hello")
	if err == nil {
		t.Fatal("a send WeCom refused reported success — the caller records a delivery that never happened")
	}
	var apiErr *wecomAPIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error is not a *wecomAPIError, so a caller cannot tell a permanent refusal from a transient one: %v", err)
	}
	if apiErr.Code != 45009 {
		t.Errorf("errcode = %d, want 45009", apiErr.Code)
	}
	if apiErr.Cmd != cmdSendMsg {
		t.Errorf("cmd = %q, want %q", apiErr.Cmd, cmdSendMsg)
	}
}

func TestSendTextSucceedsOnAZeroErrcode(t *testing.T) {
	conn := &recordingConn{}
	sender := conn.autoAck(newWSSender(conn, nil))

	if err := sender.sendText("CHAT", chatTypeSingleInt, "hello"); err != nil {
		t.Fatalf("an accepted send reported failure: %v", err)
	}
	conn.mu.Lock()
	n := len(conn.frames)
	conn.mu.Unlock()
	if n != 1 {
		t.Fatalf("wrote %d frames, want 1", n)
	}
}

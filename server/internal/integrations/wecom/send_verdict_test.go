package wecom

// send_verdict_test.go — a push used to return nil as soon as the bytes left
// the process. WeCom's answer comes back in a separate ack frame, so a frame
// the server refused was indistinguishable from one it accepted.

import (
	"errors"
	"testing"
)

func TestSendTextReportsAServerRefusal(t *testing.T) {
	conn := &recordingConn{refuseCode: 45009, refuseMsg: "rate limit"}
	sender := conn.autoAck(newWSSender(conn, nil))

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

// A verdict that never arrives must not be reported as a refusal: the message
// may well have been delivered, so the two call for opposite responses.
func TestSendTextDistinguishesALostAckFromARefusal(t *testing.T) {
	conn := &recordingConn{} // no autoAck: nothing ever answers
	sender := newWSSender(conn, nil)

	err := sender.sendText("CHAT", chatTypeSingleInt, "hello")
	if !errors.Is(err, errAckTimeout) {
		t.Fatalf("a lost ack reported as %v, want errAckTimeout", err)
	}
	var apiErr *wecomAPIError
	if errors.As(err, &apiErr) {
		t.Error("a lost ack was reported as a server refusal")
	}
	// And the same fact stated where a caller weighing another attempt reads
	// it. The frame went out, so this message may be in the chat; every reader
	// that decides whether to say the words again looks for this mark rather
	// than for a name it has to have met before (answerRetryCause, outbound.go).
	if !errors.Is(err, errWordsMayBeOnScreen) {
		t.Error("a lost ack was not marked errWordsMayBeOnScreen — a caller reading no mark as " +
			"'nothing reached the user' puts these words on the screen a second time")
	}
}

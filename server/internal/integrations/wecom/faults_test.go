//go:build wecomfaults

package wecom

// faults_test.go — the switch that only exists in this build, checked the one
// way it can be: that arming it produces the failure it says it does, and that
// it lets go afterwards.
//
// Run with the tag the code needs:
//
//	go test -tags wecomfaults -race -run Fault ./internal/integrations/wecom/

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestFaultDeadSocketOnSealFailsTheSealAndTheFallbackBehindIt is the whole
// point of the fault: a delivery is two sends, and a switch that broke only the
// first would leave the second to succeed and produce the outcome instead of the
// failure.
func TestFaultDeadSocketOnSealFailsTheSealAndTheFallbackBehindIt(t *testing.T) {
	defer SetFaults("")
	if armed := SetFaults(" Dead_Socket_On_Seal , nonsense "); len(armed) != 1 || armed[0] != FaultDeadSocketOnSeal {
		t.Fatalf("SetFaults armed %q, want just %q — the name is matched case- and space-insensitively "+
			"and anything unknown is ignored", armed, FaultDeadSocketOnSeal)
	}

	conn := &bubbleConn{}
	s := newWSSender(conn, nil)
	conn.sender = s
	ctx := context.Background()

	// The opening frame is not a seal, so it is not what arms the outage.
	if err := s.respondStream(ctx, "REQ-1", "STREAM-1", streamThinkingPlaceholder, false); err != nil {
		t.Fatalf("the opening frame was refused before anything was armed: %v", err)
	}

	err := s.respondStream(ctx, "REQ-1", "STREAM-1", "the answer", true)
	if !errors.Is(err, errFrameNotOnTheWire) {
		t.Fatalf("the closing frame reported %v, want the write that did not reach the wire — "+
			"everything downstream reads that name and nothing else", err)
	}
	if !deliveryCanBeRepeated(err) || !bubbleSurvivedTheFailure(err) {
		t.Fatalf("the manufactured failure is not the one a real dead socket produces: "+
			"repeatable=%v bubble kept=%v", deliveryCanBeRepeated(err), bubbleSurvivedTheFailure(err))
	}
	// And the plain message the closing frame falls back to goes the same way,
	// which is what makes the delivery fail rather than merely change route.
	if err := s.sendTextCtx(ctx, "CHAT_1", chatTypeSingleInt, "the answer"); !errors.Is(err, errFrameNotOnTheWire) {
		t.Fatalf("the fallback message reported %v, want the same dead socket", err)
	}
	if n := len(conn.streamFrames(t)); n != 1 {
		t.Fatalf("%d stream frames reached the conn, want 1 — the frame the outage refused must "+
			"not be written", n)
	}
}

// TestFaultDeadSocketOnSealDisarmsItself pins the two halves of "one-shot": the
// arm is spent by the first seal, and the outage it opened ends on its own.
// Neither can be left running for a session nobody is watching any more.
func TestFaultDeadSocketOnSealDisarmsItself(t *testing.T) {
	defer SetFaults("")
	SetFaults(FaultDeadSocketOnSeal)

	if err := faultDeadSocketRefusesWrite(); err != nil {
		t.Fatalf("writes were already failing before any seal: %v", err)
	}
	faultDeadSocketOnSeal(nil, true)
	if err := faultDeadSocketRefusesWrite(); err == nil {
		t.Fatal("the seal did not open the outage")
	}

	faultMu.Lock()
	deadUntil = time.Now().Add(-time.Millisecond)
	faultMu.Unlock()
	if err := faultDeadSocketRefusesWrite(); err != nil {
		t.Fatalf("writes are still failing past the window: %v", err)
	}

	// A second seal finds nothing armed.
	faultDeadSocketOnSeal(nil, true)
	if err := faultDeadSocketRefusesWrite(); err != nil {
		t.Fatalf("a second closing frame reopened the outage: %v", err)
	}
}

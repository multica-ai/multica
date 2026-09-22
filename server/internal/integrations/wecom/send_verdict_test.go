package wecom

// send_verdict_test.go — a push used to return nil as soon as the bytes left
// the process. WeCom's answer comes back in a separate ack frame, so a frame
// the server refused was indistinguishable from one it accepted.

import (
	"context"
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

// A send whose context was already over when it began is the one failure on
// this path that is certain, and all three classifiers used to call it
// uncertain.
//
// request checks the context before it mints a req_id, before it registers a
// waiter and before it builds a frame (ws_sender.go), so at that point NOTHING
// has left this process. It returned a bare ctx.Err() there, which is also
// what the wait for a verdict AFTER the write used to return — and every
// classifier reads a bare context error as "the frame may be in front of the
// person already", because for the post-write case that is the true reading.
// So a send that never started filed as "outcome unknown": the direct path
// counted it unconfirmed, which is the one outcome nobody may resend; the
// relay settled its claim and stopped re-offering the frame; and the media
// path told the user its file might have arrived. The answer is lost and the
// party whose job is to try again is told not to.
//
// It is reachable in production and not only from a test. chatLocks.acquire's
// blocking select has two ready cases the moment the lock frees and the
// delivery budget expires together, and Go picks among ready cases at random,
// so it can hand back the chat's turn together with a context that is already
// over — and this check is the next thing that runs. perform gives every
// claimed delivery a DeliveryBudget of its own (relay_outbound.go), so that
// coincidence gets one chance per offer rather than one per reply.
//
// Both pushes are here because both reach the same check by the same route:
// sendTextCtx and sendMedia take the chat's turn and then call sendMsgFrame,
// which calls request.
//
// REVERSE VERIFICATION: return the bare ctx.Err() from request's pre-write
// check again and every assertion below flips — provablyNotSent false,
// unconfirmedReason "interrupted", sendOutcome unknown, on both pushes.
func TestASendThatNeverStartedIsProvablyNotSent(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		send func(*wsSender, context.Context) error
	}{
		{"a text push", func(s *wsSender, ctx context.Context) error {
			return s.sendTextCtx(ctx, "CHAT_1", chatTypeSingleInt, "答案")
		}},
		{"a media push", func(s *wsSender, ctx context.Context) error {
			return s.sendMedia(ctx, "CHAT_1", chatTypeSingleInt, mediaSend{
				Kind: mediaTypeFile, MediaID: "MEDIA_1",
			})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			conn := &recordingConn{}
			sender := conn.autoAck(newWSSender(conn, testLogger()))

			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			// Nobody holds this chat, and acquire takes a free chat without
			// consulting the context at all (ws_sender.go) — so the failure
			// below is request's pre-write check and nothing else.
			err := tc.send(sender, ctx)

			if err == nil {
				t.Fatal("a send on a context that was already over reported success")
			}
			if got := conn.sendFrames(); len(got) != 0 {
				t.Fatalf("%d frame(s) reached the wire: %v — nothing here is a send that never started", len(got), got)
			}
			// The relay's classifier: false settles the claim and stops the
			// re-offer chain on an answer the socket never saw.
			if !provablyNotSent(err) {
				t.Errorf("provablyNotSent(%v) = false, want true", err)
			}
			// The direct path's: a reason here counts the reply unconfirmed,
			// which tells an operator not to resend a message nobody sent.
			if got := unconfirmedReason(err); got != "" {
				t.Errorf("unconfirmedReason(%v) = %q, want \"\" — %q says the message may be on the person's screen", err, got, got)
			}
			// The media path's: unknown is never retried and is described to
			// the user in words that hold either way.
			if got := sendOutcome(err); got != deliveryDefinitelyFailed {
				t.Errorf("sendOutcome(%v) = %v, want %v", err, got, deliveryDefinitelyFailed)
			}
			// And the cause is still in there, which two readers need. The log
			// line wants to say what ended the send; and sendMsgFrame's
			// closing switch answers a throttled retry that was cut short
			// before its second write with the FIRST attempt's stated refusal,
			// through the context arm this error has to keep matching
			// (rate_limit.go). Dropping the cause would send that arm to its
			// default and report a cancellation over a definite refusal.
			if !errors.Is(err, context.Canceled) {
				t.Errorf("errors.Is(%v, context.Canceled) = false — the cause is what the log line "+
					"and sendMsgFrame's closing switch read", err)
			}
		})
	}
}

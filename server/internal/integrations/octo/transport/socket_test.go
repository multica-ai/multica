package transport

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// TestSendMessageResult_NumericMessageID is the regression test for the bug
// where the Octo server returns message_id as a bare int64 number (octo-lib
// MsgSendResp) but the client decoded into a string field and errored. Both the
// numeric and string wire forms must decode to the same string.
func TestSendMessageResult_NumericMessageID(t *testing.T) {
	cases := []struct {
		name string
		json string
		want string
	}{
		{"bare number (server form)", `{"message_id":1234567890123456789,"message_seq":42,"client_msg_no":"abc"}`, "1234567890123456789"},
		{"string form", `{"message_id":"1234567890123456789","message_seq":42,"client_msg_no":"abc"}`, "1234567890123456789"},
		{"small number", `{"message_id":7,"message_seq":1,"client_msg_no":"x"}`, "7"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var r SendMessageResult
			if err := json.Unmarshal([]byte(c.json), &r); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if r.MessageID != c.want {
				t.Errorf("MessageID = %q, want %q", r.MessageID, c.want)
			}
			if r.MessageSeq == 0 {
				t.Errorf("MessageSeq not decoded: %+v", r)
			}
		})
	}
}

// TestConnect_DoubleStartGuard verifies a second Connect while a manager is
// running returns ErrAlreadyStarted instead of spawning a racing manager.
func TestConnect_DoubleStartGuard(t *testing.T) {
	s := NewSocket(SocketOptions{
		// An unroutable ws URL keeps the manager in its dial/backoff loop so the
		// started flag stays set for the duration of the test.
		WSURL: "ws://127.0.0.1:1/never",
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := s.Connect(ctx); err != nil {
		t.Fatalf("first Connect: %v", err)
	}
	if err := s.Connect(ctx); err != ErrAlreadyStarted {
		t.Errorf("second Connect = %v, want ErrAlreadyStarted", err)
	}
	s.Disconnect()
}

// TestDisconnect_Idempotent verifies Disconnect can be called repeatedly and
// before Connect without panicking.
func TestDisconnect_Idempotent(t *testing.T) {
	s := NewSocket(SocketOptions{WSURL: "ws://127.0.0.1:1/never"})
	s.Disconnect() // before Connect
	ctx := context.Background()
	if err := s.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	s.Disconnect()
	s.Disconnect() // again
}

// TestMarkDisconnect_FiresOnceWhenConnected verifies OnDisconnected fires only
// when the connection had reached the connected state, and exactly once.
func TestMarkDisconnect_FiresOnceWhenConnected(t *testing.T) {
	var disconnects int
	s := NewSocket(SocketOptions{
		OnDisconnected: func() { disconnects++ },
	})

	// Not connected → no OnDisconnected.
	s.markDisconnect(time.Time{})
	if disconnects != 0 {
		t.Fatalf("OnDisconnected fired while never connected: %d", disconnects)
	}

	// Connected → fires once, and the flag flips so a second call is a no-op.
	s.connected.Store(true)
	s.markDisconnect(time.Time{})
	s.markDisconnect(time.Time{})
	if disconnects != 1 {
		t.Errorf("OnDisconnected fired %d times, want 1", disconnects)
	}
}

// TestNextBackoff_Growth verifies the reconnect backoff: each call increments
// the attempt counter, the returned delay stays within the ±25% jitter band of
// the expected exponential value, and large attempt counts are clamped to
// reconnectMaxDelay (the math.Pow overflow guard) instead of overflowing.
func TestNextBackoff_Growth(t *testing.T) {
	s := NewSocket(SocketOptions{})

	// Early attempts: delay ≈ base * 2^attempt, within [0.75x, 1.25x].
	for attempt := 0; attempt < 4; attempt++ {
		if s.reconnectAttempts != attempt {
			t.Fatalf("before call: reconnectAttempts = %d, want %d", s.reconnectAttempts, attempt)
		}
		base := float64(reconnectBaseDelay) * pow2(attempt)
		expected := base
		if expected > float64(reconnectMaxDelay) {
			expected = float64(reconnectMaxDelay)
		}
		got := float64(s.nextBackoff())
		lo, hi := expected*0.75, expected*1.25
		if got < lo || got > hi {
			t.Errorf("attempt %d: backoff %v outside [%v, %v]", attempt, time.Duration(got), time.Duration(lo), time.Duration(hi))
		}
		if s.reconnectAttempts != attempt+1 {
			t.Errorf("attempt %d: reconnectAttempts not incremented (= %d)", attempt, s.reconnectAttempts)
		}
	}

	// A very large attempt count must clamp to ~reconnectMaxDelay (±25%) rather
	// than overflow float64/Duration.
	s.reconnectAttempts = 100
	got := s.nextBackoff()
	lo := time.Duration(float64(reconnectMaxDelay) * 0.75)
	hi := time.Duration(float64(reconnectMaxDelay) * 1.25)
	if got < lo || got > hi {
		t.Errorf("large-attempt backoff %v outside clamp band [%v, %v]", got, lo, hi)
	}
	if got <= 0 {
		t.Errorf("backoff must stay positive, got %v", got)
	}
}

// pow2 returns 2^n as a float64 without importing math into the test.
func pow2(n int) float64 {
	r := 1.0
	for i := 0; i < n; i++ {
		r *= 2
	}
	return r
}

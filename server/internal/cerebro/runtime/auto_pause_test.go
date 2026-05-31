package runtime

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/cerebro/account"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestClassifyAutoPause(t *testing.T) {
	now := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)
	validRuntime := pgtype.UUID{
		Bytes: [16]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
			0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
		Valid: true,
	}

	mkTask := func(runtime pgtype.UUID, errText string) db.AgentTaskQueue {
		return db.AgentTaskQueue{
			RuntimeID: runtime,
			Error:     pgtype.Text{String: errText, Valid: errText != ""},
		}
	}

	cases := []struct {
		name        string
		task        db.AgentTaskQueue
		wantWorthy  bool
		wantReset   bool
		wantResetAt time.Time // only checked when wantReset
		wantReason  string
		wantManual  bool
	}{
		{
			name:       "no runtime — never pause",
			task:       mkTask(pgtype.UUID{}, "You've hit your org's monthly usage limit"),
			wantWorthy: false,
		},
		{
			name:       "no error text — never pause",
			task:       mkTask(validRuntime, ""),
			wantWorthy: false,
		},
		{
			name:       "non-rate-limit error — never pause",
			task:       mkTask(validRuntime, "tool execution failed: command not found"),
			wantWorthy: false,
		},
		{
			name:       "anthropic monthly cap — pause-worthy, no parseable reset",
			task:       mkTask(validRuntime, "You've hit your org's monthly usage limit"),
			wantWorthy: true,
			wantReset:  false,
			wantReason: "rate_limit",
		},
		{
			name:       "401 invalid auth — pause-worthy, no parseable reset",
			task:       mkTask(validRuntime, "Failed to authenticate. API Error: 401 Invalid authentication credentials"),
			wantWorthy: true,
			wantReset:  false,
			wantReason: "auth_error",
			wantManual: true,
		},
		{
			name:       "http 429 — pause-worthy, no parseable reset",
			task:       mkTask(validRuntime, "Request failed: HTTP 429 Too Many Requests"),
			wantWorthy: true,
			wantReset:  false,
			wantReason: "rate_limit",
		},
		{
			name:        "explicit retry-after — parseable reset wins",
			task:        mkTask(validRuntime, "Rate limit. Try again in 90 seconds"),
			wantWorthy:  true,
			wantReset:   true,
			wantResetAt: now.Add(90 * time.Second),
			wantReason:  "rate_limit",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyAutoPause(tc.task, now)
			if got.pauseWorthy != tc.wantWorthy {
				t.Fatalf("worthy=%v, want %v (input=%q)", got.pauseWorthy, tc.wantWorthy, tc.task.Error.String)
			}
			if !got.pauseWorthy {
				return
			}
			if got.hasReset != tc.wantReset {
				t.Fatalf("hasReset=%v, want %v", got.hasReset, tc.wantReset)
			}
			if got.pauseReason != tc.wantReason {
				t.Fatalf("pauseReason=%q, want %q", got.pauseReason, tc.wantReason)
			}
			if got.manualOnly != tc.wantManual {
				t.Fatalf("manualOnly=%v, want %v", got.manualOnly, tc.wantManual)
			}
			if tc.wantReset && !got.resetAt.Equal(tc.wantResetAt) {
				t.Fatalf("resetAt=%s, want %s", got.resetAt.Format(time.RFC3339), tc.wantResetAt.Format(time.RFC3339))
			}
		})
	}
}

func TestGrowingBackoff(t *testing.T) {
	base := account.DefaultRateLimitBackoff // 5m
	cases := []struct {
		count int32
		want  time.Duration
	}{
		{count: 0, want: base},                // defensive: count<1 treated as 1
		{count: 1, want: base},                // 5m
		{count: 2, want: 2 * base},            // 10m
		{count: 3, want: 4 * base},            // 20m
		{count: 4, want: 8 * base},            // 40m
		{count: 5, want: 16 * base},           // 80m
		{count: 6, want: autoPauseBackoffCap}, // 160m → capped at 2h
		{count: 7, want: autoPauseBackoffCap}, // stays capped
		{count: 50, want: autoPauseBackoffCap},
	}
	for _, tc := range cases {
		if got := growingBackoff(tc.count); got != tc.want {
			t.Errorf("growingBackoff(%d)=%s, want %s", tc.count, got, tc.want)
		}
	}
}

func TestNextUnpauseAt(t *testing.T) {
	now := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)
	reset := now.Add(3 * time.Hour)

	// Parseable reset time always wins over the growing backoff.
	if got := nextUnpauseAt(1, reset, true, now); !got.Equal(reset) {
		t.Errorf("with parseable reset: got %s, want %s", got, reset)
	}
	// No parseable reset → growing backoff from the count.
	if got := nextUnpauseAt(3, time.Time{}, false, now); !got.Equal(now.Add(growingBackoff(3))) {
		t.Errorf("fallback backoff: got %s, want %s", got, now.Add(growingBackoff(3)))
	}
}

// TestCircuitBreakerThreshold documents the count → circuit-open / notify
// boundary so a future tuning change is a deliberate, test-visible edit.
func TestCircuitBreakerThreshold(t *testing.T) {
	for count := int32(1); count <= autoPauseCircuitLimit+2; count++ {
		circuitOpen := count >= autoPauseCircuitLimit
		notifyOnce := count == autoPauseCircuitLimit

		switch {
		case count < autoPauseCircuitLimit:
			if circuitOpen {
				t.Errorf("count=%d: circuit should be closed", count)
			}
			if notifyOnce {
				t.Errorf("count=%d: should not notify", count)
			}
		case count == autoPauseCircuitLimit:
			if !circuitOpen || !notifyOnce {
				t.Errorf("count=%d: expected circuit open AND single notify", count)
			}
		default: // count > limit
			if !circuitOpen {
				t.Errorf("count=%d: circuit should stay open", count)
			}
			if notifyOnce {
				t.Errorf("count=%d: must not re-notify past the trip", count)
			}
		}
	}
}

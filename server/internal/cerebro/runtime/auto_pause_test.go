package runtime

import (
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
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
		name          string
		task          db.AgentTaskQueue
		wantWorthy    bool
		wantReset     bool
		wantResetAt   time.Time // only checked when wantReset
		wantReason    string
		wantManual    bool
		wantFlatRetry time.Duration // 0 = growing backoff; >0 = fixed re-check
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
			// FIR-1889: the raisable monthly SPEND cap gets a flat hourly
			// re-check instead of the 2h/4h/6h give-up backoff.
			name:          "anthropic monthly spend limit — flat hourly re-check",
			task:          mkTask(validRuntime, "You've hit your monthly spend limit · raise it at claude.ai/settings/usage"),
			wantWorthy:    true,
			wantReset:     false,
			wantReason:    "rate_limit",
			wantFlatRetry: time.Hour,
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
		{
			name:       "gateway EOF — pause-worthy as runtime_recovery",
			task:       mkTask(validRuntime, `call gateway: Post "http://firtal-data-registry.internal:3000/api/ai/proxy/v1/chat/completions": EOF`),
			wantWorthy: true,
			wantReset:  false,
			wantReason: "runtime_recovery",
		},
		{
			name:       "gateway connection reset — pause-worthy as runtime_recovery",
			task:       mkTask(validRuntime, "call anthropic gateway: Post \"http://firtal-data-registry.internal:3000/api/ai/proxy/v1/messages\": read tcp: connection reset by peer"),
			wantWorthy: true,
			wantReset:  false,
			wantReason: "runtime_recovery",
		},
		{
			name:       "non-gateway EOF — not pause-worthy (gating)",
			task:       mkTask(validRuntime, `read tcp 10.0.0.1:443: EOF`),
			wantWorthy: false,
		},
		{
			name:       "gateway HTTP 503 (no transport drop) — not gateway-transient",
			task:       mkTask(validRuntime, "gateway returned HTTP 503: service unavailable"),
			wantWorthy: false,
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
			if got.flatRetry != tc.wantFlatRetry {
				t.Fatalf("flatRetry=%s, want %s", got.flatRetry, tc.wantFlatRetry)
			}
			if tc.wantReset && !got.resetAt.Equal(tc.wantResetAt) {
				t.Fatalf("resetAt=%s, want %s", got.resetAt.Format(time.RFC3339), tc.wantResetAt.Format(time.RFC3339))
			}
		})
	}
}

func TestGrowingBackoff(t *testing.T) {
	cases := []struct {
		count int32
		want  time.Duration
	}{
		{count: 0, want: 2 * time.Hour},  // defensive: count<1 treated as 1
		{count: 1, want: 2 * time.Hour},  // first retry
		{count: 2, want: 4 * time.Hour},  // second retry
		{count: 3, want: 6 * time.Hour},  // third retry
		{count: 4, want: 6 * time.Hour},  // circuit-open callers ignore this
		{count: 50, want: 6 * time.Hour}, // stays capped
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
		circuitOpen := circuitOpenForAutoPause(count, autoPauseDecision{pauseWorthy: true, pauseReason: "rate_limit"})
		notifyOnce := count == autoPauseCircuitLimit

		switch {
		case count < autoPauseCircuitLimit:
			if circuitOpen {
				t.Errorf("count=%d: circuit should be closed", count)
			}
			if notifyOnce {
				t.Errorf("count=%d: should not post issue comment", count)
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

// TestFlatRetryNeverOpensCircuit documents FIR-1889: a raisable monthly spend
// cap re-checks hourly forever and must never trip the circuit breaker, even
// well past the count that would open it for a generic no-reset rate limit.
func TestFlatRetryNeverOpensCircuit(t *testing.T) {
	decision := autoPauseDecision{
		pauseWorthy: true,
		pauseReason: "rate_limit",
		flatRetry:   time.Hour,
	}
	for count := int32(1); count <= autoPauseCircuitLimit+5; count++ {
		if circuitOpenForAutoPause(count, decision) {
			t.Fatalf("count=%d: flat-retry spend cap must keep the circuit closed", count)
		}
	}
}

func TestCircuitBreakerRequiresNoResetTime(t *testing.T) {
	for count := int32(1); count <= autoPauseCircuitLimit+2; count++ {
		decision := autoPauseDecision{
			pauseWorthy: true,
			hasReset:    true,
			resetAt:     time.Now().Add(time.Hour),
			pauseReason: "rate_limit",
		}
		if circuitOpenForAutoPause(count, decision) {
			t.Fatalf("count=%d: parseable provider reset time must keep auto-resume scheduled", count)
		}
	}
}

func TestCircuitBreakerCommentMentionsRuntimeOwner(t *testing.T) {
	task := db.AgentTaskQueue{
		ID: pgtype.UUID{
			Bytes: [16]byte{0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70, 0x80,
				0x90, 0xa0, 0xb0, 0xc0, 0xd0, 0xe0, 0xf0, 0x01},
			Valid: true,
		},
		Error: pgtype.Text{String: "You've hit your monthly spend limit", Valid: true},
	}
	ownerMention := "[@Runtime owner](mention://member/11111111-1111-1111-1111-111111111111)"
	body := circuitBreakerAnalysisCommentBody(task, autoPauseDecision{title: "Usage or rate limit reached"}, ownerMention)

	if want := "mention://member/11111111-1111-1111-1111-111111111111"; !strings.Contains(body, want) {
		t.Fatalf("comment body missing owner mention %q:\n%s", want, body)
	}
	if !strings.Contains(body, "2 hours, 4 hours, and 6 hours") {
		t.Fatalf("comment body missing retry analysis:\n%s", body)
	}
	if !strings.Contains(body, "You've hit your monthly spend limit") {
		t.Fatalf("comment body missing redacted last error:\n%s", body)
	}
}

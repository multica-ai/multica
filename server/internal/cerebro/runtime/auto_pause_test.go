package runtime

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/cerebro/account"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestShouldAutoPause(t *testing.T) {
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
		name      string
		task      db.AgentTaskQueue
		wantPause bool
		wantAt    time.Time // only checked when wantPause
	}{
		{
			name:      "no runtime — never pause",
			task:      mkTask(pgtype.UUID{}, "You've hit your org's monthly usage limit"),
			wantPause: false,
		},
		{
			name:      "no error text — never pause",
			task:      mkTask(validRuntime, ""),
			wantPause: false,
		},
		{
			name:      "non-rate-limit error — never pause",
			task:      mkTask(validRuntime, "tool execution failed: command not found"),
			wantPause: false,
		},
		{
			name:      "anthropic monthly cap — pause for 5min default",
			task:      mkTask(validRuntime, "You've hit your org's monthly usage limit"),
			wantPause: true,
			wantAt:    now.Add(account.DefaultRateLimitBackoff),
		},
		{
			name:      "401 invalid auth — pause for 5min default",
			task:      mkTask(validRuntime, "Failed to authenticate. API Error: 401 Invalid authentication credentials"),
			wantPause: true,
			wantAt:    now.Add(account.DefaultRateLimitBackoff),
		},
		{
			name:      "openai insufficient_quota — pause for 5min default",
			task:      mkTask(validRuntime, `{"error":{"code":"insufficient_quota"}}`),
			wantPause: true,
			wantAt:    now.Add(account.DefaultRateLimitBackoff),
		},
		{
			name:      "explicit retry-after wins over default",
			task:      mkTask(validRuntime, "Rate limit. Try again in 90 seconds"),
			wantPause: true,
			wantAt:    now.Add(90 * time.Second),
		},
		{
			name:      "http 429 — pause for 5min default",
			task:      mkTask(validRuntime, "Request failed: HTTP 429 Too Many Requests"),
			wantPause: true,
			wantAt:    now.Add(account.DefaultRateLimitBackoff),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := shouldAutoPause(tc.task, now)
			if ok != tc.wantPause {
				t.Fatalf("pause=%v, want %v (input=%q)", ok, tc.wantPause, tc.task.Error.String)
			}
			if !ok {
				return
			}
			if !got.Equal(tc.wantAt) {
				t.Fatalf("got unpauseAt=%s, want %s", got.Format(time.RFC3339), tc.wantAt.Format(time.RFC3339))
			}
		})
	}
}

// CEREBRO-PATCH(daemon-client-account-test): FIR-3118 — a Codex Pro plan only
// exposes the weekly window, so the weekly number must still reach the legacy
// usage_window_pct column that coordinator agents and runtime cards read.
package daemon

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	cerebroaccount "github.com/multica-ai/multica/server/internal/cerebro/account"
)

func TestReportAccountUsageWindowsMirrorsTightestWindow(t *testing.T) {
	pct := func(v float32) *float32 { return &v }
	resets := time.Date(2026, 7, 19, 19, 35, 14, 0, time.UTC)

	tests := []struct {
		name string
		snap cerebroaccount.OAuthUsageSnapshot
		want map[string]any
	}{
		{
			name: "weekly only mirrors the week and clears the 5h window",
			snap: cerebroaccount.OAuthUsageSnapshot{SevenDayPct: pct(52), SevenDayResetsAt: &resets},
			want: map[string]any{
				"usage_5h_pct":       nil,
				"usage_5h_resets_at": nil,
				"usage_7d_pct":       float64(52),
				"usage_7d_resets_at": resets.Format(time.RFC3339),
				"usage_window_pct":   float64(52),
			},
		},
		{
			name: "both windows mirror the 5h window",
			snap: cerebroaccount.OAuthUsageSnapshot{FiveHourPct: pct(7), SevenDayPct: pct(28)},
			want: map[string]any{
				"usage_5h_pct":       float64(7),
				"usage_5h_resets_at": nil,
				"usage_7d_pct":       float64(28),
				"usage_7d_resets_at": nil,
				"usage_window_pct":   float64(7),
			},
		},
		{
			name: "5h only mirrors the 5h window and clears the week",
			snap: cerebroaccount.OAuthUsageSnapshot{FiveHourPct: pct(11)},
			want: map[string]any{
				"usage_5h_pct":       float64(11),
				"usage_5h_resets_at": nil,
				"usage_7d_pct":       nil,
				"usage_7d_resets_at": nil,
				"usage_window_pct":   float64(11),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				if err := json.Unmarshal(b, &got); err != nil {
					t.Errorf("unmarshal body: %v", err)
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()

			c := NewClient(srv.URL)
			if err := c.ReportAccountUsageWindows(context.Background(), "acct-1", tt.snap); err != nil {
				t.Fatalf("ReportAccountUsageWindows: %v", err)
			}
			for k, want := range tt.want {
				if got[k] != want {
					t.Errorf("body[%q] = %v, want %v", k, got[k], want)
				}
			}
			if len(got) != len(tt.want) {
				t.Errorf("body = %v, want exactly the keys %v", got, tt.want)
			}
		})
	}
}

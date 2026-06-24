package handler

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestLLMLimitStatusBuildsLegacyWindowsFromSnapshots(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nexai.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE llm_usage_snapshots (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			recorded_at DATETIME NOT NULL,
			provider TEXT NOT NULL,
			window_type TEXT NOT NULL,
			used_pct REAL NOT NULL,
			reset_at DATETIME NOT NULL
		);
		INSERT INTO llm_usage_snapshots (recorded_at, provider, window_type, used_pct, reset_at) VALUES
			('2026-06-11 13:30:04', 'claude', '5h', 58.0, '2026-06-11 16:30:00'),
			('2026-06-11 13:30:04', 'claude', '7d', 45.0, '2026-06-18 15:00:00'),
			('2026-06-11 13:30:04', 'openai', '5h', 36.0, '2026-06-11 17:34:48'),
			('2026-06-11 13:30:04', 'openai', '7d', 97.0, '2026-06-18 12:34:48'),
			('2026-06-11 13:00:03', 'claude', '5h', 10.0, '2026-06-11 15:00:00');
	`)
	if err != nil {
		t.Fatal(err)
	}

	rows, err := readLLMSnapshots(path)
	if err != nil {
		t.Fatal(err)
	}
	windows := buildLLMWindows(rows)

	claude5h := findWindow(t, windows, "claude", "5h")
	if claude5h.UsedPct != 58.0 || claude5h.RemainingPct != 42.0 {
		t.Fatalf("claude 5h pct = %.1f remaining %.1f", claude5h.UsedPct, claude5h.RemainingPct)
	}
	if claude5h.ResetLabel != "(목) 오후 4:30에 재설정" {
		t.Fatalf("reset label = %q", claude5h.ResetLabel)
	}

	openai7d := findWindow(t, windows, "openai", "7d")
	if openai7d.UsedPct != 97.0 || openai7d.RemainingPct != 3.0 {
		t.Fatalf("openai 7d pct = %.1f remaining %.1f", openai7d.UsedPct, openai7d.RemainingPct)
	}

	resetAt := time.Date(2026, 6, 18, 15, 0, 0, 0, kst)
	now := time.Date(2026, 6, 11, 15, 0, 0, 0, kst)
	progress, dayIndex, label := computeLegacyWeekProgress(now, &resetAt)
	if progress != 0 || dayIndex != 0 || label != "7일 후 리셋" {
		t.Fatalf("progress=%d dayIndex=%d label=%q", progress, dayIndex, label)
	}
}

func TestTokenSnapshotOverridesClaudeAndAddsSonnet(t *testing.T) {
	usedSonnet := 34.0
	resp := LLMLimitStatusResponse{
		Windows: []LLMLimitWindowResponse{
			{Provider: "claude", Window: "5h", UsedPct: 58, RemainingPct: 42},
			{Provider: "claude", Window: "7d", UsedPct: 45, RemainingPct: 55},
		},
	}

	applyTokenSnapshot(&resp, tokenSnapshotFile{
		FiveHourUtilization:       ptrFloat(80),
		FiveHourResetsAt:          "2026-06-11T16:30:00+00:00",
		SevenDayUtilization:       ptrFloat(46),
		SevenDayResetsAt:          "2026-06-11T14:59:59+00:00",
		SevenDaySonnetUtilization: &usedSonnet,
		SevenDaySonnetResetsAt:    "2026-06-11T15:00:00+00:00",
	})

	if got := findWindow(t, resp.Windows, "claude", "5h"); got.UsedPct != 80 || got.RemainingPct != 20 {
		t.Fatalf("claude 5h override = %.1f remaining %.1f", got.UsedPct, got.RemainingPct)
	}
	if resp.SonnetUsedPct == nil || *resp.SonnetUsedPct != 34 {
		t.Fatalf("sonnet pct = %#v", resp.SonnetUsedPct)
	}
	if resp.SonnetRemainingPct == nil || *resp.SonnetRemainingPct != 66 {
		t.Fatalf("sonnet remaining = %#v", resp.SonnetRemainingPct)
	}
}

func findWindow(t *testing.T, windows []LLMLimitWindowResponse, provider string, window string) LLMLimitWindowResponse {
	t.Helper()
	for _, item := range windows {
		if item.Provider == provider && item.Window == window {
			return item
		}
	}
	t.Fatalf("missing %s %s", provider, window)
	return LLMLimitWindowResponse{}
}

func ptrFloat(v float64) *float64 {
	return &v
}

package providerusage

import (
	"context"
	"strings"
	"testing"
	"time"
)

const claudeLive = `You are currently using your subscription to power your Claude Code usage

Current session: 38% used · resets Sep 7 at 2:59pm (Asia/Jakarta)
Current week (all models): 4% used · resets Sep 14 at 5:59am (Asia/Jakarta)

What's contributing to your limits usage?
Approximate, based on local sessions on this machine — does not include other devices.

Last 24h · 268 requests · 3 sessions
  37% of your usage was at >150k context
`

func TestParseClaudeUsageLiveFixture(t *testing.T) {
	now := time.Date(2026, 9, 7, 6, 0, 0, 0, time.UTC)
	windows, plan, ok := ParseClaudeUsage(claudeLive, now)
	if !ok {
		t.Fatal("expected windows")
	}
	if plan != "" {
		t.Fatalf("plan = %q, want empty", plan)
	}
	if len(windows) != 2 {
		t.Fatalf("windows = %d, want 2 (prose percentages must be ignored)", len(windows))
	}
	if windows[0].ID != "session" || windows[0].PercentUsed != 38 {
		t.Fatalf("session = %+v", windows[0])
	}
	if windows[1].ID != "weekly_all" || windows[1].PercentUsed != 4 {
		t.Fatalf("weekly = %+v", windows[1])
	}
	if windows[0].ResetsAt == nil || !windows[0].ResetsAt.Equal(time.Date(2026, 9, 7, 7, 59, 0, 0, time.UTC)) {
		t.Fatalf("session reset = %v", windows[0].ResetsAt)
	}
}

func TestParseClaudeUsagePlanAndOnTheHour(t *testing.T) {
	text := "You are currently using Max 5x\n\nCurrent session: 72% used · resets Sep 7 at 3pm (Asia/Jakarta)\n"
	now := time.Date(2026, 9, 7, 6, 0, 0, 0, time.UTC)
	windows, plan, ok := ParseClaudeUsage(text, now)
	if !ok || plan != "Max 5x" {
		t.Fatalf("plan = %q ok=%v", plan, ok)
	}
	if windows[0].ResetsAt == nil || !windows[0].ResetsAt.Equal(time.Date(2026, 9, 7, 8, 0, 0, 0, time.UTC)) {
		t.Fatalf("reset = %v", windows[0].ResetsAt)
	}
}

func TestParseClaudeUsageExtraUsagePhrase(t *testing.T) {
	text := "You are currently using your extra usage to power your Claude Code usage\n\nCurrent session: 10% used · resets Sep 7 at 2:59pm (UTC)\n"
	_, plan, ok := ParseClaudeUsage(text, time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC))
	if !ok || plan != "extra usage" {
		t.Fatalf("plan = %q ok=%v", plan, ok)
	}
}

func TestParseClaudeUsageKeepsPercentWhenResetIsGarbage(t *testing.T) {
	windows, _, ok := ParseClaudeUsage("Current session: 38% used · resets whenever it feels like it\n", time.Now())
	if !ok || len(windows) != 1 || windows[0].PercentUsed != 38 || windows[0].ResetsAt != nil {
		t.Fatalf("windows = %+v ok=%v", windows, ok)
	}
}

func TestParseClaudeUsagePerModelWeekly(t *testing.T) {
	text := "Current session: 10% used · resets Sep 7 at 2:59pm (UTC)\nCurrent week (Opus): 12% used · resets Sep 14 at 5:59am (UTC)\n"
	windows, _, ok := ParseClaudeUsage(text, time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC))
	if !ok || len(windows) != 2 || windows[1].ID != "weekly_opus" {
		t.Fatalf("windows = %+v", windows)
	}
}

func TestParseClaudeUsageYearBoundary(t *testing.T) {
	dec := time.Date(2026, 12, 31, 12, 0, 0, 0, time.UTC)
	got, ok := parseClaudeReset("Jan 2 at 3:00am (UTC)", dec)
	want := time.Date(2027, 1, 2, 3, 0, 0, 0, time.UTC)
	if !ok || !got.Equal(want) {
		t.Fatalf("jan reset = %v ok=%v", got, ok)
	}
	jan := time.Date(2027, 1, 2, 12, 0, 0, 0, time.UTC)
	got, ok = parseClaudeReset("Dec 31 at 3:00am (UTC)", jan)
	want = time.Date(2026, 12, 31, 3, 0, 0, 0, time.UTC)
	if !ok || !got.Equal(want) {
		t.Fatalf("dec reset = %v ok=%v", got, ok)
	}
}

func TestParseClaudeUsageMalformed(t *testing.T) {
	if _, _, ok := ParseClaudeUsage("Please run /login first", time.Now()); ok {
		t.Fatal("login text parsed as a limit")
	}
	if _, _, ok := ParseClaudeUsage("", time.Now()); ok {
		t.Fatal("empty text parsed")
	}
	if _, _, ok := ParseClaudeUsage("{not json and not usage}", time.Now()); ok {
		t.Fatal("garbage parsed")
	}
}

func TestClaudeCollectorDoesNotRunWhenCLIMissing(t *testing.T) {
	c := ClaudeCollector{
		Locate: func() (string, error) { return "", osErr("missing") },
		Run: func(context.Context, string, []string, string) (string, string, int, error) {
			t.Fatal("command ran")
			return "", "", 0, nil
		},
		Now: func() time.Time { return time.Unix(0, 0).UTC() },
	}
	got := c.Collect(context.Background())
	if !got.Upload || got.Snapshot.ReasonCode != ReasonCLIUnavailable {
		t.Fatalf("result = %+v", got)
	}
}

func TestClaudeCollectorLoginTextIsEmptyNotFailure(t *testing.T) {
	c := ClaudeCollector{
		Locate: func() (string, error) { return "/tmp/fake-claude", nil },
		Run: func(context.Context, string, []string, string) (string, string, int, error) {
			return "Please run /login first\n", "", 1, nil
		},
	}
	got := c.Collect(context.Background())
	if !got.Upload || got.Snapshot.ReasonCode != ReasonNotLoggedIn || len(got.Snapshot.Windows) != 0 {
		t.Fatalf("result = %+v", got)
	}
}

func TestClaudeCollectorKeepsLastGoodOnGarbage(t *testing.T) {
	c := ClaudeCollector{
		Locate: func() (string, error) { return "/tmp/fake-claude", nil },
		Run: func(context.Context, string, []string, string) (string, string, int, error) {
			return "usage output changed shape", "", 0, nil
		},
	}
	got := c.Collect(context.Background())
	if got.Upload {
		t.Fatal("unparsed stdout must not replace the last snapshot")
	}
}

type osErr string

func (e osErr) Error() string { return string(e) }

func TestClaudeUsageArgsDoNotRenewLogin(t *testing.T) {
	joined := strings.Join(claudeUsageArgs, " ")
	if strings.Contains(joined, "-p ") || strings.Contains(joined, "login") {
		t.Fatalf("args must not refresh oauth: %s", joined)
	}
}

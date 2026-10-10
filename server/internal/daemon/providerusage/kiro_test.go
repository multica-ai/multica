package providerusage

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestParseKiroUsage(t *testing.T) {
	text := "Plan: KIRO PRO | 1 usage breakdown\n████████ 40%\n(120 of 300 covered in plan)\nresets on 2026-10-01\nBonus credits: 10/50\n"
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	windows, plan, ok := ParseKiroUsage(text, now)
	if !ok || plan != "Kiro Pro" || len(windows) != 2 {
		t.Fatalf("windows=%+v plan=%q ok=%v", windows, plan, ok)
	}
	if windows[0].ID != "credits" || windows[0].PercentUsed != 40 || windows[0].ResetsAt == nil {
		t.Fatalf("credits = %+v", windows[0])
	}
	if windows[1].ID != "bonus" || windows[1].PercentUsed != 20 {
		t.Fatalf("bonus = %+v", windows[1])
	}
}

func TestParseKiroUsageCreditsWithoutBar(t *testing.T) {
	windows, _, ok := ParseKiroUsage("(15 of 100 covered in plan)", time.Now())
	if !ok || len(windows) != 1 || windows[0].PercentUsed != 15 {
		t.Fatalf("windows=%+v ok=%v", windows, ok)
	}
}

func TestKiroLoggedOutIsEmpty(t *testing.T) {
	c := KiroCollector{
		Locate: func() (string, error) { return "/usr/bin/kiro-cli", nil },
		Run: func(ctx context.Context, path string, args []string, dir string) (string, string, int, error) {
			if path != "/usr/bin/kiro-cli" || len(args) != 3 || args[2] != "/usage" {
				t.Fatalf("command = %s %v", path, args)
			}
			return "You are not logged in. Run kiro-cli login.", "", 1, nil
		},
		Now: func() time.Time { return time.Unix(10, 0).UTC() },
	}
	got := c.Collect(t.Context())
	if !got.Upload || got.Snapshot.ReasonCode != ReasonNotLoggedIn {
		t.Fatalf("result = %+v", got)
	}
}

func TestKiroMissingCLIIsEmpty(t *testing.T) {
	c := KiroCollector{
		Locate: func() (string, error) { return "", os.ErrNotExist },
		Run: func(ctx context.Context, path string, args []string, dir string) (string, string, int, error) {
			t.Fatal("missing cli must not run")
			return "", "", 0, nil
		},
	}
	got := c.Collect(t.Context())
	if !got.Upload || got.Snapshot.ReasonCode != ReasonCLIUnavailable {
		t.Fatalf("result = %+v", got)
	}
}

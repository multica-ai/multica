package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
)

func TestUsageLimitsTrackerKeepsWindowsWhenProbeFails(t *testing.T) {
	var tracker usageLimitsTracker
	first := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	tracker.recordProbe("claude", "pc", agent.RateLimitSnapshot{
		AccountID: "acct",
		Plan:      "max",
		Windows:   []agent.RateLimitWindow{{ID: "seven_day", UsedPercent: 40}},
	}, nil, first)

	failed := tracker.recordProbe("claude", "pc", agent.RateLimitSnapshot{}, errors.New("get_usage timed out"), first.Add(15*time.Minute))
	if failed.Error != "get_usage timed out" {
		t.Fatalf("Error = %q, want the probe error", failed.Error)
	}
	if len(failed.Windows) != 1 || failed.Windows[0].UsedPercent != 40 || failed.AccountID != "acct" {
		t.Fatalf("failed probe dropped the last known limits: %+v", failed)
	}
	if failed.UpdatedAt == nil || !failed.UpdatedAt.Equal(first) {
		t.Fatalf("UpdatedAt = %v, want the last successful check %v", failed.UpdatedAt, first)
	}
	if !failed.CheckedAt.Equal(first.Add(15 * time.Minute)) {
		t.Fatalf("CheckedAt = %v, want the failed check time", failed.CheckedAt)
	}

	recovered := tracker.recordProbe("claude", "pc", agent.RateLimitSnapshot{
		Windows: []agent.RateLimitWindow{{ID: "seven_day", UsedPercent: 45}},
	}, nil, first.Add(30*time.Minute))
	if recovered.Error != "" || recovered.Windows[0].UsedPercent != 45 {
		t.Fatalf("successful probe did not replace the stale state: %+v", recovered)
	}
}

func TestUsageLimitsTrackerFirstFailureHasEmptyWindows(t *testing.T) {
	var tracker usageLimitsTracker
	snapshot := tracker.recordProbe("codex", "pc", agent.RateLimitSnapshot{}, errors.New(strings.Repeat("x", 500)), time.Now())
	if snapshot.Windows == nil || len(snapshot.Windows) != 0 {
		t.Fatalf("Windows = %#v, want an empty list", snapshot.Windows)
	}
	if len(snapshot.Error) != usageLimitsMaxErrorLength {
		t.Fatalf("error length = %d, want it capped at %d", len(snapshot.Error), usageLimitsMaxErrorLength)
	}
	if snapshot.UpdatedAt != nil {
		t.Fatalf("UpdatedAt = %v, want nil before any successful check", snapshot.UpdatedAt)
	}
}

func TestUsageLimitsTrackerThrottlesRunReports(t *testing.T) {
	var tracker usageLimitsTracker
	now := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	update := func(id string, used float64) agent.RateLimitSnapshot {
		return agent.RateLimitSnapshot{Windows: []agent.RateLimitWindow{{ID: id, UsedPercent: used}}}
	}

	wait, schedule := tracker.recordRun("claude", "pc", update("five_hour", 10), now)
	if !schedule || wait != 0 {
		t.Fatalf("first run update: wait=%v schedule=%v, want an immediate report", wait, schedule)
	}
	if _, schedule := tracker.recordRun("claude", "pc", update("seven_day", 20), now.Add(time.Second)); schedule {
		t.Fatal("second update scheduled another report while one was pending")
	}

	report := tracker.takeRunReport("claude", now.Add(2*time.Second))
	if len(report.Windows) != 2 || report.Source != agent.RateLimitSourceRun {
		t.Fatalf("report = %+v, want both merged windows from the run", report)
	}

	wait, schedule = tracker.recordRun("claude", "pc", update("five_hour", 11), now.Add(12*time.Second))
	if !schedule || wait != usageLimitsRunReportInterval-10*time.Second {
		t.Fatalf("update after a report: wait=%v schedule=%v, want the rest of the interval", wait, schedule)
	}
}

func TestUsageLimitsTrackerRefreshFloor(t *testing.T) {
	var tracker usageLimitsTracker
	now := time.Now()
	if !tracker.startProbe("claude", false, now) {
		t.Fatal("first check was refused")
	}
	if tracker.startProbe("claude", true, now.Add(5*time.Second)) {
		t.Fatal("a refresh right after a check started another CLI")
	}
	if !tracker.startProbe("claude", true, now.Add(usageLimitsRefreshFloor)) {
		t.Fatal("a refresh after the floor was refused")
	}
	if !tracker.startProbe("claude", false, now.Add(usageLimitsRefreshFloor+time.Second)) {
		t.Fatal("a scheduled check was refused")
	}
}

func TestCheckUsageLimitsReportsToBuiltInRuntimes(t *testing.T) {
	var mu sync.Mutex
	reports := map[string]agent.RateLimitSnapshot{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runtimeID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/daemon/runtimes/"), "/usage-limits")
		var snapshot agent.RateLimitSnapshot
		if err := json.NewDecoder(r.Body).Decode(&snapshot); err != nil {
			t.Errorf("decode report: %v", err)
		}
		mu.Lock()
		reports[runtimeID] = snapshot
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	originalProbe := probeRateLimits
	probeRateLimits = func(_ context.Context, provider string, _ agent.Command) (agent.RateLimitSnapshot, error) {
		if provider == "codex" {
			return agent.RateLimitSnapshot{}, errors.New("codex is not logged in")
		}
		return agent.RateLimitSnapshot{
			Provider:  provider,
			AccountID: "acct",
			Windows:   []agent.RateLimitWindow{{ID: "five_hour", UsedPercent: 12}},
		}, nil
	}
	t.Cleanup(func() { probeRateLimits = originalProbe })

	d := &Daemon{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		client: NewClient(server.URL),
		runtimeIndex: map[string]Runtime{
			"rt-claude":        {ID: "rt-claude", Provider: "claude"},
			"rt-claude-custom": {ID: "rt-claude-custom", Provider: "claude", ProfileID: "profile-1"},
			"rt-codex":         {ID: "rt-codex", Provider: "codex"},
			"rt-gemini":        {ID: "rt-gemini", Provider: "gemini"},
		},
	}
	d.cfg.DeviceName = "pc-test"
	d.cfg.Agents = map[string]AgentEntry{
		"claude": {Path: "/nonexistent/claude"},
		"codex":  {Path: "/nonexistent/codex"},
		"gemini": {Path: "/nonexistent/gemini"},
	}

	d.checkUsageLimits(context.Background(), false)

	mu.Lock()
	defer mu.Unlock()
	if len(reports) != 2 {
		t.Fatalf("reports went to %v, want only rt-claude and rt-codex", reports)
	}
	claude := reports["rt-claude"]
	if claude.Provider != "claude" || claude.Device != "pc-test" || claude.AccountID != "acct" || len(claude.Windows) != 1 {
		t.Fatalf("claude report = %+v", claude)
	}
	codex := reports["rt-codex"]
	if codex.Error != "codex is not logged in" || codex.Provider != "codex" {
		t.Fatalf("codex report = %+v, want the failed check", codex)
	}
}

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestTerminalReportNewClaimSurvivesFailedDelivery(t *testing.T) {
	cfg := Config{ServerBaseURL: "https://api.example.test", WorkspacesRoot: t.TempDir(), DaemonID: "claim-race"}
	d := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	d.terminalReportSend = func(context.Context, terminalTaskReport, []time.Duration) error {
		return errors.New("network unavailable")
	}
	old := terminalTaskReport{kind: terminalTaskReportFail, taskID: "task-claim", claimDispatchedAt: "2026-10-27T00:00:00Z", errorMessage: "old failure"}
	current := terminalTaskReport{kind: terminalTaskReportComplete, taskID: old.taskID, claimDispatchedAt: "2026-10-27T00:01:00Z", output: "new result"}
	if err := d.reportTerminalTask(context.Background(), old); err == nil {
		t.Fatal("old report unexpectedly delivered")
	}
	if err := d.reportTerminalTask(context.Background(), current); err == nil {
		t.Fatal("current report unexpectedly delivered")
	}
	items, err := d.terminalReports.list()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].report != current {
		t.Fatalf("current result missing from durable queue: %+v", items)
	}
	after := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	var delivered terminalTaskReport
	after.terminalReportSend = func(_ context.Context, report terminalTaskReport, _ []time.Duration) error {
		delivered = report
		return nil
	}
	if pending, count := after.replayPendingTerminalReports(context.Background()); pending != 0 || count != 1 || delivered != current {
		t.Fatalf("restart replay pending=%d delivered=%d report=%+v", pending, count, delivered)
	}
}

func TestTerminalReportStaleReplayPreservesNewClaim(t *testing.T) {
	cfg := Config{ServerBaseURL: "https://api.example.test", WorkspacesRoot: t.TempDir(), DaemonID: "stale-snapshot"}
	d := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ids := []string{"task-a", "task-b", "task-c", "task-d", "task-e"}
	sort.Slice(ids, func(i, j int) bool { return terminalReportFileName(ids[i]) < terminalReportFileName(ids[j]) })
	old := terminalTaskReport{kind: terminalTaskReportFail, taskID: ids[4], claimDispatchedAt: "2026-10-27T00:00:00Z", errorMessage: "old failure"}
	current := terminalTaskReport{kind: terminalTaskReportComplete, taskID: old.taskID, claimDispatchedAt: "2026-10-27T00:01:00Z", output: "new result"}
	for _, id := range ids[:4] {
		if err := d.terminalReports.enqueue(terminalTaskReport{kind: terminalTaskReportFail, taskID: id}); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.terminalReports.enqueue(old); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{}, 4)
	release := make(chan struct{})
	var oldCalls atomic.Int32
	d.terminalReportSend = func(_ context.Context, report terminalTaskReport, _ []time.Duration) error {
		if report.taskID != old.taskID {
			started <- struct{}{}
			<-release
			return nil
		}
		if report.claimDispatchedAt == old.claimDispatchedAt {
			oldCalls.Add(1)
			return &requestError{StatusCode: http.StatusConflict, Body: `{"code":"task_claim_generation_mismatch"}`}
		}
		return errors.New("network unavailable")
	}
	done := make(chan struct{})
	go func() { d.replayPendingTerminalReports(context.Background()); close(done) }()
	for range 4 {
		select {
		case <-started:
		case <-time.After(10 * time.Second):
			t.Fatal("replay workers did not start")
		}
	}
	if err := d.reportTerminalTask(context.Background(), old); err == nil {
		t.Fatal("stale report unexpectedly delivered")
	}
	if err := d.reportTerminalTask(context.Background(), current); err == nil {
		t.Fatal("current report unexpectedly delivered")
	}
	close(release)
	<-done
	if oldCalls.Load() != 2 {
		t.Fatalf("stale report calls = %d, want foreground and stale replay", oldCalls.Load())
	}
	items, err := d.terminalReports.list()
	if err != nil || len(items) != 1 || items[0].report != current {
		t.Fatalf("current result after stale replay = %+v, err %v", items, err)
	}
}

func TestTerminalReportNewClaimPersistsDuringOldReplay(t *testing.T) {
	cfg := Config{ServerBaseURL: "https://api.example.test", WorkspacesRoot: t.TempDir(), DaemonID: "in-flight-claim"}
	d := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	old := terminalTaskReport{kind: terminalTaskReportFail, taskID: "task-in-flight", claimDispatchedAt: "2026-10-27T00:00:00Z", errorMessage: "old failure"}
	current := terminalTaskReport{kind: terminalTaskReportComplete, taskID: old.taskID, claimDispatchedAt: "2026-10-27T00:01:00Z", output: "new result"}
	if err := d.terminalReports.enqueue(old); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	d.terminalReportSend = func(_ context.Context, report terminalTaskReport, _ []time.Duration) error {
		close(started)
		<-release
		return &requestError{StatusCode: http.StatusConflict, Body: `{"code":"task_claim_generation_mismatch"}`}
	}
	done := make(chan struct{})
	go func() { d.replayPendingTerminalReports(context.Background()); close(done) }()
	<-started
	if err := d.reportTerminalTask(context.Background(), current); err == nil {
		t.Fatal("concurrent report unexpectedly delivered")
	}
	close(release)
	<-done
	items, err := d.terminalReports.list()
	if err != nil || len(items) != 1 || items[0].report != current {
		t.Fatalf("current result after in-flight replay = %+v, err %v", items, err)
	}
}

func TestTerminalReportOlderClaimCannotReplaceCurrent(t *testing.T) {
	cfg := Config{ServerBaseURL: "https://api.example.test", WorkspacesRoot: t.TempDir(), DaemonID: "reverse-claims"}
	d := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	current := terminalTaskReport{kind: terminalTaskReportComplete, taskID: "task-reversed", claimDispatchedAt: "2026-10-27T00:01:00Z", output: "new result"}
	old := terminalTaskReport{kind: terminalTaskReportFail, taskID: current.taskID, claimDispatchedAt: "2026-10-27T00:00:00Z", errorMessage: "old failure"}
	d.terminalReportSend = func(context.Context, terminalTaskReport, []time.Duration) error {
		return errors.New("network unavailable")
	}
	_ = d.reportTerminalTask(context.Background(), current)
	_ = d.reportTerminalTask(context.Background(), old)
	items, err := d.terminalReports.list()
	if err != nil || len(items) != 1 || items[0].report != current {
		t.Fatalf("current result after older claim = %+v, err %v", items, err)
	}
}

func TestTerminalReportRecoversNewClaimAfterInterruptedReplace(t *testing.T) {
	cfg := Config{ServerBaseURL: "https://api.example.test", WorkspacesRoot: t.TempDir(), DaemonID: "replace-crash"}
	d := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	old := terminalTaskReport{kind: terminalTaskReportFail, taskID: "task-recover", claimDispatchedAt: "2026-10-27T00:00:00Z"}
	current := terminalTaskReport{kind: terminalTaskReportComplete, taskID: old.taskID, claimDispatchedAt: "2026-10-27T00:01:00Z", output: "recovered result"}
	if err := d.terminalReports.enqueue(old); err != nil {
		t.Fatal(err)
	}
	record, err := persistedTerminalReport(current, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	name := "." + strings.TrimSuffix(terminalReportFileName(old.taskID), ".json") + "-interrupted.tmp"
	if err := os.WriteFile(filepath.Join(d.terminalReports.dir, name), body, 0o600); err != nil {
		t.Fatal(err)
	}
	items, err := d.terminalReports.list()
	if err != nil || len(items) != 1 || items[0].report != current {
		t.Fatalf("recovered current result = %+v, err %v", items, err)
	}
}

func TestTerminalReportGenerationReplay(t *testing.T) {
	for _, kind := range []terminalTaskReportKind{terminalTaskReportComplete, terminalTaskReportFail} {
		for _, generation := range []string{"", "2026-10-27T09:00:00.123456+09:00"} {
			for _, code := range []string{"task_claim_generation_mismatch", "other_conflict"} {
				name, _ := terminalReportKindName(kind)
				t.Run(name+"/"+generation+"/"+code, func(t *testing.T) {
					calls := 0
					srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						calls++
						if r.URL.Path != "/api/daemon/tasks/task-replay/"+name {
							t.Errorf("path = %s", r.URL.Path)
						}
						var body map[string]any
						if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
							t.Error(err)
						}
						if generation == "" {
							if _, present := body["expected_dispatched_at"]; present {
								t.Error("legacy report included generation")
							}
						} else if body["expected_dispatched_at"] != generation {
							t.Errorf("generation = %v, want %s", body["expected_dispatched_at"], generation)
						}
						w.WriteHeader(http.StatusConflict)
						json.NewEncoder(w).Encode(map[string]string{"code": code})
					}))
					defer srv.Close()
					cfg := Config{ServerBaseURL: srv.URL, WorkspacesRoot: t.TempDir(), DaemonID: "daemon-replay"}
					logger := slog.New(slog.NewTextHandler(io.Discard, nil))
					before := New(cfg, logger)
					report := terminalTaskReport{kind: kind, taskID: "task-replay", claimDispatchedAt: generation, output: "original", errorMessage: "original failure"}
					if err := before.terminalReports.enqueue(report); err != nil {
						t.Fatal(err)
					}
					body, err := os.ReadFile(filepath.Join(before.terminalReports.dir, terminalReportFileName(report.taskID)))
					if err != nil {
						t.Fatal(err)
					}
					var record map[string]any
					if err := json.Unmarshal(body, &record); err != nil {
						t.Fatal(err)
					}
					if record["version"] != float64(1) {
						t.Fatalf("version = %v", record["version"])
					}
					if generation == "" {
						if _, present := record["claim_dispatched_at"]; present {
							t.Fatal("legacy record included generation")
						}
					} else if record["claim_dispatched_at"] != generation {
						t.Fatal("record changed claim generation")
					}
					after := New(cfg, logger)
					pending, delivered := after.replayPendingTerminalReports(context.Background())
					wantPending := 1
					if code == "task_claim_generation_mismatch" {
						wantPending = 0
					}
					if pending != wantPending || delivered != 0 {
						t.Fatalf("pending/delivered = %d/%d", pending, delivered)
					}
					items, err := after.terminalReports.list()
					if err != nil || len(items) != wantPending {
						t.Fatalf("queue = %v, %v", items, err)
					}
					if calls != 1 {
						t.Fatalf("requests = %d; stale completion must not compensate", calls)
					}
				})
			}
		}
	}
}

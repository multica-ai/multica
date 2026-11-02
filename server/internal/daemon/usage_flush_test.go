package daemon

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
)

type liveUsageReport struct {
	Usage    []TaskUsageEntry `json:"usage"`
	Snapshot bool             `json:"snapshot"`
}

type liveUsageTransport func(*http.Request) (*http.Response, error)

func (f liveUsageTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func newLiveUsageTestDaemon(t *testing.T, report func(liveUsageReport) int) *Daemon {
	t.Helper()
	client := NewClient("http://usage.invalid")
	client.client.Transport = liveUsageTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/daemon/tasks/task/usage" {
			t.Errorf("unexpected usage request: %s %s", r.Method, r.URL.Path)
		}
		var payload liveUsageReport
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode usage: %v", err)
			return nil, err
		}
		return &http.Response{StatusCode: report(payload), Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header)}, nil
	})
	return &Daemon{client: client}
}

func TestUsageFlushStopWaitsForInFlightSnapshot(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		snapshots := 0
		var stored int64
		d := newLiveUsageTestDaemon(t, func(report liveUsageReport) int {
			if report.Snapshot {
				snapshots++
				<-release
			}
			stored = report.Usage[0].InputTokens
			return http.StatusOK
		})
		var live atomic.Value
		live.Store([]TaskUsageEntry{{Provider: "hermes", Model: "m", InputTokens: 10}})
		stop := d.startUsageFlush(context.Background(), "task", &live, slog.Default())
		synctest.Wait()
		time.Sleep(liveUsageFlushEvery)
		synctest.Wait()
		if snapshots != 1 {
			t.Fatalf("in-flight snapshots = %d, want 1", snapshots)
		}

		stopped := make(chan struct{})
		go func() { stop(); close(stopped) }()
		synctest.Wait()
		select {
		case <-stopped:
			t.Error("stop returned while an old snapshot could still overwrite the terminal report")
		default:
		}
		close(release)
		<-stopped
		synctest.Wait()
		if err := d.client.ReportTaskUsage(context.Background(), "task", []TaskUsageEntry{{Provider: "hermes", Model: "m", InputTokens: 100}}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * liveUsageFlushEvery)
		synctest.Wait()
		if snapshots != 1 || stored != 100 {
			t.Fatalf("after terminal report: snapshots=%d stored=%d, want 1 and 100", snapshots, stored)
		}
	})
}

func TestUsageFlushSkipsUnknownAndUnchangedButRetriesFailures(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		reports := 0
		d := newLiveUsageTestDaemon(t, func(report liveUsageReport) int {
			reports++
			if !report.Snapshot {
				t.Error("mid-run report did not carry snapshot=true")
			}
			if reports == 1 {
				return http.StatusServiceUnavailable
			}
			return http.StatusOK
		})
		var live atomic.Value
		stop := d.startUsageFlush(context.Background(), "task", &live, slog.Default())
		defer stop()
		synctest.Wait()
		tick := func() { time.Sleep(liveUsageFlushEvery); synctest.Wait() }
		tick()
		if reports != 0 {
			t.Fatal("unknown usage was reported as zero")
		}
		live.Store([]TaskUsageEntry{{Provider: "hermes", Model: "m", InputTokens: 10}})
		tick()
		tick()
		tick()
		if reports != 2 {
			t.Fatalf("reports after failure, retry and unchanged tick = %d, want 2", reports)
		}
		live.Store([]TaskUsageEntry{{Provider: "hermes", Model: "m", InputTokens: 20}})
		tick()
		if reports != 3 {
			t.Fatalf("reports after new usage = %d, want 3", reports)
		}
	})
}

func TestUsageFlushCancellationStillWaitsForTheServerWrite(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		release := make(chan struct{})
		d := newLiveUsageTestDaemon(t, func(liveUsageReport) int { <-release; return http.StatusOK })
		transport := d.client.client.Transport
		var requestCtx context.Context
		d.client.client.Transport = liveUsageTransport(func(r *http.Request) (*http.Response, error) {
			requestCtx = r.Context()
			return transport.RoundTrip(r)
		})
		var live atomic.Value
		live.Store([]TaskUsageEntry{{Provider: "hermes", Model: "m", InputTokens: 10}})
		stop := d.startUsageFlush(ctx, "task", &live, slog.Default())
		synctest.Wait()
		time.Sleep(liveUsageFlushEvery)
		synctest.Wait()
		cancel()
		if requestCtx == nil || requestCtx.Err() != nil {
			t.Error("run cancellation abandoned an in-flight server write")
		}
		stopped := make(chan struct{})
		go func() { stop(); close(stopped) }()
		synctest.Wait()
		select {
		case <-stopped:
			t.Error("stop returned before the server write settled on cancellation")
		default:
		}
		close(release)
		<-stopped
		stop()
	})
}

func TestTaskUsageSnapshotsRetainPreviousAttempts(t *testing.T) {
	prior := map[string]agent.TokenUsage{"m": {InputTokens: 100, OutputTokens: 50, CacheReadTokens: 20, CacheWriteTokens: 10, CostUSDTicks: 100}, "other": {InputTokens: 40}}
	var got map[string]agent.TokenUsage
	opts := withPriorTaskUsage(agent.ExecOptions{OnUsage: func(usage map[string]agent.TokenUsage) { got = usage }}, prior)
	opts.OnUsage(map[string]agent.TokenUsage{"m": {InputTokens: 3, OutputTokens: 2, CacheReadTokens: 1, CostUSDTicks: 5}})
	opts.OnUsage(map[string]agent.TokenUsage{"m": {InputTokens: 7, OutputTokens: 4, CacheReadTokens: 3, CacheWriteTokens: 2, CostUSDTicks: 8}})
	if want := (agent.TokenUsage{InputTokens: 107, OutputTokens: 54, CacheReadTokens: 23, CacheWriteTokens: 12, CostUSDTicks: 108}); got["m"] != want || got["other"] != prior["other"] {
		t.Fatalf("task-wide live usage = %v, want m=%+v and previous other model", got, want)
	}
	if prior["m"].InputTokens != 100 {
		t.Fatal("live snapshots mutated the previous attempt's terminal usage")
	}
}

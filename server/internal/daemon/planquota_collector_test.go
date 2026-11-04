package daemon

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

func newQuotaLoopTestDaemon() *Daemon {
	return &Daemon{
		cfg:        Config{},
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		workspaces: map[string]*workspaceState{"ws-1": {runtimeIDs: []string{"rt-kimi", "rt-codex"}}},
		runtimeIndex: map[string]Runtime{
			"rt-kimi":  {ID: "rt-kimi", Provider: "kimi"},
			"rt-codex": {ID: "rt-codex", Provider: "codex"},
		},
	}
}

func waitForQuotaCondition(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestRuntimeIDsForProvider(t *testing.T) {
	d := newQuotaLoopTestDaemon()
	got := d.runtimeIDsForProvider("kimi")
	if len(got) != 1 || got[0] != "rt-kimi" {
		t.Fatalf("kimi ids = %v", got)
	}
	if got := d.runtimeIDsForProvider("codex", "kimi"); len(got) != 2 || got[0] != "rt-codex" || got[1] != "rt-kimi" {
		t.Fatalf("sorted ids = %v", got)
	}
	if got := d.runtimeIDsForProvider("hermes"); len(got) != 0 {
		t.Fatalf("hermes ids = %v", got)
	}
}

func TestRunPlanQuotaCollector_RecordsMatchingRuntimes(t *testing.T) {
	d := newQuotaLoopTestDaemon()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var calls atomic.Int64
	collect := func(context.Context) (*protocol.RuntimePlanQuota, error) {
		calls.Add(1)
		return &protocol.RuntimePlanQuota{
			Provider:   "kimi",
			Status:     protocol.PlanQuotaStatusOK,
			ObservedAt: time.Now().Unix(),
			Source:     protocol.PlanQuotaSourceDaemon,
			Windows:    []protocol.RuntimePlanQuotaWindow{{Name: "primary"}},
		}, nil
	}
	go d.runPlanQuotaCollector(ctx, "kimi", 10*time.Millisecond,
		func() []string { return d.runtimeIDsForProvider("kimi") }, collect)

	waitForQuotaCondition(t, "snapshot cached", func() bool {
		_, ok := d.planQuotaCache.Load("rt-kimi")
		return ok
	})
	cancel()
	if _, ok := d.planQuotaCache.Load("rt-codex"); ok {
		t.Fatal("codex runtime received a kimi snapshot")
	}
}

func TestRunPlanQuotaCollector_NoTargetsSkipsCollection(t *testing.T) {
	d := newQuotaLoopTestDaemon()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var calls atomic.Int64
	collect := func(context.Context) (*protocol.RuntimePlanQuota, error) {
		calls.Add(1)
		return nil, nil
	}
	done := make(chan struct{})
	go func() {
		d.runPlanQuotaCollector(ctx, "zenmux", 10*time.Millisecond,
			func() []string { return d.runtimeIDsForProvider("hermes") }, collect)
		close(done)
	}()

	// No hermes runtime registered: the source must never be polled.
	time.Sleep(100 * time.Millisecond)
	cancel()
	<-done
	if calls.Load() != 0 {
		t.Fatalf("collect called %d times with no target runtimes", calls.Load())
	}
}

func TestRunPlanQuotaCollector_FailureKeepsCache(t *testing.T) {
	d := newQuotaLoopTestDaemon()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fail := func(context.Context) (*protocol.RuntimePlanQuota, error) {
		return nil, errors.New("source down")
	}
	done := make(chan struct{})
	go func() {
		d.runPlanQuotaCollector(ctx, "kimi", 10*time.Millisecond,
			func() []string { return d.runtimeIDsForProvider("kimi") }, fail)
		close(done)
	}()
	time.Sleep(100 * time.Millisecond)
	cancel()
	<-done
	if _, ok := d.planQuotaCache.Load("rt-kimi"); ok {
		t.Fatal("failed collection populated the cache")
	}
}

func TestRateLimitBackoff(t *testing.T) {
	base := 2 * time.Minute
	want := []time.Duration{4, 8, 16, 30, 30, 30, 30} // 2m<<4=32m hits the 30m cap
	for streak := 1; streak <= 7; streak++ {
		got := rateLimitBackoff(base, streak)
		if got != want[streak-1]*time.Minute {
			t.Fatalf("streak %d = %v, want %v", streak, got, want[streak-1]*time.Minute)
		}
	}
	if got := rateLimitBackoff(30*time.Minute, 3); got != planQuotaRateLimitMaxBackoff {
		t.Fatalf("cap = %v", got)
	}
	if got := rateLimitBackoff(base, 0); got != 4*time.Minute {
		t.Fatalf("streak 0 = %v", got)
	}
}

// S2b: a clear marker is a deliberate state, not an observation — it is
// re-stamped on every heartbeat so "not reported" never ages into "stale",
// while real snapshots keep their observed_at (staleness must keep signaling
// a silently failing collector).
func TestHeartbeatExtrasFor_ClearMarkerReStamped(t *testing.T) {
	d := newQuotaLoopTestDaemon()

	d.recordZenMuxPlanQuotaClearMarker("rt-kimi")
	// Age the cached marker 25h, as if the daemon cleared long ago.
	cached, _ := d.planQuotaCache.Load("rt-kimi")
	aged := *cached.(planQuotaCacheEntry).quota
	aged.ObservedAt = time.Now().Add(-25 * time.Hour).Unix()
	d.planQuotaCache.Store("rt-kimi", planQuotaCacheEntry{quota: &aged, clearMarker: true})

	extras := d.heartbeatExtrasFor("rt-kimi")
	if extras.PlanQuota == nil {
		t.Fatal("marker not attached to heartbeat")
	}
	if age := time.Now().Unix() - extras.PlanQuota.ObservedAt; age > 5 {
		t.Fatalf("marker observed_at not re-stamped: age %ds", age)
	}
	if len(extras.PlanQuota.Windows) != 0 {
		t.Fatalf("marker carries windows: %+v", extras.PlanQuota.Windows)
	}
	// The cached original keeps its age — the re-stamp is per-heartbeat.
	if got, _ := d.planQuotaCache.Load("rt-kimi"); got.(planQuotaCacheEntry).quota.ObservedAt != aged.ObservedAt {
		t.Fatal("cached marker mutated in place")
	}

	// A real snapshot replaces the marker and is never re-stamped.
	real := &protocol.RuntimePlanQuota{
		Provider:   "zenmux",
		Status:     protocol.PlanQuotaStatusOK,
		ObservedAt: time.Now().Add(-time.Hour).Unix(),
		Source:     protocol.PlanQuotaSourceDaemon,
		Windows:    []protocol.RuntimePlanQuotaWindow{{Name: "primary"}},
	}
	d.recordRuntimePlanQuota("rt-codex", real)
	extras = d.heartbeatExtrasFor("rt-codex")
	if extras.PlanQuota.ObservedAt != real.ObservedAt {
		t.Fatal("real snapshot re-stamped — collector failure would be masked")
	}
	if got, _ := d.planQuotaCache.Load("rt-codex"); got.(planQuotaCacheEntry).clearMarker {
		t.Fatal("real snapshot left a clear marker behind")
	}
}

// OL-1205 scenario 1: a task-reported snapshot must fan out to every
// same-source runtime (same provider + profile_id) this daemon hosts across
// all workspaces, so each workspace's row updates on its own heartbeat.
func TestRecordTaskPlanQuota_FansOutToSameSourceRuntimes(t *testing.T) {
	d := newQuotaLoopTestDaemon()
	d.mu.Lock()
	d.workspaces = map[string]*workspaceState{
		"ws-a": {runtimeIDs: []string{"rt-claude-a"}},
		"ws-b": {runtimeIDs: []string{"rt-claude-b", "rt-claude-other-profile"}},
		"ws-c": {runtimeIDs: []string{"rt-codex", "rt-claude-c"}},
	}
	d.runtimeIndex = map[string]Runtime{
		"rt-claude-a":             {ID: "rt-claude-a", Provider: "claude"},
		"rt-claude-b":             {ID: "rt-claude-b", Provider: "claude"},
		"rt-claude-c":             {ID: "rt-claude-c", Provider: "claude"},
		"rt-claude-other-profile": {ID: "rt-claude-other-profile", Provider: "claude", ProfileID: "prof-1"},
		"rt-codex":                {ID: "rt-codex", Provider: "codex"},
	}
	d.mu.Unlock()

	quota := &protocol.RuntimePlanQuota{
		Provider:   "claude",
		Status:     protocol.PlanQuotaStatusOK,
		ObservedAt: time.Now().Unix(),
		Source:     protocol.PlanQuotaSourceDaemon,
		Windows:    []protocol.RuntimePlanQuotaWindow{{Name: "5h"}},
	}
	d.recordTaskPlanQuota("rt-claude-b", quota)

	for _, id := range []string{"rt-claude-a", "rt-claude-b", "rt-claude-c"} {
		if _, ok := d.planQuotaCache.Load(id); !ok {
			t.Errorf("same-source runtime %s did not receive the snapshot", id)
		}
	}
	if _, ok := d.planQuotaCache.Load("rt-claude-other-profile"); ok {
		t.Error("claude runtime with a different profile_id received the snapshot")
	}
	if _, ok := d.planQuotaCache.Load("rt-codex"); ok {
		t.Error("codex runtime received a claude snapshot")
	}
}

// The reporting runtime itself must be covered even when it no longer sits in
// any workspace's runtime list (e.g. pruned mid-report).
func TestRecordTaskPlanQuota_UnindexedReportingRuntimeStillCached(t *testing.T) {
	d := newQuotaLoopTestDaemon()
	quota := &protocol.RuntimePlanQuota{
		Provider:   "claude",
		Status:     protocol.PlanQuotaStatusOK,
		ObservedAt: time.Now().Unix(),
		Source:     protocol.PlanQuotaSourceDaemon,
	}
	d.recordTaskPlanQuota("rt-unknown", quota)
	if _, ok := d.planQuotaCache.Load("rt-unknown"); !ok {
		t.Fatal("reporting runtime without an index entry did not get the snapshot")
	}
}

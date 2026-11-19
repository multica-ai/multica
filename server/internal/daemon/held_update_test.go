package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"
)

// updateReportRecorder records every update report the daemon sends and
// answers with status(payload).
type updateReportRecorder struct {
	mu       sync.Mutex
	payloads []map[string]any
}

func (r *updateReportRecorder) handler(t *testing.T, status func(map[string]any) int) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Errorf("decode update report: %v", err)
		}
		r.mu.Lock()
		r.payloads = append(r.payloads, payload)
		r.mu.Unlock()
		code := http.StatusOK
		if status != nil {
			code = status(payload)
		}
		w.WriteHeader(code)
		_, _ = w.Write([]byte(`{}`))
	}
}

func (r *updateReportRecorder) statuses() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.payloads))
	for _, p := range r.payloads {
		s, _ := p["status"].(string)
		out = append(out, s)
	}
	return out
}

func (r *updateReportRecorder) last() map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.payloads) == 0 {
		return nil
	}
	return r.payloads[len(r.payloads)-1]
}

func deferrableUpdate(id string) *PendingUpdate {
	return &PendingUpdate{ID: id, TargetVersion: "v0.4.14", Deferrable: true}
}

func stubRestartTarget(t *testing.T) {
	t.Helper()
	originalResolveSelfExecutable := resolveSelfExecutable
	originalIsBrewInstall := isBrewInstall
	resolveSelfExecutable = func() (string, error) { return "/tmp/multica-next", nil }
	isBrewInstall = func() bool { return false }
	t.Cleanup(func() {
		resolveSelfExecutable = originalResolveSelfExecutable
		isBrewInstall = originalIsBrewInstall
	})
}

func assertUpdateLockReleased(t *testing.T, d *Daemon) {
	t.Helper()
	if d.updating.Load() {
		t.Fatal("update lock still held")
	}
	d.claimMu.Lock()
	paused := d.pauseClaims
	d.claimMu.Unlock()
	if paused {
		t.Fatal("claims still paused")
	}
}

func TestDeferrableUpdateIsQueuedWhileBusy(t *testing.T) {
	var rec updateReportRecorder
	d, _ := updateReportDaemon(t, rec.handler(t, nil))
	d.runUpdateFn = func(string) (string, error) {
		t.Fatal("runUpdateFn must not run while agent work is active")
		return "", nil
	}
	d.activeTasks.Store(2)

	d.handleUpdate(context.Background(), "runtime-1", deferrableUpdate("update-1"))

	if got := rec.statuses(); len(got) != 1 || got[0] != "scheduled" {
		t.Fatalf("reports = %v, want [scheduled]", got)
	}
	if got := rec.last()["active_tasks"]; got != float64(2) {
		t.Fatalf("active_tasks = %v, want 2", got)
	}
	if _, held := d.snapshotHeldUpdate(); !held {
		t.Fatal("deferred update was not held")
	}
	assertUpdateLockReleased(t, d)

	// A re-offer while nothing changed is only a keepalive.
	d.handleUpdate(context.Background(), "runtime-1", deferrableUpdate("update-1"))
	if got := rec.statuses(); len(got) != 1 {
		t.Fatalf("re-offer with an unchanged task count reported again: %v", got)
	}

	// A changed count is reported so the Runtimes page stays current.
	d.activeTasks.Store(1)
	d.handleUpdate(context.Background(), "runtime-1", deferrableUpdate("update-1"))
	if got := rec.statuses(); len(got) != 2 || got[1] != "scheduled" {
		t.Fatalf("reports = %v, want a second scheduled report", got)
	}
	if got := rec.last()["active_tasks"]; got != float64(1) {
		t.Fatalf("active_tasks = %v, want 1", got)
	}
}

func TestHeldUpdateStartsWhenLastTaskFinishes(t *testing.T) {
	stubRestartTarget(t)
	var rec updateReportRecorder
	d, _ := updateReportDaemon(t, rec.handler(t, nil))
	d.updateIdle = make(chan struct{}, 1)
	d.cancelFunc = func() {}
	installed := make(chan string, 1)
	d.runUpdateFn = func(version string) (string, error) {
		installed <- version
		return "upgraded", nil
	}
	d.activeTasks.Store(1)

	d.handleUpdate(context.Background(), "runtime-1", deferrableUpdate("update-1"))

	ctx, cancel := context.WithCancel(context.Background())
	loopDone := make(chan struct{})
	go func() {
		d.heldServerUpdateLoop(ctx)
		close(loopDone)
	}()
	// The loop finishes the restart handoff after the last report; wait for
	// it before the stubbed restart target is restored.
	defer func() {
		cancel()
		<-loopDone
	}()

	d.finishActiveTask()

	select {
	case version := <-installed:
		if version != "v0.4.14" {
			t.Fatalf("installed %q, want v0.4.14", version)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("held update did not start after the last task finished")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(rec.statuses()) < 3 {
		time.Sleep(5 * time.Millisecond)
	}
	got := rec.statuses()
	want := []string{"scheduled", "running", "completed"}
	if len(got) != len(want) {
		t.Fatalf("reports = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("reports = %v, want %v", got, want)
		}
	}
	if _, held := d.snapshotHeldUpdate(); held {
		t.Fatal("held update not cleared after it started")
	}
}

func TestHeldUpdateDroppedWhenServerRejectsStart(t *testing.T) {
	var rec updateReportRecorder
	d, _ := updateReportDaemon(t, rec.handler(t, func(p map[string]any) int {
		if p["status"] == "running" {
			return http.StatusConflict
		}
		return http.StatusOK
	}))
	d.runUpdateFn = func(string) (string, error) {
		t.Fatal("a cancelled update must not install")
		return "", nil
	}

	d.handleUpdate(context.Background(), "runtime-1", deferrableUpdate("update-1"))

	if got := rec.statuses(); len(got) != 1 || got[0] != "running" {
		t.Fatalf("reports = %v, want [running]", got)
	}
	if _, held := d.snapshotHeldUpdate(); held {
		t.Fatal("rejected update is still held")
	}
	assertUpdateLockReleased(t, d)
}

func TestHeldUpdateKeptWhenServerUnreachable(t *testing.T) {
	withFastUpdateReportBackoffs(t)
	var rec updateReportRecorder
	d, _ := updateReportDaemon(t, rec.handler(t, func(map[string]any) int {
		return http.StatusInternalServerError
	}))
	d.runUpdateFn = func(string) (string, error) {
		t.Fatal("must not install without the server's go-ahead")
		return "", nil
	}

	d.handleUpdate(context.Background(), "runtime-1", deferrableUpdate("update-1"))

	if _, held := d.snapshotHeldUpdate(); !held {
		t.Fatal("update dropped after a transient failure; the next re-offer could not retry it")
	}
	assertUpdateLockReleased(t, d)
}

func TestHeldUpdateDroppedWhenCancelledWhileQueued(t *testing.T) {
	var rec updateReportRecorder
	d, _ := updateReportDaemon(t, rec.handler(t, func(p map[string]any) int {
		if p["status"] == "scheduled" {
			return http.StatusConflict
		}
		return http.StatusOK
	}))
	d.activeTasks.Store(1)

	d.handleUpdate(context.Background(), "runtime-1", deferrableUpdate("update-1"))

	if _, held := d.snapshotHeldUpdate(); held {
		t.Fatal("update cancelled on the server is still held")
	}
}

func TestHeldUpdateAlreadySatisfiedCompletesWithoutInstalling(t *testing.T) {
	var rec updateReportRecorder
	d, _ := updateReportDaemon(t, rec.handler(t, nil))
	d.cfg.CLIVersion = "v0.5.0"
	d.runUpdateFn = func(string) (string, error) {
		t.Fatal("must not reinstall a version the daemon already runs")
		return "", nil
	}

	d.handleUpdate(context.Background(), "runtime-1", deferrableUpdate("update-1"))

	if got := rec.statuses(); len(got) != 1 || got[0] != "completed" {
		t.Fatalf("reports = %v, want [completed]", got)
	}
	if got := rec.last()["output"]; got != "Already on v0.5.0" {
		t.Fatalf("output = %v", got)
	}
	if _, held := d.snapshotHeldUpdate(); held {
		t.Fatal("satisfied update is still held")
	}
}

func TestFinishActiveTaskSignalsOnlyWhenIdle(t *testing.T) {
	d := &Daemon{updateIdle: make(chan struct{}, 1)}
	d.activeTasks.Store(2)

	d.finishActiveTask()
	select {
	case <-d.updateIdle:
		t.Fatal("signalled idle with a task still running")
	default:
	}

	d.finishActiveTask()
	select {
	case <-d.updateIdle:
	default:
		t.Fatal("did not signal idle after the last task finished")
	}
}

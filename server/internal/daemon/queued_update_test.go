package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type updateReport struct {
	updateID string
	payload  map[string]any
}

// queuedUpdateDaemon returns a daemon whose update reports are recorded and
// answered by respond (nil answers 200).
func queuedUpdateDaemon(t *testing.T, respond func(updateReport) int) (*Daemon, func() []updateReport) {
	t.Helper()
	withFastUpdateReportBackoffs(t)
	var mu sync.Mutex
	var reports []updateReport
	d, _ := updateReportDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		var rep updateReport
		parts := strings.Split(r.URL.Path, "/")
		rep.updateID = parts[len(parts)-2]
		if err := json.NewDecoder(r.Body).Decode(&rep.payload); err != nil {
			t.Errorf("decode update report: %v", err)
		}
		mu.Lock()
		reports = append(reports, rep)
		mu.Unlock()
		code := http.StatusOK
		if respond != nil {
			code = respond(rep)
		}
		w.WriteHeader(code)
	})
	d.runUpdateFn = func(string) (string, error) {
		t.Fatal("runUpdateFn must not run")
		return "", nil
	}
	return d, func() []updateReport {
		mu.Lock()
		defer mu.Unlock()
		return append([]updateReport(nil), reports...)
	}
}

func deferrable(id string) *PendingUpdate {
	return &PendingUpdate{ID: id, TargetVersion: "v0.12.0", Deferrable: true}
}

func statuses(reports []updateReport) []string {
	out := make([]string, len(reports))
	for i, r := range reports {
		out[i] = r.updateID + ":" + r.payload["status"].(string)
	}
	return out
}

func TestQueuedUpdate_HeldWhileBusyAndStartedWhenIdle(t *testing.T) {
	d, reports := queuedUpdateDaemon(t, nil)
	d.activeTasks.Store(2)

	d.handleUpdate(context.Background(), "runtime-1", deferrable("update-1"))
	// A re-offer with an unchanged task count reports nothing new.
	d.handleUpdate(context.Background(), "runtime-1", deferrable("update-1"))

	got := reports()
	if len(got) != 1 || got[0].payload["status"] != "scheduled" || got[0].payload["active_tasks"] != float64(2) {
		t.Fatalf("reports = %+v, want one scheduled report with 2 active tasks", got)
	}
	d.claimMu.Lock()
	paused := d.pauseClaims
	d.claimMu.Unlock()
	if paused || d.updating.Load() {
		t.Fatal("holding an update must not pause claims")
	}

	installed := make(chan string, 1)
	d.runUpdateFn = func(target string) (string, error) {
		installed <- target
		return "", errors.New("stop before restart")
	}
	d.finishActiveTask()
	d.finishActiveTask()

	select {
	case target := <-installed:
		if target != "v0.12.0" {
			t.Fatalf("installed %q", target)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("held update did not start once the daemon became idle")
	}
	waitFor(t, func() bool { return len(reports()) == 3 }, "install result reported")
	if got := statuses(reports()); got[1] != "update-1:running" || got[2] != "update-1:failed" {
		t.Fatalf("reports = %v", got)
	}
}

func TestQueuedUpdate_RefusedStartIsNotInstalled(t *testing.T) {
	d, reports := queuedUpdateDaemon(t, func(r updateReport) int {
		if r.payload["status"] == "running" {
			return http.StatusConflict // cancelled or expired meanwhile
		}
		return http.StatusOK
	})
	d.activeTasks.Store(1)
	d.handleUpdate(context.Background(), "runtime-1", deferrable("update-1"))

	d.activeTasks.Store(0)
	d.handleUpdate(context.Background(), "runtime-1", deferrable("update-1"))

	if got := statuses(reports()); len(got) != 2 || got[1] != "update-1:running" {
		t.Fatalf("reports = %v", got)
	}
	if d.queuedUpdate != nil {
		t.Fatal("a refused update must be dropped")
	}
	d.claimMu.Lock()
	paused := d.pauseClaims
	d.claimMu.Unlock()
	if paused || d.updating.Load() {
		t.Fatal("a refused start must release the claim barrier")
	}
}

func TestQueuedUpdate_OneUpdatePerMachine(t *testing.T) {
	var withdrawn atomic.Bool
	d, reports := queuedUpdateDaemon(t, func(r updateReport) int {
		if r.updateID == "update-1" && withdrawn.Load() {
			return http.StatusConflict
		}
		return http.StatusOK
	})
	d.activeTasks.Store(1)
	d.handleUpdate(context.Background(), "runtime-ws-a", deferrable("update-1"))

	// Another workspace's runtime on the same daemon asks for a second update.
	d.handleUpdate(context.Background(), "runtime-ws-b", deferrable("update-2"))
	got := reports()
	last := got[len(got)-1]
	if last.updateID != "update-2" || last.payload["status"] != "failed" || last.payload["error"] != errAnotherUpdateQueued {
		t.Fatalf("second update reports = %v, want it refused", statuses(got))
	}
	if d.queuedUpdate == nil || d.queuedUpdate.id != "update-1" {
		t.Fatal("the held update must stay held")
	}

	// Once the held one is cancelled, the next one takes the slot.
	withdrawn.Store(true)
	d.handleUpdate(context.Background(), "runtime-ws-b", deferrable("update-3"))
	if d.queuedUpdate == nil || d.queuedUpdate.id != "update-3" {
		t.Fatalf("held = %+v, want update-3", d.queuedUpdate)
	}
}

func TestQueuedUpdate_RefusedWhileInstalling(t *testing.T) {
	d, reports := queuedUpdateDaemon(t, nil)
	d.updating.Store(true)

	d.handleUpdate(context.Background(), "runtime-1", deferrable("update-2"))

	got := reports()
	if len(got) != 1 || got[0].payload["status"] != "failed" || got[0].payload["error"] != errAnotherUpdateQueued {
		t.Fatalf("reports = %v, want the update refused", statuses(got))
	}
	if d.queuedUpdate != nil {
		t.Fatal("nothing should be held while another update installs")
	}
}

// A re-offer computed before the server accepted "running" can arrive while
// the update installs; it must not be reported as a failure of itself.
func TestQueuedUpdate_ReofferWhileInstallingIsIgnored(t *testing.T) {
	d, reports := queuedUpdateDaemon(t, nil)
	release := make(chan struct{})
	installed := make(chan struct{})
	d.runUpdateFn = func(string) (string, error) {
		close(installed)
		<-release
		return "", errors.New("stop before restart")
	}

	d.handleUpdate(context.Background(), "runtime-1", deferrable("update-1"))
	<-installed
	d.handleUpdate(context.Background(), "runtime-1", deferrable("update-1"))
	d.handleUpdate(context.Background(), "runtime-2", deferrable("update-2"))
	close(release)

	waitFor(t, func() bool { return len(reports()) == 3 }, "install result reported")
	got := statuses(reports())
	want := []string{"update-1:running", "update-2:failed", "update-1:failed"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("reports = %v, want %v", got, want)
		}
	}
	waitFor(t, func() bool {
		d.queuedUpdateMu.Lock()
		defer d.queuedUpdateMu.Unlock()
		return d.queuedUpdate == nil
	}, "failed install releases the slot")
}

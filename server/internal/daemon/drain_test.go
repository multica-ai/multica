package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func decodeDrainResponse(t *testing.T, rec *httptest.ResponseRecorder) drainResponse {
	t.Helper()
	var got drainResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode drain response: %v; body=%s", err, rec.Body.String())
	}
	return got
}

func postDrain(d *Daemon, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	d.drainHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/drain", strings.NewReader(body)))
	return rec
}

func startDrain(d *Daemon, body string) <-chan *httptest.ResponseRecorder {
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() { result <- postDrain(d, body) }()
	return result
}

func waitDrainStatus(t *testing.T, d *Daemon, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for d.drainSnapshot().Status != want {
		if time.Now().After(deadline) {
			t.Fatalf("drain status = %q, want %q", d.drainSnapshot().Status, want)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestDrainHandlerDrainsIdleDaemonAndIsIdempotent(t *testing.T) {
	d := &Daemon{}

	first := postDrain(d, `{"timeout_ms":100}`)
	if first.Code != http.StatusOK {
		t.Fatalf("first drain status = %d, want 200", first.Code)
	}
	got := decodeDrainResponse(t, first)
	if got.Status != "drained" || got.Generation != 1 {
		t.Fatalf("first drain = %+v, want drained generation 1", got)
	}
	if d.tryEnterClaim() {
		t.Fatal("drained daemon admitted a new claim")
	}

	second := postDrain(d, `{"timeout_ms":100}`)
	got = decodeDrainResponse(t, second)
	if second.Code != http.StatusOK || got.Generation != 1 || got.Status != "drained" {
		t.Fatalf("repeat drain = %d %+v, want 200 drained generation 1", second.Code, got)
	}
}

func TestDrainHandlerWaitsForInFlightClaimAndActiveTask(t *testing.T) {
	d := &Daemon{}
	d.claimMu.Lock()
	d.claimsInFlight = 1
	d.claimMu.Unlock()
	d.activeTasks.Store(1)

	result := startDrain(d, `{"timeout_ms":2000}`)
	waitDrainStatus(t, d, "draining")
	if d.tryEnterClaim() {
		t.Fatal("draining daemon admitted a new claim")
	}

	// The in-flight claim hands its task off (activeTasks is counted before
	// exitClaim) and exits; the drain must keep waiting for the active task.
	d.exitClaim()
	select {
	case rec := <-result:
		t.Fatalf("drain returned %d while a task was still active: %s", rec.Code, rec.Body.String())
	case <-time.After(50 * time.Millisecond):
	}

	d.activeTasks.Store(0)
	rec := <-result
	if rec.Code != http.StatusOK {
		t.Fatalf("drain status = %d, want 200", rec.Code)
	}
	if got := decodeDrainResponse(t, rec); got.Status != "drained" || got.ClaimsInFlight != 0 || got.ActiveTaskCount != 0 {
		t.Fatalf("drain response = %+v, want drained with zero counts", got)
	}
}

func TestDrainHandlerTimeoutAutomaticallyResumesAdmission(t *testing.T) {
	d := &Daemon{}
	d.activeTasks.Store(1)
	rec := postDrain(d, `{"timeout_ms":10}`)
	if rec.Code != http.StatusRequestTimeout {
		t.Fatalf("timeout status = %d, want 408", rec.Code)
	}
	got := decodeDrainResponse(t, rec)
	if got.Status != "timeout" || got.ActiveTaskCount != 1 {
		t.Fatalf("timeout response = %+v, want timeout with the task still counted", got)
	}
	if d.activeTasks.Load() != 1 {
		t.Fatal("timeout touched the running task")
	}
	if !d.tryEnterClaim() {
		t.Fatal("admission remained paused after timeout")
	}
	d.exitClaim()
}

func TestDrainHandlerResumesWhenCallerDisconnects(t *testing.T) {
	d := &Daemon{}
	d.activeTasks.Store(1)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		rec := httptest.NewRecorder()
		d.drainHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/drain", strings.NewReader(`{"timeout_ms":60000}`)).WithContext(ctx))
		close(done)
	}()
	waitDrainStatus(t, d, "draining")
	cancel()
	<-done
	if !d.tryEnterClaim() {
		t.Fatal("admission remained paused after the drain caller went away")
	}
	d.exitClaim()
}

func TestResumeEndsWaitingDrain(t *testing.T) {
	d := &Daemon{}
	d.activeTasks.Store(1)
	result := startDrain(d, `{"timeout_ms":60000}`)
	waitDrainStatus(t, d, "draining")

	resume := httptest.NewRecorder()
	d.resumeHandler().ServeHTTP(resume, httptest.NewRequest(http.MethodPost, "/resume", nil))
	if resume.Code != http.StatusOK {
		t.Fatalf("resume status = %d, want 200", resume.Code)
	}
	rec := <-result
	if rec.Code != http.StatusConflict {
		t.Fatalf("drain after resume = %d, want 409", rec.Code)
	}
	if got := decodeDrainResponse(t, rec); got.Status != "running" {
		t.Fatalf("drain after resume = %+v, want running", got)
	}
	if !d.tryEnterClaim() {
		t.Fatal("admission paused after resume")
	}
	d.exitClaim()
}

func TestDrainAndAutoUpdateBarrierAreIndependent(t *testing.T) {
	d := &Daemon{}

	// Resume must not release an auto-update barrier it does not own.
	if !d.trySetClaimBarrier() {
		t.Fatal("could not take the auto-update barrier on an idle daemon")
	}
	d.resumeHandler().ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/resume", nil))
	if d.tryEnterClaim() {
		t.Fatal("resume released the auto-update barrier")
	}
	d.releaseClaimBarrier()

	// Releasing the auto-update barrier must not lift an operator drain.
	if rec := postDrain(d, `{"timeout_ms":100}`); rec.Code != http.StatusOK {
		t.Fatalf("drain status = %d, want 200", rec.Code)
	}
	d.releaseClaimBarrier()
	if d.tryEnterClaim() {
		t.Fatal("auto-update barrier release lifted the operator drain")
	}
	if got := d.drainSnapshot(); got.Status != "drained" {
		t.Fatalf("snapshot after barrier release = %+v, want drained", got)
	}
}

func TestDrainHandlerRejectsOtherMethodsAndBadBody(t *testing.T) {
	d := &Daemon{}
	rec := httptest.NewRecorder()
	d.drainHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/drain", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("PUT /drain = %d, want 405", rec.Code)
	}
	if rec := postDrain(d, `{`); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad body = %d, want 400", rec.Code)
	}
	if !d.tryEnterClaim() {
		t.Fatal("rejected request paused admission")
	}
	d.exitClaim()

	get := httptest.NewRecorder()
	d.drainHandler().ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/drain", nil))
	if got := decodeDrainResponse(t, get); get.Code != http.StatusOK || got.Status != "running" {
		t.Fatalf("GET /drain = %d %+v, want 200 running", get.Code, got)
	}
}

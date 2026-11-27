package daemon

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A concurrent request for the same generation must not undo a drain another
// request already reported as drained, whether it later times out or its
// caller goes away.
func TestStaleTimeoutDoesNotUndoReportedDrain(t *testing.T) {
	d := &Daemon{}
	if rec := postDrain(d, `{"timeout_ms":100}`); rec.Code != http.StatusOK {
		t.Fatalf("drain = %d, want 200", rec.Code)
	}
	// What a second request for generation 1 does when its own deadline fires.
	d.endDrain(1)
	if d.tryEnterClaim() {
		t.Fatal("a same-generation timeout lifted a drain that was already reported drained")
	}
	if got := d.drainSnapshot(); got.Status != "drained" || got.Generation != 1 {
		t.Fatalf("snapshot = %+v, want drained generation 1", got)
	}
}

func TestConcurrentDrainsBothReportDrained(t *testing.T) {
	d := &Daemon{}
	d.activeTasks.Store(1)
	short := startDrain(d, `{"timeout_ms":300}`)
	long := startDrain(d, `{"timeout_ms":5000}`)
	waitDrainStatus(t, d, "draining")
	d.activeTasks.Store(0)
	for _, ch := range []<-chan *httptest.ResponseRecorder{long, short} {
		if rec := <-ch; rec.Code != http.StatusOK {
			t.Fatalf("drain = %d %s, want 200", rec.Code, rec.Body.String())
		}
	}
	time.Sleep(350 * time.Millisecond)
	if d.tryEnterClaim() {
		t.Fatal("drain lifted after both requests reported drained")
	}
}

func TestConcurrentDrainDisconnectDoesNotUndoReportedDrain(t *testing.T) {
	d := &Daemon{}
	d.activeTasks.Store(1)

	ctx, cancel := context.WithCancel(context.Background())
	gone := make(chan struct{})
	go func() {
		rec := httptest.NewRecorder()
		d.drainHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/drain", strings.NewReader(`{"timeout_ms":60000}`)).WithContext(ctx))
		close(gone)
	}()
	waitDrainStatus(t, d, "draining")
	d.activeTasks.Store(0)
	if rec := postDrain(d, `{"timeout_ms":2000}`); rec.Code != http.StatusOK {
		t.Fatalf("drain = %d, want 200", rec.Code)
	}
	cancel()
	<-gone
	if d.tryEnterClaim() {
		t.Fatal("a disconnecting same-generation request lifted a reported drain")
	}
}

func writeControlToken(t *testing.T, value string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "control-token")
	if err := os.WriteFile(p, []byte(value+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func controlRequest(method, path, body, token string) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	return r
}

func TestAdmissionControlRequiresConfiguredToken(t *testing.T) {
	d := &Daemon{cfg: Config{ControlTokenFile: writeControlToken(t, "operator-secret")}}

	for _, tc := range []struct {
		name, token string
	}{{"no token", ""}, {"wrong token", "task-token"}} {
		rec := httptest.NewRecorder()
		d.drainHandler().ServeHTTP(rec, controlRequest(http.MethodPost, "/drain", `{"timeout_ms":100}`, tc.token))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s: POST /drain = %d, want 401", tc.name, rec.Code)
		}
		if !d.tryEnterClaim() {
			t.Fatalf("%s: rejected drain paused admission", tc.name)
		}
		d.exitClaim()
	}

	rec := httptest.NewRecorder()
	d.drainHandler().ServeHTTP(rec, controlRequest(http.MethodPost, "/drain", `{"timeout_ms":100}`, "operator-secret"))
	if rec.Code != http.StatusOK {
		t.Fatalf("authorized drain = %d, want 200", rec.Code)
	}

	rec = httptest.NewRecorder()
	d.resumeHandler().ServeHTTP(rec, controlRequest(http.MethodPost, "/resume", "", "task-token"))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized resume = %d, want 401", rec.Code)
	}
	if d.tryEnterClaim() {
		t.Fatal("rejected resume lifted the drain")
	}

	// Status stays readable without the token.
	rec = httptest.NewRecorder()
	d.drainHandler().ServeHTTP(rec, controlRequest(http.MethodGet, "/drain", "", ""))
	if got := decodeDrainResponse(t, rec); rec.Code != http.StatusOK || got.Status != "drained" {
		t.Fatalf("GET /drain = %d %+v, want 200 drained", rec.Code, got)
	}

	rec = httptest.NewRecorder()
	d.resumeHandler().ServeHTTP(rec, controlRequest(http.MethodPost, "/resume", "", "operator-secret"))
	if rec.Code != http.StatusOK || !d.tryEnterClaim() {
		t.Fatalf("authorized resume = %d, admission not restored", rec.Code)
	}
	d.exitClaim()
}

func TestAdmissionControlFailsClosedWhenTokenFileUnreadable(t *testing.T) {
	d := &Daemon{cfg: Config{ControlTokenFile: filepath.Join(t.TempDir(), "missing")}}
	rec := httptest.NewRecorder()
	d.drainHandler().ServeHTTP(rec, controlRequest(http.MethodPost, "/drain", `{"timeout_ms":100}`, "anything"))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("drain with unreadable token file = %d, want 503", rec.Code)
	}
	if !d.tryEnterClaim() {
		t.Fatal("rejected drain paused admission")
	}
	d.exitClaim()
}

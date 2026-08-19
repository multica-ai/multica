package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/realtime"
)

func TestMainRouterDoesNotExposeDiagnostics(t *testing.T) {
	router := NewRouter(nil, realtime.NewHub(), events.New(), analytics.NoopClient{}, nil)

	for _, path := range []string{"/metrics", "/debug/pprof/", "/debug/pprof/heap"} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, path, nil)
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusNotFound {
				t.Fatalf("main API %s status = %d, want %d", path, rec.Code, http.StatusNotFound)
			}
		})
	}
}

// With /metrics off, main.go leaves the *WecomMetrics nil. Assigning that
// pointer straight into the adapter's interface field would produce an
// interface that is not nil while the pointer inside it is, so the adapter's
// own nil check would pass and the first counter call would panic — on the one
// configuration nobody runs in staging.
func TestNilWecomMetricsBecomesANilInterface(t *testing.T) {
	var off *obsmetrics.WecomMetrics
	if got := wecomMetricsOrNil(off); got != nil {
		t.Fatal("a nil *WecomMetrics produced a non-nil interface; the adapter would call through a nil pointer")
	}

	on := obsmetrics.NewWecomMetrics()
	got := wecomMetricsOrNil(on)
	if got == nil {
		t.Fatal("a real sink was dropped")
	}
	got.RecordAuthFailure() // must reach the counter, not a no-op
}

package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
)

// TestMetricReportsAreRateLimitedPerWorkspaceAndMetric: sixty reports a minute
// per workspace and metric, the next one refused, and a different metric has its
// own budget. Without Redis the in-process limiter bounds the reports.
func TestMetricReportsAreRateLimitedPerWorkspaceAndMetric(t *testing.T) {
	h := &Handler{}
	ctx := context.Background()
	ws := fmt.Sprintf("rl-ws-%p", h)
	for i := 0; i < metricReportsPerMinute; i++ {
		if !h.allowMetricReport(ctx, ws, "rl.metric") {
			t.Fatalf("report %d refused inside the budget", i+1)
		}
	}
	if h.allowMetricReport(ctx, ws, "rl.metric") {
		t.Fatalf("report %d allowed past the budget", metricReportsPerMinute+1)
	}
	if !h.allowMetricReport(ctx, ws, "rl.other") {
		t.Fatalf("a different metric shares the budget")
	}
	if !h.allowMetricReport(ctx, "rl-other-ws-"+ws, "rl.metric") {
		t.Fatalf("a different workspace shares the budget")
	}
}

// TestMetricErrorsMapToTheirStatus: the refusals a person can fix answer 400; a
// database failure does not leak its text as a client error.
func TestMetricErrorsMapToTheirStatus(t *testing.T) {
	cases := map[error]int{
		service.ErrMetricBuiltinReadOnly: http.StatusBadRequest,
		service.ErrMetricKeyInvalid:      http.StatusBadRequest,
		service.ErrMetricValueInvalid:    http.StatusBadRequest,
		service.ErrMetricScopeInvalid:    http.StatusBadRequest,
		service.ErrMetricObservedInvalid: http.StatusBadRequest,
		service.ErrMetricNoteTooLong:     http.StatusBadRequest,
		errors.New("connection reset"):   http.StatusInternalServerError,
	}
	for err, want := range cases {
		rec := httptest.NewRecorder()
		writeMetricError(rec, err)
		if rec.Code != want {
			t.Errorf("%v: status %d, want %d", err, rec.Code, want)
		}
	}
}

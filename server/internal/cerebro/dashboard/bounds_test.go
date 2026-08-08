package dashboard

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func boundsRequest(t *testing.T, query url.Values) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "/api/cerebro/dashboard?"+query.Encode(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func TestApplyExactBoundsOverridesPeriodAndPrior(t *testing.T) {
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	base := parseRange("30d", now)
	start := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 1)

	w := httptest.NewRecorder()
	spec, ok := applyExactBounds(w, boundsRequest(t, url.Values{
		"start": {start.Format(time.RFC3339)},
		"end":   {end.Format(time.RFC3339)},
	}), base)
	if !ok {
		t.Fatalf("expected ok, got 400: %s", w.Body.String())
	}
	if !spec.periodStart.Equal(start) || !spec.periodEnd.Equal(end) {
		t.Fatalf("period not overridden: %v..%v", spec.periodStart, spec.periodEnd)
	}
	if !spec.priorStart.Equal(start.AddDate(0, 0, -1)) || !spec.priorEnd.Equal(start) {
		t.Fatalf("prior window wrong: %v..%v", spec.priorStart, spec.priorEnd)
	}
	if !spec.bucketsByDay {
		t.Fatal("a full day should bucket by day")
	}
}

func TestApplyExactBoundsAbsentKeepsPreset(t *testing.T) {
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	base := parseRange("7d", now)
	w := httptest.NewRecorder()
	spec, ok := applyExactBounds(w, boundsRequest(t, url.Values{}), base)
	if !ok {
		t.Fatal("expected ok without bounds")
	}
	if spec != base {
		t.Fatalf("spec changed without bounds: %+v vs %+v", spec, base)
	}
}

func TestApplyExactBoundsRejectsMalformedOrUnordered(t *testing.T) {
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	base := parseRange("7d", now)
	cases := []url.Values{
		{"start": {"not-a-time"}, "end": {now.Format(time.RFC3339)}},
		{"start": {now.Format(time.RFC3339)}},
		{"start": {now.Format(time.RFC3339)}, "end": {now.Add(-time.Hour).Format(time.RFC3339)}},
		{"start": {now.Format(time.RFC3339)}, "end": {now.Format(time.RFC3339)}},
	}
	for _, query := range cases {
		w := httptest.NewRecorder()
		if _, ok := applyExactBounds(w, boundsRequest(t, query), base); ok {
			t.Fatalf("expected rejection for %v", query)
		}
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for %v, got %d", query, w.Code)
		}
	}
}

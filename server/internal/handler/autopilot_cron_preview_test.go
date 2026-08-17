package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func cronPreviewRequest(expr, tz string) *http.Request {
	q := url.Values{}
	q.Set("expr", expr)
	if tz != "" {
		q.Set("tz", tz)
	}
	return newRequest("GET", "/api/autopilots/cron-preview?"+q.Encode(), nil)
}

func decodeCronPreview(t *testing.T, w *httptest.ResponseRecorder) []string {
	t.Helper()
	var body struct {
		NextRuns []string `json:"next_runs"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	return body.NextRuns
}

func TestCronPreview_ValidExpression(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	w := httptest.NewRecorder()
	testHandler.CronPreview(w, cronPreviewRequest("0 9 * * *", "UTC"))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	runs := decodeCronPreview(t, w)
	if len(runs) != 3 {
		t.Fatalf("expected 3 next_runs, got %d: %v", len(runs), runs)
	}
	var prev time.Time
	for i, s := range runs {
		ts, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatalf("next_runs[%d] = %q is not RFC3339: %v", i, s, err)
		}
		if ts.UTC().Hour() != 9 || ts.Minute() != 0 {
			t.Fatalf("next_runs[%d] = %q, want 09:00 UTC", i, s)
		}
		if i > 0 && !ts.After(prev) {
			t.Fatalf("next_runs not strictly ascending: %v", runs)
		}
		prev = ts
	}
}

func TestCronPreview_CompoundExpression(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	w := httptest.NewRecorder()
	testHandler.CronPreview(w, cronPreviewRequest("0 9-21/2 * * 2-4", "Asia/Shanghai"))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("failed to load location: %v", err)
	}
	for i, s := range decodeCronPreview(t, w) {
		ts, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatalf("next_runs[%d] not RFC3339: %v", i, err)
		}
		local := ts.In(loc)
		if wd := local.Weekday(); wd < time.Tuesday || wd > time.Thursday {
			t.Fatalf("next_runs[%d] = %s falls on %s, want Tue-Thu", i, s, wd)
		}
		if h := local.Hour(); h < 9 || h > 21 || (h-9)%2 != 0 {
			t.Fatalf("next_runs[%d] = %s local hour %d, want 9-21 step 2", i, s, h)
		}
	}
}

func TestCronPreview_TimezoneApplied(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	w := httptest.NewRecorder()
	testHandler.CronPreview(w, cronPreviewRequest("0 9 * * *", "Asia/Shanghai"))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	runs := decodeCronPreview(t, w)
	if len(runs) == 0 {
		t.Fatal("expected next_runs, got none")
	}
	ts, err := time.Parse(time.RFC3339, runs[0])
	if err != nil {
		t.Fatalf("not RFC3339: %v", err)
	}
	// 09:00 Asia/Shanghai (UTC+8, no DST) is 01:00 UTC.
	if ts.UTC().Hour() != 1 {
		t.Fatalf("expected 01:00 UTC for 09:00 Asia/Shanghai, got %s", runs[0])
	}
}

func TestCronPreview_DefaultsToUTC(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	w := httptest.NewRecorder()
	testHandler.CronPreview(w, cronPreviewRequest("30 18 * * *", ""))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	runs := decodeCronPreview(t, w)
	if len(runs) == 0 {
		t.Fatal("expected next_runs, got none")
	}
	ts, err := time.Parse(time.RFC3339, runs[0])
	if err != nil {
		t.Fatalf("not RFC3339: %v", err)
	}
	if ts.UTC().Hour() != 18 || ts.Minute() != 30 {
		t.Fatalf("expected 18:30 UTC, got %s", runs[0])
	}
}

// A syntactically valid expression that never fires answers 200 with an empty
// list, not 400: the editor tells "never runs" from "your cron is wrong" by the
// status, and a 400 here would also block the trigger from being saved at all.
func TestCronPreview_NeverFiringExpression(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	w := httptest.NewRecorder()
	testHandler.CronPreview(w, cronPreviewRequest("0 0 30 2 *", "UTC"))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if runs := decodeCronPreview(t, w); len(runs) != 0 {
		t.Fatalf("expected no next_runs, got %v", runs)
	}
}

func TestCronPreview_BadRequests(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cases := []struct {
		name string
		expr string
		tz   string
		code string
	}{
		{"invalid expression", "not a cron", "UTC", cronPreviewInvalidCron},
		{"six fields", "0 0 9 * * *", "UTC", cronPreviewInvalidCron},
		{"descriptor", "@daily", "UTC", cronPreviewInvalidCron},
		{"L token", "0 9 L * *", "UTC", cronPreviewInvalidCron},
		{"missing expr", "", "UTC", cronPreviewInvalidCron},
		// A timezone the server does not know is a distinct fault from a bad
		// expression — the editor fixes them in different controls.
		{"invalid timezone", "0 9 * * *", "Not/AZone", cronPreviewInvalidTimezone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			testHandler.CronPreview(w, cronPreviewRequest(tc.expr, tc.tz))
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
			var body struct {
				Error string `json:"error"`
				Code  string `json:"code"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Error == "" {
				t.Fatalf("expected {\"error\": ...} body, got %s", w.Body.String())
			}
			if body.Code != tc.code {
				t.Fatalf("expected code %q, got %q", tc.code, body.Code)
			}
		})
	}
}

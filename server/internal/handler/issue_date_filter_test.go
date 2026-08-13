package handler

import (
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func TestParseIssueDateFilterScheduleOverlap(t *testing.T) {
	w := httptest.NewRecorder()
	filter, ok := parseIssueDateFilter(w, url.Values{
		"schedule_start": {"2026-06-08"},
		"schedule_end":   {"2026-06-14"},
	})
	if !ok {
		t.Fatalf("parseIssueDateFilter returned ok=false: %s", w.Body.String())
	}
	if filter == nil || !filter.scheduleStart.Valid || !filter.scheduleEnd.Valid {
		t.Fatalf("schedule filter not populated: %#v", filter)
	}
	if got := filter.scheduleStart.Time.Format("2006-01-02"); got != "2026-06-08" {
		t.Fatalf("scheduleStart = %s, want 2026-06-08", got)
	}
	if got := filter.scheduleEnd.Time.Format("2006-01-02"); got != "2026-06-14" {
		t.Fatalf("scheduleEnd = %s, want 2026-06-14", got)
	}
}

func TestParseIssueDateFilterScheduleOverlapRequiresBothBounds(t *testing.T) {
	w := httptest.NewRecorder()
	_, ok := parseIssueDateFilter(w, url.Values{
		"schedule_start": {"2026-06-08"},
	})
	if ok {
		t.Fatal("expected ok=false when schedule_end is missing")
	}
	if !strings.Contains(w.Body.String(), "schedule_start and schedule_end are required together") {
		t.Fatalf("error body = %q", w.Body.String())
	}
}

func TestAppendIssueDateFilterScheduleOverlapPredicate(t *testing.T) {
	w := httptest.NewRecorder()
	filter, ok := parseIssueDateFilter(w, url.Values{
		"schedule_start": {"2026-06-08"},
		"schedule_end":   {"2026-06-14"},
	})
	if !ok {
		t.Fatalf("parseIssueDateFilter returned ok=false: %s", w.Body.String())
	}

	args := []any{}
	where := appendIssueDateFilter(nil, func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}, filter)

	if len(where) != 1 {
		t.Fatalf("where len = %d, want 1 (%v)", len(where), where)
	}
	predicate := where[0]
	for _, want := range []string{
		"i.start_date IS NOT NULL",
		"i.due_date IS NOT NULL",
		"i.start_date <= $2",
		"i.due_date >= $1",
	} {
		if !strings.Contains(predicate, want) {
			t.Fatalf("predicate %q missing %q", predicate, want)
		}
	}
	if len(args) != 2 {
		t.Fatalf("args len = %d, want 2", len(args))
	}
}

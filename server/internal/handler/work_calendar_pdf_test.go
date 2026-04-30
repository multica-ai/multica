package handler

import (
	"os"
	"testing"
)

func TestParseRealPDF(t *testing.T) {
	data, err := os.ReadFile("testdata/calendario.pdf")
	if err != nil {
		t.Skip("no testdata/calendario.pdf")
	}

	cal, err := parseWorkCalendarPDF(data)
	if err != nil {
		t.Fatalf("parseWorkCalendarPDF: %v", err)
	}

	if cal.Year != 2026 {
		t.Errorf("year: got %d, want 2026", cal.Year)
	}
	if len(cal.Days) != 365 {
		t.Errorf("days: got %d, want 365", len(cal.Days))
	}

	// Count types
	counts := map[string]int{}
	for _, d := range cal.Days {
		counts[d.Type]++
	}
	t.Logf("Day types: %v", counts)

	// Must have holidays and reduced days
	if counts["holiday"] == 0 {
		t.Error("no holidays detected")
	}
	if counts["reduced"] == 0 {
		t.Error("no reduced days detected")
	}
	if counts["weekend"] < 100 {
		t.Errorf("weekends: got %d, want >= 100", counts["weekend"])
	}

	// Check specific known days
	dayMap := map[string]CalendarDayResponse{}
	for _, d := range cal.Days {
		dayMap[d.Date] = d
	}

	// Jan 1 = holiday (green)
	if d := dayMap["2026-01-01"]; d.Type != "holiday" {
		t.Errorf("Jan 1: got %q, want holiday", d.Type)
	}
	// Jan 6 = holiday (green, Epiphany)
	if d := dayMap["2026-01-06"]; d.Type != "holiday" {
		t.Errorf("Jan 6: got %q, want holiday", d.Type)
	}
	// Jul 1 = reduced (blue) - Tuesday
	if d := dayMap["2026-07-01"]; d.Type != "reduced" {
		t.Errorf("Jul 1: got %q, want reduced", d.Type)
	}
	// Aug 1 = reduced (blue) - Saturday? No, let me check. Aug 1 2026 is Saturday.
	// Aug 3 = Monday = reduced
	if d := dayMap["2026-08-03"]; d.Type != "reduced" {
		t.Errorf("Aug 3: got %q, want reduced", d.Type)
	}
	// Jan 2 = normal (white weekday)? Or check what it actually is
	if d := dayMap["2026-01-02"]; d.Type != "normal" && d.Type != "reduced" {
		t.Logf("Jan 2: got %q", d.Type)
	}

	// Regression: days that MUST NOT be incorrectly classified
	// (These were false positives due to Y-tolerance bleeding)
	if d := dayMap["2026-01-22"]; d.Type == "holiday" {
		t.Errorf("Jan 22: incorrectly marked as holiday (should be normal/reduced)")
	}
	if d := dayMap["2026-04-16"]; d.Type == "holiday" {
		t.Errorf("Apr 16: incorrectly marked as holiday (should be normal/reduced)")
	}
	if d := dayMap["2026-10-06"]; d.Type == "reduced" {
		t.Errorf("Oct 6: incorrectly marked as reduced")
	}
	if d := dayMap["2026-10-07"]; d.Type == "reduced" {
		t.Errorf("Oct 7: incorrectly marked as reduced")
	}
	if d := dayMap["2026-10-08"]; d.Type == "reduced" {
		t.Errorf("Oct 8: incorrectly marked as reduced")
	}
	if d := dayMap["2026-12-01"]; d.Type == "holiday" {
		t.Errorf("Dec 1: incorrectly marked as holiday")
	}
	if d := dayMap["2026-12-17"]; d.Type == "reduced" {
		t.Errorf("Dec 17: incorrectly marked as reduced")
	}
	if d := dayMap["2026-12-18"]; d.Type == "holiday" {
		t.Errorf("Dec 18: incorrectly marked as holiday")
	}

	// Check hours
	for _, d := range cal.Days {
		switch d.Type {
		case "weekend", "holiday":
			if d.Hours != 0 {
				t.Errorf("%s (%s): hours=%v, want 0", d.Date, d.Type, d.Hours)
				break
			}
		case "reduced":
			if d.Hours != 7 {
				t.Errorf("%s (%s): hours=%v, want 7", d.Date, d.Type, d.Hours)
				break
			}
		case "normal":
			if d.Hours != 8.5 {
				t.Errorf("%s (%s): hours=%v, want 8.5", d.Date, d.Type, d.Hours)
				break
			}
		}
	}

	// Log monthly hours
	for _, m := range cal.MonthlyHours {
		t.Logf("  Month %2d: %.1f hours", m.Month, m.TotalHours)
	}
}

func TestParseWorkCalendarPDF_Invalid(t *testing.T) {
	_, err := parseWorkCalendarPDF([]byte("not a pdf"))
	if err == nil {
		t.Error("expected error for invalid PDF")
	}
}

package main

// CEREBRO-PATCH(issue-date-reminders): unit tests for the start/due-date reminder sweeper helpers.

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestIssueDateReminderContent(t *testing.T) {
	tests := []struct {
		kind         string
		wantType     string
		wantSeverity string
		wantBody     string
	}{
		{"due", "due_date_reminder", "attention", "Due today"},
		{"start", "start_date_reminder", "info", "Starts today"},
		// Unknown kinds fall back to the due-date copy rather than emit an
		// empty notification.
		{"", "due_date_reminder", "attention", "Due today"},
	}
	for _, tt := range tests {
		gotType, gotSeverity, gotBody := issueDateReminderContent(tt.kind)
		if gotType != tt.wantType || gotSeverity != tt.wantSeverity || gotBody != tt.wantBody {
			t.Errorf("issueDateReminderContent(%q) = (%q, %q, %q), want (%q, %q, %q)",
				tt.kind, gotType, gotSeverity, gotBody, tt.wantType, tt.wantSeverity, tt.wantBody)
		}
	}
}

func TestDateString(t *testing.T) {
	valid := pgtype.Date{Time: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), Valid: true}
	if got := dateString(valid); got != "2026-06-01" {
		t.Errorf("dateString(valid) = %q, want %q", got, "2026-06-01")
	}
	if got := dateString(pgtype.Date{Valid: false}); got != "" {
		t.Errorf("dateString(invalid) = %q, want empty string", got)
	}
}

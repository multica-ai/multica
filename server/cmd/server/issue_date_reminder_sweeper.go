package main

// CEREBRO-PATCH(issue-date-reminders): background sweeper that fires a
// notification when an issue's start_date or due_date arrives (the day-of, in
// the assignee's timezone). Delivery honors the recipient's per-channel
// notification preferences via dispatchToMember, so the reminder lands on
// inbox / mobile push / desktop exactly where the user has opted in. The claim
// query guarantees each reminder rings at most once per issue + kind + day.

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	issueDateReminderSweepInterval = 5 * time.Minute
	issueDateReminderSweepLimit    = 200
)

func runIssueDateReminderSweeper(ctx context.Context, queries *db.Queries, bus *events.Bus) {
	ticker := time.NewTicker(issueDateReminderSweepInterval)
	defer ticker.Stop()

	tickIssueDateReminders(ctx, queries, bus)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tickIssueDateReminders(ctx, queries, bus)
		}
	}
}

func tickIssueDateReminders(ctx context.Context, queries *db.Queries, bus *events.Bus) {
	rows, err := queries.ClaimArrivingDateReminders(ctx, issueDateReminderSweepLimit)
	if err != nil {
		slog.Warn("issue date reminder sweeper: failed to claim due reminders", "error", err)
		return
	}
	for _, row := range rows {
		if !row.AssigneeID.Valid {
			continue
		}
		notifType, severity, body := issueDateReminderContent(row.Kind)

		details, err := json.Marshal(map[string]string{
			"kind":          row.Kind,
			"reminder_date": dateString(row.ReminderDate),
		})
		if err != nil {
			details = emptyDetails
		}

		dispatchToMember(ctx, queries, bus, inboxItemDraft{
			WorkspaceID:   util.UUIDToString(row.WorkspaceID),
			RecipientType: "member",
			RecipientID:   util.UUIDToString(row.AssigneeID),
			IssueID:       util.UUIDToString(row.IssueID),
			IssueStatus:   row.Status,
			NotifType:     notifType,
			Severity:      severity,
			Title:         row.Title,
			Body:          body,
			Details:       details,
			Actor:         events.Event{ActorType: "system"},
			IsAssignee:    true,
		})
	}
}

// issueDateReminderContent maps the claim kind to the notification type,
// severity, and body copy. A due date is a deadline (attention); a start date
// is informational.
func issueDateReminderContent(kind string) (notifType, severity, body string) {
	if kind == "start" {
		return "start_date_reminder", "info", "Starts today"
	}
	return "due_date_reminder", "attention", "Due today"
}

func dateString(d pgtype.Date) string {
	if !d.Valid {
		return ""
	}
	return d.Time.Format("2006-01-02")
}

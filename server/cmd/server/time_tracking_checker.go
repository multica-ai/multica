package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const timeTrackingCheckInterval = 1 * time.Hour

// calendarDay represents a single day entry from the work calendar JSONB days array.
type calendarDay struct {
	Date  string  `json:"date"`
	Type  string  `json:"type"`  // "holiday", "reduced", "normal", "weekend"
	Hours float64 `json:"hours"`
	Label string  `json:"label,omitempty"`
}

// runTimeTrackingChecker runs a background loop that periodically checks
// whether workspace members have logged the required time for the current day
// based on the workspace's work calendar. Sends inbox notifications to members
// who have not logged enough hours.
func runTimeTrackingChecker(ctx context.Context, queries *db.Queries, bus *events.Bus) {
	ticker := time.NewTicker(timeTrackingCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			checkTimeTracking(ctx, queries, bus)
		}
	}
}

// checkTimeTracking runs the daily time tracking check for all workspaces.
// It only triggers notifications after 17:00 local time (end of typical work day).
func checkTimeTracking(ctx context.Context, queries *db.Queries, bus *events.Bus) {
	now := time.Now()

	// Only check after 17:00 UTC to give users time to log.
	if now.Hour() < 17 {
		return
	}

	runTimeTrackingCheckNow(ctx, queries, bus)
}

// runTimeTrackingCheckNow executes the time tracking check immediately,
// without any time-of-day restrictions. Used by the debug endpoint.
func runTimeTrackingCheckNow(ctx context.Context, queries *db.Queries, bus *events.Bus) {
	now := time.Now()

	workspaces, err := queries.ListAllWorkspaces(ctx)
	if err != nil {
		slog.Error("time tracking checker: failed to list workspaces", "error", err)
		return
	}

	today := now.Format("2006-01-02")

	for _, ws := range workspaces {
		checkWorkspaceTimeTracking(ctx, queries, bus, ws, today, now)
	}
}

// checkWorkspaceTimeTracking checks time tracking for all members in a workspace.
func checkWorkspaceTimeTracking(
	ctx context.Context,
	queries *db.Queries,
	bus *events.Bus,
	ws db.Workspace,
	today string,
	now time.Time,
) {
	wsID := util.UUIDToString(ws.ID)

	// Get the work calendar for the current year.
	calendars, err := queries.ListWorkCalendarsByYear(ctx, db.ListWorkCalendarsByYearParams{
		WorkspaceID: ws.ID,
		Year:        int32(now.Year()),
	})
	if err != nil || len(calendars) == 0 {
		// No calendar configured for this workspace/year — skip.
		return
	}

	// Use the first calendar for the workspace (workspaces typically have one per year).
	cal := calendars[0]

	// Parse the days from the calendar JSONB.
	var days []calendarDay
	if err := json.Unmarshal(cal.Days, &days); err != nil {
		slog.Error("time tracking checker: failed to parse calendar days",
			"workspace_id", wsID, "calendar_id", util.UUIDToString(cal.ID), "error", err)
		return
	}

	// Find today's entry in the calendar.
	var requiredHours float64
	var isWorkDay bool
	for _, d := range days {
		if d.Date == today {
			requiredHours = d.Hours
			isWorkDay = d.Type == "normal" || d.Type == "reduced"
			break
		}
	}

	// If today is not a work day (holiday/weekend) or has 0 required hours, skip.
	if !isWorkDay || requiredHours <= 0 {
		return
	}

	requiredMinutes := int32(requiredHours * 60)

	// Get all members in this workspace.
	members, err := queries.ListMembers(ctx, ws.ID)
	if err != nil {
		slog.Error("time tracking checker: failed to list members",
			"workspace_id", wsID, "error", err)
		return
	}

	// Load notification preferences for all members.
	memberIDs := make([]string, len(members))
	for i, m := range members {
		memberIDs[i] = util.UUIDToString(m.UserID)
	}
	userPrefs := loadUserPrefs(ctx, queries, wsID, memberIDs)

	spentOn := pgtype.Date{Time: now, Valid: true}

	for _, member := range members {
		memberUserID := util.UUIDToString(member.UserID)

		// Check if this notification type is muted for this user.
		if prefs, ok := userPrefs[memberUserID]; ok && isNotifMuted(prefs, "time_not_logged") {
			continue
		}

		// Get the total time logged by this user today.
		totalMinutes, err := queries.GetUserTimeOnDate(ctx, db.GetUserTimeOnDateParams{
			WorkspaceID: ws.ID,
			UserID:      member.UserID,
			SpentOn:     spentOn,
		})
		if err != nil {
			slog.Error("time tracking checker: failed to get user time",
				"workspace_id", wsID, "user_id", memberUserID, "error", err)
			continue
		}

		// If the user has logged enough time, skip.
		if totalMinutes >= requiredMinutes {
			continue
		}

		// Build notification content.
		loggedHours := float64(totalMinutes) / 60.0
		title := fmt.Sprintf("Time not logged: %.1fh of %.1fh logged today", loggedHours, requiredHours)
		body := fmt.Sprintf(
			"You have logged %.1f hours today but the work calendar requires %.1f hours. Please log your remaining time.",
			loggedHours, requiredHours,
		)

		details, _ := json.Marshal(map[string]any{
			"required_minutes": requiredMinutes,
			"logged_minutes":   totalMinutes,
			"date":             today,
		})

		// Check if a notification already exists for this user/date to avoid duplicates.
		existing, err := queries.GetTimeNotLoggedItemForDate(ctx, db.GetTimeNotLoggedItemForDateParams{
			WorkspaceID: ws.ID,
			RecipientID: member.UserID,
			Column3:     today,
		})

		var item db.InboxItem
		if err == nil {
			// Update the existing notification with the latest logged time.
			item, err = queries.UpdateTimeNotLoggedItem(ctx, db.UpdateTimeNotLoggedItemParams{
				ID:      existing.ID,
				Title:   title,
				Body:    pgtype.Text{String: body, Valid: true},
				Details: details,
			})
			if err != nil {
				slog.Error("time tracking checker: failed to update inbox item",
					"workspace_id", wsID, "user_id", memberUserID, "error", err)
				continue
			}
		} else {
			// No existing notification — create a new one.
			item, err = queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
				WorkspaceID:   ws.ID,
				RecipientType: "member",
				RecipientID:   member.UserID,
				Type:          "time_not_logged",
				Severity:      "attention",
				IssueID:       pgtype.UUID{Valid: false},
				Title:         title,
				Body:          pgtype.Text{String: body, Valid: true},
				ActorType:     pgtype.Text{Valid: false}, // system notification
				ActorID:       pgtype.UUID{Valid: false},
				Details:       details,
			})
			if err != nil {
				slog.Error("time tracking checker: failed to create inbox item",
					"workspace_id", wsID, "user_id", memberUserID, "error", err)
				continue
			}
		}

		resp := inboxItemToResponse(item)
		bus.Publish(events.Event{
			Type:        protocol.EventInboxNew,
			WorkspaceID: wsID,
			Payload:     map[string]any{"item": resp},
		})
	}
}

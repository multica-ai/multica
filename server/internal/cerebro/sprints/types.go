package sprints

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
)

const dateLayout = "2006-01-02"

// SettingsResponse is the JSON shape returned by the settings endpoints.
type SettingsResponse struct {
	ProjectID                  string `json:"project_id"`
	WorkspaceID                string `json:"workspace_id"`
	Enabled                    bool   `json:"enabled"`
	DurationUnit               string `json:"duration_unit"`
	DurationCount              int32  `json:"duration_count"`
	StartWeekday               int    `json:"start_weekday"`
	NameTemplate               string `json:"name_template"`
	AutoCreateEnabled          bool   `json:"auto_create_enabled"`
	AutoCreateLeadDays         int32  `json:"auto_create_lead_days"`
	MoveIncompleteEnabled      bool   `json:"move_incomplete_enabled"`
	MoveIncompleteTargetStatus string `json:"move_incomplete_target_status"`
	Timezone                   string `json:"timezone"`
	AcceptsExternalIssues      bool   `json:"accepts_external_issues"`
	CreatedAt                  string `json:"created_at"`
	UpdatedAt                  string `json:"updated_at"`
}

func settingsToResponse(s cerebrodb.CerebroSprintSetting) SettingsResponse {
	return SettingsResponse{
		ProjectID:                  util.UUIDToString(s.ProjectID),
		WorkspaceID:                util.UUIDToString(s.WorkspaceID),
		Enabled:                    s.Enabled,
		DurationUnit:               s.DurationUnit,
		DurationCount:              s.DurationCount,
		StartWeekday:               int(s.StartWeekday),
		NameTemplate:               s.NameTemplate,
		AutoCreateEnabled:          s.AutoCreateEnabled,
		AutoCreateLeadDays:         s.AutoCreateLeadDays,
		MoveIncompleteEnabled:      s.MoveIncompleteEnabled,
		MoveIncompleteTargetStatus: s.MoveIncompleteTargetStatus,
		Timezone:                   s.Timezone,
		AcceptsExternalIssues:      s.AcceptsExternalIssues,
		CreatedAt:                  tsString(s.CreatedAt),
		UpdatedAt:                  tsString(s.UpdatedAt),
	}
}

// SelectableSprintResponse is one row of the issue sprint picker: a sprint the
// issue may be assigned to, annotated with its owning project's title and
// whether that project is the issue's own home project (FIR-1657).
type SelectableSprintResponse struct {
	ID           string `json:"id"`
	WorkspaceID  string `json:"workspace_id"`
	ProjectID    string `json:"project_id"`
	ProjectTitle string `json:"project_title"`
	Name         string `json:"name"`
	SequenceNo   int32  `json:"sequence_no"`
	Status       string `json:"status"`
	StartDate    string `json:"start_date"`
	EndDate      string `json:"end_date"`
	Goal         string `json:"goal,omitempty"`
	IsOwnProject bool   `json:"is_own_project"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

func selectableSprintToResponse(s cerebrodb.ListSelectableCerebroSprintsForIssueRow) SelectableSprintResponse {
	out := SelectableSprintResponse{
		ID:           util.UUIDToString(s.ID),
		WorkspaceID:  util.UUIDToString(s.WorkspaceID),
		ProjectID:    util.UUIDToString(s.ProjectID),
		ProjectTitle: s.ProjectTitle,
		Name:         s.Name,
		SequenceNo:   s.SequenceNo,
		Status:       s.Status,
		StartDate:    dateString(s.StartDate),
		EndDate:      dateString(s.EndDate),
		IsOwnProject: s.IsOwnProject,
		CreatedAt:    tsString(s.CreatedAt),
		UpdatedAt:    tsString(s.UpdatedAt),
	}
	if s.Goal.Valid {
		out.Goal = s.Goal.String
	}
	return out
}

// SprintResponse is the JSON shape returned by the sprint endpoints.
type SprintResponse struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	ProjectID   string `json:"project_id"`
	Name        string `json:"name"`
	SequenceNo  int32  `json:"sequence_no"`
	Status      string `json:"status"`
	StartDate   string `json:"start_date"`
	EndDate     string `json:"end_date"`
	Goal        string `json:"goal,omitempty"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

func sprintToResponse(s cerebrodb.CerebroSprint) SprintResponse {
	out := SprintResponse{
		ID:          util.UUIDToString(s.ID),
		WorkspaceID: util.UUIDToString(s.WorkspaceID),
		ProjectID:   util.UUIDToString(s.ProjectID),
		Name:        s.Name,
		SequenceNo:  s.SequenceNo,
		Status:      s.Status,
		StartDate:   dateString(s.StartDate),
		EndDate:     dateString(s.EndDate),
		CreatedAt:   tsString(s.CreatedAt),
		UpdatedAt:   tsString(s.UpdatedAt),
	}
	if s.Goal.Valid {
		out.Goal = s.Goal.String
	}
	return out
}

// RecurringTaskResponse is the JSON shape returned by the recurring-task
// endpoints.
type RecurringTaskResponse struct {
	ID              string `json:"id"`
	WorkspaceID     string `json:"workspace_id"`
	ProjectID       string `json:"project_id"`
	CadenceUnit     string `json:"cadence_unit"`
	CadenceCount    int32  `json:"cadence_count"`
	Title           string `json:"title"`
	Description     string `json:"description,omitempty"`
	Priority        string `json:"priority,omitempty"`
	AssigneeType    string `json:"assignee_type,omitempty"`
	AssigneeID      string `json:"assignee_id,omitempty"`
	SprintDayOffset int32  `json:"sprint_day_offset"`
	Enabled         bool   `json:"enabled"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

func recurringTaskToResponse(t cerebrodb.CerebroSprintRecurringTask) RecurringTaskResponse {
	out := RecurringTaskResponse{
		ID:              util.UUIDToString(t.ID),
		WorkspaceID:     util.UUIDToString(t.WorkspaceID),
		ProjectID:       util.UUIDToString(t.ProjectID),
		CadenceUnit:     t.CadenceUnit,
		CadenceCount:    t.CadenceCount,
		Title:           t.Title,
		SprintDayOffset: t.SprintDayOffset,
		Enabled:         t.Enabled,
		CreatedAt:       tsString(t.CreatedAt),
		UpdatedAt:       tsString(t.UpdatedAt),
	}
	if t.Description.Valid {
		out.Description = t.Description.String
	}
	if t.Priority.Valid {
		out.Priority = t.Priority.String
	}
	if t.AssigneeType.Valid {
		out.AssigneeType = t.AssigneeType.String
	}
	if t.AssigneeID.Valid {
		out.AssigneeID = util.UUIDToString(t.AssigneeID)
	}
	return out
}

func tsString(t pgtype.Timestamptz) string {
	if !t.Valid {
		return ""
	}
	return t.Time.UTC().Format(time.RFC3339)
}

func dateString(d pgtype.Date) string {
	if !d.Valid {
		return ""
	}
	return d.Time.UTC().Format(dateLayout)
}

func parseDate(s string) (pgtype.Date, error) {
	if s == "" {
		return pgtype.Date{}, nil
	}
	t, err := time.Parse(dateLayout, s)
	if err != nil {
		return pgtype.Date{}, err
	}
	return pgtype.Date{Time: t, Valid: true}, nil
}

func pgTextFromString(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

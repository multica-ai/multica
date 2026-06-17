package recurringissue

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
)

const dateLayout = "2006-01-02"

// RecurrenceResponse is the JSON shape returned by the issue-recurrence
// endpoints. It mirrors the FIR-334 panel fields one-to-one.
type RecurrenceResponse struct {
	ID              string `json:"id"`
	WorkspaceID     string `json:"workspace_id"`
	ProjectID       string `json:"project_id,omitempty"`
	SourceIssueID   string `json:"source_issue_id"`
	Frequency       string `json:"frequency"`
	IntervalCount   int32  `json:"interval_count"`
	Weekdays        []int  `json:"weekdays"`
	DaysAfter       int32  `json:"days_after"`
	TriggerStatus   string `json:"trigger_status"`
	Anchor          string `json:"anchor"`
	CreateNewIssue  bool   `json:"create_new_issue"`
	NewStatus       string `json:"new_status"`
	RecurForever    bool   `json:"recur_forever"`
	EndDate         string `json:"end_date,omitempty"`
	MaxOccurrences  int32  `json:"max_occurrences,omitempty"`
	OccurrenceCount int32  `json:"occurrence_count"`
	Armed           bool   `json:"armed"`
	Enabled         bool   `json:"enabled"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

func toResponse(r cerebrodb.CerebroIssueRecurrence) RecurrenceResponse {
	out := RecurrenceResponse{
		ID:              util.UUIDToString(r.ID),
		WorkspaceID:     util.UUIDToString(r.WorkspaceID),
		SourceIssueID:   util.UUIDToString(r.SourceIssueID),
		Frequency:       r.Frequency,
		IntervalCount:   r.IntervalCount,
		Weekdays:        int16sToInts(r.Weekdays),
		DaysAfter:       r.DaysAfter,
		TriggerStatus:   r.TriggerStatus,
		Anchor:          r.Anchor,
		CreateNewIssue:  r.CreateNewIssue,
		NewStatus:       r.NewStatus,
		RecurForever:    r.RecurForever,
		OccurrenceCount: r.OccurrenceCount,
		Armed:           r.Armed,
		Enabled:         r.Enabled,
		CreatedAt:       tsString(r.CreatedAt),
		UpdatedAt:       tsString(r.UpdatedAt),
	}
	if r.ProjectID.Valid {
		out.ProjectID = util.UUIDToString(r.ProjectID)
	}
	if r.EndDate.Valid {
		out.EndDate = r.EndDate.Time.UTC().Format(dateLayout)
	}
	if r.MaxOccurrences.Valid {
		out.MaxOccurrences = r.MaxOccurrences.Int32
	}
	return out
}

func int16sToInts(in []int16) []int {
	out := make([]int, 0, len(in))
	for _, v := range in {
		out = append(out, int(v))
	}
	return out
}

func intsToInt16s(in []int) []int16 {
	out := make([]int16, 0, len(in))
	for _, v := range in {
		out = append(out, int16(v))
	}
	return out
}

func tsString(t pgtype.Timestamptz) string {
	if !t.Valid {
		return ""
	}
	return t.Time.UTC().Format(time.RFC3339)
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

func pgInt4(v *int32) pgtype.Int4 {
	if v == nil {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: *v, Valid: true}
}

func uuidEqual(a, b pgtype.UUID) bool {
	if !a.Valid || !b.Valid {
		return false
	}
	return a.Bytes == b.Bytes
}

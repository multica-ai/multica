package prompts

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ScheduleIssueInput describes a single issue to schedule.
type ScheduleIssueInput struct {
	ID        string
	Title     string
	Priority  string
	Status    string
	StartDate *time.Time
	EndDate   *time.Time
	// BlockedBy lists IDs of issues that must finish before this one starts.
	BlockedBy []string
}

// BuildScheduleMessages returns system + user messages for scheduling suggestions.
func BuildScheduleMessages(issues []ScheduleIssueInput, today time.Time) (string, string) {
	system := buildScheduleSystemPrompt(today)
	user := buildScheduleUserPrompt(issues)
	return system, user
}

func buildScheduleSystemPrompt(today time.Time) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf(`You are a project scheduling assistant. Today is %s.
Your task is to suggest start_date and end_date for issues based on their priority, status, and dependencies.

IMPORTANT RULES:
- Return ONLY valid JSON matching the schema below. No markdown, no explanation.
- Dates must be in YYYY-MM-DD format.
- Higher priority issues should be scheduled earlier.
- If issue A is in blocked_by list of issue B, then B's start_date must be >= A's end_date.
- Issues already having both start_date and end_date set are provided as context only — do not include them in results unless explicitly requested.
- Suggest realistic durations: "urgent" = 1-2 days, "high" = 2-4 days, "medium" = 3-5 days, "low" = 5-7 days.
- Do not schedule work on weekends (Saturday/Sunday).
- Keep scheduling compact — start from today and pack issues densely by priority.

OUTPUT JSON SCHEMA:
{
  "suggestions": [
    {
      "issue_id": "<string>",
      "start_date": "YYYY-MM-DD",
      "end_date": "YYYY-MM-DD",
      "reason": "<brief explanation>"
    }
  ]
}
`, today.Format("2006-01-02")))

	return sb.String()
}

func buildScheduleUserPrompt(issues []ScheduleIssueInput) string {
	type issueJSON struct {
		ID        string   `json:"id"`
		Title     string   `json:"title"`
		Priority  string   `json:"priority"`
		Status    string   `json:"status"`
		StartDate string   `json:"start_date,omitempty"`
		EndDate   string   `json:"end_date,omitempty"`
		BlockedBy []string `json:"blocked_by,omitempty"`
	}
	items := make([]issueJSON, len(issues))
	for i, iss := range issues {
		item := issueJSON{
			ID:        iss.ID,
			Title:     iss.Title,
			Priority:  iss.Priority,
			Status:    iss.Status,
			BlockedBy: iss.BlockedBy,
		}
		if iss.StartDate != nil {
			item.StartDate = iss.StartDate.Format("2006-01-02")
		}
		if iss.EndDate != nil {
			item.EndDate = iss.EndDate.Format("2006-01-02")
		}
		items[i] = item
	}
	data, _ := json.Marshal(items)
	return fmt.Sprintf("Suggest schedules for the following issues:\n%s", string(data))
}

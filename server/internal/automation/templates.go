// Package automation defines the registry of built-in autopilot templates.
// It is intentionally small — a fixed list of named templates, not a rule engine.
// New templates are added here; enablement state lives in the automation_rule table.
package automation

// Template defines a reusable autopilot automation.
type Template struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// TriggerType is either "scheduled" (runs at a fixed time) or "manual"
	// (user-triggered via the UI or API).
	TriggerType string `json:"trigger_type"`
	// Schedule is a human-readable time string like "22:00" or "07:00".
	// Empty for manual templates.
	Schedule string `json:"schedule,omitempty"`
	// Icon is a hint for the frontend (e.g. lucide icon name).
	Icon string `json:"icon"`
}

// BuiltinTemplates is the canonical list of all available autopilot templates.
// Order here determines display order in the UI.
var BuiltinTemplates = []Template{
	{
		ID:          "nightly_review",
		Name:        "Nightly Review",
		Description: "Generate a personal daily review draft at 10 PM",
		TriggerType: "scheduled",
		Schedule:    "22:00",
		Icon:        "moon",
	},
	{
		ID:          "morning_plan",
		Name:        "Morning Plan",
		Description: "Generate a personal next-day plan draft at 7 AM",
		TriggerType: "scheduled",
		Schedule:    "07:00",
		Icon:        "sun",
	},
	{
		ID:          "standup_summary",
		Name:        "Daily Standup Summary",
		Description: "Generate a team standup summary based on yesterday's activity",
		TriggerType: "manual",
		Icon:        "clipboard-list",
	},
}

// FindTemplate returns the template with the given ID, or nil if not found.
func FindTemplate(id string) *Template {
	for i := range BuiltinTemplates {
		if BuiltinTemplates[i].ID == id {
			return &BuiltinTemplates[i]
		}
	}
	return nil
}

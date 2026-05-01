package prompts

import (
	"encoding/json"
	"fmt"
	"strings"
)

// LabelIssueInput describes a single issue to label.
type LabelIssueInput struct {
	ID          string
	Title       string
	Description string
}

// ExistingLabel is a label already in the workspace.
type ExistingLabel struct {
	ID    string
	Name  string
	Color string
}

// BuildLabelMessages returns the system + user messages for label suggestions.
func BuildLabelMessages(issues []LabelIssueInput, existingLabels []ExistingLabel, rules []string) (string, string) {
	system := buildLabelSystemPrompt(existingLabels, rules)
	user := buildLabelUserPrompt(issues)
	return system, user
}

func buildLabelSystemPrompt(existingLabels []ExistingLabel, rules []string) string {
	var sb strings.Builder

	sb.WriteString(`You are a label classification assistant for a software project management tool.
Your task is to suggest relevant labels for issues based on their content.

IMPORTANT RULES:
- Return ONLY valid JSON matching the schema below. No markdown, no explanation.
- Prefer existing labels over creating new ones when the meaning matches.
- Suggest 1-4 labels per issue. Do not over-label.
- For new labels, suggest a concise lowercase name and a readable hex color.

`)

	if len(rules) > 0 {
		sb.WriteString("WORKSPACE LABEL RULES (follow these):\n")
		for _, r := range rules {
			sb.WriteString("- ")
			sb.WriteString(r)
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	if len(existingLabels) > 0 {
		sb.WriteString("EXISTING WORKSPACE LABELS (prefer these when relevant):\n")
		for _, l := range existingLabels {
			sb.WriteString(fmt.Sprintf("- id=%q name=%q color=%q\n", l.ID, l.Name, l.Color))
		}
		sb.WriteString("\n")
	}

	sb.WriteString(`OUTPUT JSON SCHEMA:
{
  "results": [
    {
      "issue_id": "<string>",
      "suggestions": [
        {
          "name": "<label name>",
          "existing": true,
          "label_id": "<existing label id>"
        },
        {
          "name": "<new label name>",
          "existing": false,
          "color": "#hex"
        }
      ]
    }
  ]
}`)

	return sb.String()
}

func buildLabelUserPrompt(issues []LabelIssueInput) string {
	type issueJSON struct {
		ID          string `json:"id"`
		Title       string `json:"title"`
		Description string `json:"description,omitempty"`
	}
	items := make([]issueJSON, len(issues))
	for i, iss := range issues {
		items[i] = issueJSON{ID: iss.ID, Title: iss.Title, Description: iss.Description}
	}
	data, _ := json.Marshal(items)
	return fmt.Sprintf("Suggest labels for the following issues:\n%s", string(data))
}

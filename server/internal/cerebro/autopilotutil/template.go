package autopilotutil

import (
	"fmt"
	"strings"
	"time"
)

// InterpolateTitleTemplate supports both the original Multica {{date}} token
// and the Go-template-style tokens used by Firtal autopilot prompts.
func InterpolateTitleTemplate(tmpl string, now time.Time) string {
	if tmpl == "" {
		return ""
	}
	year, week := now.ISOWeek()
	replacements := map[string]string{
		"{{date}}":     now.UTC().Format("2006-01-02"),
		"{{.Date}}":    now.UTC().Format("2006-01-02"),
		"{{isoWeek}}":  formatISOWeek(year, week),
		"{{.ISOWeek}}": formatISOWeek(year, week),
	}
	out := tmpl
	for token, value := range replacements {
		out = strings.ReplaceAll(out, token, value)
	}
	return out
}

func formatISOWeek(year, week int) string {
	return fmt.Sprintf("%04d-W%02d", year, week)
}

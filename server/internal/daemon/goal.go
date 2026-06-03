package daemon

import (
	"encoding/json"
	"fmt"
	"strings"
)

type goalStatus string

const (
	goalStatusUnknown   goalStatus = "unknown"
	goalStatusSatisfied goalStatus = "satisfied"
	goalStatusBlocked   goalStatus = "blocked"
	goalStatusPartial   goalStatus = "partial"
)

func appendGoalInstruction(prompt, provider, goal string) string {
	goal = strings.TrimSpace(goal)
	if goal == "" {
		return prompt
	}
	var b strings.Builder
	b.WriteString(strings.TrimRight(prompt, "\n"))
	b.WriteString("\n\n")
	b.WriteString(compileGoalInstruction(provider, goal))
	return b.String()
}

func compileGoalInstruction(provider, goal string) string {
	var b strings.Builder
	b.WriteString("## Completion Goal\n\n")
	fmt.Fprintf(&b, "Multica has stored this task's completion condition as structured data, separate from the issue description:\n\n> %s\n\n", goal)

	switch provider {
	case "codex":
		b.WriteString("Codex runtime instruction: keep working until the completion goal is satisfied or a real blocker is encountered. Do not treat a narrative summary or vague 'done' as enough.\n\n")
	case "openclaw", "claude":
		b.WriteString("Claude runtime instruction: keep working until the completion goal is satisfied or a real blocker is encountered. Do not depend on slash-command support in non-interactive mode; treat this prompt-level instruction as authoritative.\n\n")
	default:
		b.WriteString("Runtime instruction: keep working until the completion goal is satisfied or a real blocker is encountered. Do not treat a narrative summary or vague 'done' as enough.\n\n")
	}

	b.WriteString("Before your final output, verify the goal and include exactly one of these markers with evidence:\n")
	b.WriteString("- GOAL_SATISFIED — the completion condition is true.\n")
	b.WriteString("- BLOCKED — a real blocker prevents satisfying the goal.\n")
	b.WriteString("- PARTIAL — meaningful progress was made, but the goal is not fully satisfied.\n\n")
	b.WriteString("Preferred final shape:\n")
	b.WriteString("```json\n")
	b.WriteString("{\n  \"goal_status\": \"satisfied|blocked|partial\",\n  \"evidence\": [\"...\"],\n  \"remaining\": [\"...\"]\n}\n")
	b.WriteString("```\n")
	return b.String()
}

func parseGoalStatus(output string) goalStatus {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return goalStatusUnknown
	}

	if status := parseGoalStatusJSON(trimmed); status != goalStatusUnknown {
		return status
	}

	upper := strings.ToUpper(trimmed)
	switch {
	case strings.Contains(upper, "GOAL_SATISFIED"):
		return goalStatusSatisfied
	case strings.Contains(upper, "BLOCKED"):
		return goalStatusBlocked
	case strings.Contains(upper, "PARTIAL"):
		return goalStatusPartial
	default:
		return goalStatusUnknown
	}
}

func parseGoalStatusJSON(output string) goalStatus {
	candidates := []string{output}
	if start := strings.Index(output, "{"); start >= 0 {
		if end := strings.LastIndex(output, "}"); end > start {
			candidates = append(candidates, output[start:end+1])
		}
	}

	for _, candidate := range candidates {
		var payload struct {
			GoalStatus string `json:"goal_status"`
		}
		if err := json.Unmarshal([]byte(candidate), &payload); err != nil {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(payload.GoalStatus)) {
		case "satisfied", "goal_satisfied", "done", "complete", "completed":
			return goalStatusSatisfied
		case "blocked":
			return goalStatusBlocked
		case "partial", "partially_satisfied":
			return goalStatusPartial
		}
	}
	return goalStatusUnknown
}

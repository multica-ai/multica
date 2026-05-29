package service

import "strings"

func appendMissingGoalStatusNotice(output string) string {
	output = strings.TrimSpace(output)
	notice := "Goal condition was present, but the agent finished without declaring GOAL_SATISFIED, BLOCKED, or PARTIAL."
	if output == "" {
		return notice
	}
	return output + "\n\n" + notice
}

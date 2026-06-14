package step

import (
	"fmt"
	"strings"
	"time"
)

// Run analyzes coding-team state and comments for dropped handoff notifications.
// It does not call Multica or post recovery comments.
//
// Input:
//   - master_issue_id: string.
//   - state: coding-team state object with tasks[].
//   - task_comments: object keyed by task_issue_id, each value an array of comments.
//   - master_comments: array of master issue comments.
//   - agent_ids: object keyed by planner, implementer, test_writer, reviewer, orchestrator.
//   - now: RFC3339 time used for the 5-minute stale-comment check.
//   - assume_stale_without_timestamps: optional bool, default false.
//
// Output machine_data:
//   - actions: recovery comments to post.
//   - state_patches: task status corrections to apply.
//   - scanned, recovered, skipped: counts for watchdog summary.
func Run(input map[string]any) map[string]any {
	state := object(input["state"])
	masterIssueID := str(input["master_issue_id"])
	taskComments := object(input["task_comments"])
	masterComments := array(input["master_comments"])
	agentIDs := object(input["agent_ids"])
	now := parseNow(str(input["now"]))
	assumeStale := boolValue(input["assume_stale_without_timestamps"])

	tasks := array(state["tasks"])
	actions := []any{}
	patches := []any{}
	skips := []any{}
	scanned := 0

	for _, rawTask := range tasks {
		task := object(rawTask)
		taskID := str(task["task_issue_id"])
		status := str(task["status"])
		if taskID == "" {
			skips = append(skips, skip("", "missing task_issue_id"))
			continue
		}
		if status == "committed" || status == "done" || status == "failed" || status == "awaiting_clarification" {
			skips = append(skips, skip(taskID, "terminal or blocked task status"))
			continue
		}
		scanned++
		comments := array(taskComments[taskID])
		stage, markerIdx := latestStage(comments)
		nextRole := nextRoleForStage(stage)
		if nextRole == "" {
			skips = append(skips, skip(taskID, "stage does not require handoff recovery: "+stage))
			patch := patchForStage(taskID, stage)
			if patch != nil {
				patches = append(patches, patch)
			}
			continue
		}
		agentID := str(agentIDs[nextRole])
		if agentID == "" {
			skips = append(skips, skip(taskID, "missing agent id for "+nextRole))
			continue
		}
		if !latestCommentIsStale(comments, now, assumeStale) {
			skips = append(skips, skip(taskID, "latest comment is not at least 5 minutes old"))
			continue
		}
		if stage != "review_passed" && hasExpectedNotification(comments, markerIdx, agentID) {
			skips = append(skips, skip(taskID, "expected task notification already exists"))
			continue
		}
		if stage == "review_passed" && hasMasterTaskComplete(masterComments, taskID, agentID) {
			skips = append(skips, skip(taskID, "expected master TASK_COMPLETE notification already exists"))
			patches = append(patches, map[string]any{"task_issue_id": taskID, "status": "committed"})
			continue
		}

		action := recoveryAction(masterIssueID, taskID, stage, nextRole, agentID)
		actions = append(actions, action)
		patch := patchForStage(taskID, stage)
		if patch != nil {
			patches = append(patches, patch)
		}
	}

	return map[string]any{
		"status":  "ok",
		"summary": fmt.Sprintf("Analyzed %d active task(s); found %d recovery action(s)", scanned, len(actions)),
		"machine_data": map[string]any{
			"actions":       actions,
			"state_patches": patches,
			"skips":         skips,
			"scanned":       scanned,
			"recovered":     len(actions),
			"skipped":       len(skips),
		},
	}
}

func latestStage(comments []any) (string, int) {
	markers := []struct {
		stage  string
		marker string
	}{
		{"planning_blocked", "## Planning Blocked: Clarification Needed"},
		{"plan_done", "## Implementation Plan"},
		{"impl_done", "## Implementation Complete"},
		{"tests_done", "## Tests Written"},
		{"review_passed", "## Review: PASS"},
		{"review_failed", "## Review: FAIL"},
	}
	bestStage := "not_started"
	bestIdx := -1
	for i, raw := range comments {
		body := commentBody(raw)
		for _, marker := range markers {
			if strings.Contains(body, marker.marker) && i >= bestIdx {
				bestStage = marker.stage
				bestIdx = i
			}
		}
	}
	return bestStage, bestIdx
}

func nextRoleForStage(stage string) string {
	switch stage {
	case "not_started":
		return "planner"
	case "plan_done", "review_failed":
		return "implementer"
	case "impl_done":
		return "test_writer"
	case "tests_done":
		return "reviewer"
	case "review_passed":
		return "orchestrator"
	default:
		return ""
	}
}

func recoveryAction(masterID, taskID, stage, role, agentID string) map[string]any {
	target := taskID
	actionType := "task_handoff_comment"
	content := "Watchdog re-issuing handoff - original notification appears to have been lost.\n\n" + mention(role, agentID)
	if stage == "review_passed" {
		target = masterID
		actionType = "master_task_complete_comment"
		content = mention(role, agentID) + "\n\nTASK_COMPLETE\ntask_issue_id: " + taskID + "\nstatus: committed"
	}
	action := map[string]any{
		"type":            actionType,
		"target_issue_id": target,
		"task_issue_id":   taskID,
		"stage":           stage,
		"role":            role,
		"agent_id":        agentID,
		"content":         content,
	}
	if stage == "review_passed" {
		action["issue_status"] = "done"
	}
	return action
}

func patchForStage(taskID, stage string) any {
	switch stage {
	case "review_passed":
		return map[string]any{"task_issue_id": taskID, "status": "committed"}
	case "review_failed":
		return map[string]any{"task_issue_id": taskID, "status": "pending"}
	case "planning_blocked":
		return map[string]any{"task_issue_id": taskID, "status": "awaiting_clarification"}
	default:
		return nil
	}
}

func hasExpectedNotification(comments []any, markerIdx int, agentID string) bool {
	if markerIdx < 0 {
		markerIdx = 0
	}
	needle := "mention://agent/" + agentID
	for i, raw := range comments {
		if i >= markerIdx && strings.Contains(commentBody(raw), needle) {
			return true
		}
	}
	return false
}

func hasMasterTaskComplete(comments []any, taskID, agentID string) bool {
	mentionNeedle := "mention://agent/" + agentID
	for _, raw := range comments {
		body := commentBody(raw)
		if strings.Contains(body, mentionNeedle) &&
			strings.Contains(body, "TASK_COMPLETE") &&
			strings.Contains(body, "task_issue_id: "+taskID) &&
			(strings.Contains(body, "status: committed") || strings.Contains(body, "status: done")) {
			return true
		}
	}
	return false
}

func latestCommentIsStale(comments []any, now time.Time, assumeMissing bool) bool {
	if len(comments) == 0 {
		return true
	}
	if now.IsZero() {
		return false
	}
	last := object(comments[len(comments)-1])
	timestamp := firstString(last, "created_at", "createdAt", "created_date", "createdDate")
	if timestamp == "" {
		return assumeMissing
	}
	created, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		return assumeMissing
	}
	return now.Sub(created) >= 5*time.Minute
}

func parseNow(raw string) time.Time {
	if raw != "" {
		if t, err := time.Parse(time.RFC3339, raw); err == nil {
			return t
		}
	}
	return time.Time{}
}

func mention(role, id string) string {
	name := map[string]string{
		"planner":      "Coding Team Planner",
		"implementer":  "Coding Team Implementer",
		"test_writer":  "Coding Team Test Writer",
		"reviewer":     "Coding Team Reviewer",
		"orchestrator": "Coding Team Orchestrator",
	}[role]
	return "[@" + name + "](mention://agent/" + id + ")"
}

func skip(taskID, reason string) map[string]any {
	return map[string]any{"task_issue_id": taskID, "reason": reason}
}

func commentBody(raw any) string {
	comment := object(raw)
	return firstString(comment, "content", "body", "text")
}

func firstString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if s := str(m[key]); s != "" {
			return s
		}
	}
	return ""
}

func object(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func array(v any) []any {
	if a, ok := v.([]any); ok {
		return a
	}
	return []any{}
}

func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func boolValue(v any) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return false
}

package step

import "strings"

const (
	markerNotStarted             = "not_started"
	markerPlanningBlocked        = "planning_blocked"
	markerImplementationPlan     = "implementation_plan"
	markerImplementationComplete = "implementation_complete"
	markerTestsWritten           = "tests_written"
	markerReviewPass             = "review_pass"
	markerReviewFail             = "review_fail"
)

// Run computes the next deterministic coding-team handoff decision.
func Run(input map[string]any) map[string]any {
	currentRole := canonicalRole(str(input["current_role"]))
	if currentRole == "" {
		return errorMachine("INVALID_INPUT", "missing or unsupported current_role")
	}

	taskIssueID := str(input["task_issue_id"])
	masterIssueID := str(input["master_issue_id"])
	if taskIssueID == "" {
		return errorMachine("INVALID_INPUT", "missing required field: task_issue_id")
	}

	taskComments := array(input["task_comments"])
	_ = array(input["master_comments"]) // accepted for parity, intentionally not used
	agentIDs := object(input["agent_ids"])
	agentNames := object(input["agent_names"])
	options := object(input["options"])
	event := canonicalEvent(str(input["event"]))

	markers := extractMarkerSeries(taskComments)
	currentMarker, markerIndex := latestMarker(markers)
	if currentMarker == "" {
		currentMarker = markerNotStarted
	}
	if event != "" {
		expectedEvent := markerToEvent(currentMarker)
		if expectedEvent == "" {
			return errorMachine("EVENT_MARKER_MISMATCH", "event provided but no compatible marker was found")
		}
		if event != expectedEvent {
			return errorMachine("EVENT_MARKER_MISMATCH", "event mismatch: supplied "+event+" but latest marker is "+expectedEvent)
		}
	}

	failMarker := strings.TrimSpace(strings.ToLower(str(options["review_fail_requires_test_writer_marker"])))
	if failMarker == "" {
		failMarker = "requires_test_writer: true"
	}
	preferPRWriter := boolValue(options["prefer_pr_writer_after_review_pass"])

	evidence := map[string]any{
		"current_marker":                         currentMarker,
		"current_marker_index":                   markerIndex,
		"latest_planning_blocked_index":          lastIndex(markers[markerPlanningBlocked]),
		"latest_implementation_plan_index":       lastIndex(markers[markerImplementationPlan]),
		"latest_implementation_complete_index":   lastIndex(markers[markerImplementationComplete]),
		"previous_implementation_complete_index": previousIndex(markers[markerImplementationComplete]),
		"latest_tests_written_index":             lastIndex(markers[markerTestsWritten]),
		"latest_review_pass_index":               lastIndex(markers[markerReviewPass]),
		"latest_review_fail_index":               lastIndex(markers[markerReviewFail]),
	}

	var decision map[string]any
	switch currentMarker {
	case markerNotStarted:
		d, ok := decideByRole("planner", taskIssueID, markerNotStarted, "not_started", "", nil, true, "Please begin with planning.", agentIDs, agentNames)
		if !ok {
			return errorMachine("INVALID_INPUT", "missing agent id for role planner")
		}
		decision = d
	case markerPlanningBlocked:
		if _, _, err := resolveAgent(agentIDs, agentNames, "planner"); err != "" {
			return errorMachine("INVALID_INPUT", "missing agent id for role planner")
		}
		decision = map[string]any{
			"route_kind":      "blocked",
			"current_marker":  markerPlanningBlocked,
			"current_role":    currentRole,
			"next_role":       "planner",
			"next_agent_name": canonicalRoleDisplayName("planner"),
			"next_agent_id":   str(agentIDs["planner"]),
			"target_issue_id": taskIssueID,
			"target_status":   "awaiting_clarification",
			"state_patches": []any{
				map[string]any{"task_issue_id": taskIssueID, "status": "awaiting_clarification"},
			},
			"comment_content": "",
			"comment_field":   "",
			"comment_payload": map[string]any{},
			"reason":          "Task is blocked pending clarification; no handoff is emitted.",
		}
	case markerImplementationPlan:
		d, ok := decideByRole("implementer", taskIssueID, markerImplementationPlan, "implementation_plan", "planned", []any{}, true, "", agentIDs, agentNames)
		if !ok {
			return errorMachine("INVALID_INPUT", "missing agent id for role implementer")
		}
		decision = d
	case markerImplementationComplete:
		if isReviewFix(markers, failMarker, taskComments) {
			if requiresTestMarker(markers, failMarker, taskComments) {
				d, ok := decideByRole("test_writer", taskIssueID, markerImplementationComplete, "review_fix_requires_tests", "implemented", []any{
					map[string]any{"task_issue_id": taskIssueID, "status": "implemented"},
				}, true, "", agentIDs, agentNames)
				if !ok {
					return errorMachine("INVALID_INPUT", "missing agent id for role test_writer")
				}
				decision = d
			} else {
				d, ok := decideByRole("reviewer", taskIssueID, markerImplementationComplete, "review_fix", "implemented", []any{
					map[string]any{"task_issue_id": taskIssueID, "status": "implemented"},
				}, true, "", agentIDs, agentNames)
				if !ok {
					return errorMachine("INVALID_INPUT", "missing agent id for role reviewer")
				}
				decision = d
			}
		} else {
			d, ok := decideByRole("test_writer", taskIssueID, markerImplementationComplete, "normal", "implemented", []any{
				map[string]any{"task_issue_id": taskIssueID, "status": "implemented"},
			}, true, "", agentIDs, agentNames)
			if !ok {
				return errorMachine("INVALID_INPUT", "missing agent id for role test_writer")
			}
			decision = d
		}
	case markerTestsWritten:
		d, ok := decideByRole("reviewer", taskIssueID, markerTestsWritten, "normal", "tested", []any{
			map[string]any{"task_issue_id": taskIssueID, "status": "tested"},
		}, true, "", agentIDs, agentNames)
		if !ok {
			return errorMachine("INVALID_INPUT", "missing agent id for role reviewer")
		}
		decision = d
	case markerReviewFail:
		d, ok := decideByRole("implementer", taskIssueID, markerReviewFail, "review_fail", "pending", []any{
			map[string]any{"task_issue_id": taskIssueID, "status": "pending"},
		}, true, "", agentIDs, agentNames)
		if !ok {
			return errorMachine("INVALID_INPUT", "missing agent id for role implementer")
		}
		decision = d
	case markerReviewPass:
		targetRole := "orchestrator"
		if preferPRWriter {
			targetRole = "pr_writer"
		}
		target, ok := decideByRole(targetRole, masterIssueID, markerReviewPass, "review_pass", "committed", []any{
			map[string]any{"task_issue_id": taskIssueID, "status": "committed"},
		}, true, "", agentIDs, agentNames)
		if !ok {
			if targetRole == "pr_writer" {
				return errorMachine("INVALID_INPUT", "missing agent id for role pr_writer")
			}
			return errorMachine("INVALID_INPUT", "missing agent id for role orchestrator")
		}
		if masterIssueID == "" {
			return errorMachine("INVALID_INPUT", "missing required field: master_issue_id")
		}
		decision = target
	default:
		return errorMachine("INVALID_INPUT", "unsupported marker state")
	}

	if decision == nil {
		return errorMachine("INVALID_INPUT", "no decision could be generated")
	}

	decorateRecovery(decision, taskComments, markerIndex)
	return map[string]any{
		"status":          "ok",
		"summary":         "decided coding-team handoff",
		"target_issue_id": str(decision["target_issue_id"]),
		"machine_data": map[string]any{
			"decision": decision,
			"evidence": evidence,
			"warnings": []string{},
		},
	}
}

func decideByRole(nextRole, targetIssueID, marker, routeKind, nextStatus string, statePatches []any, sendComment bool, defaultReason string, agentIDs, agentNames map[string]any) (map[string]any, bool) {
	agentName, agentID, err := resolveAgent(agentIDs, agentNames, nextRole)
	if err != "" {
		return nil, false
	}
	commentField := "content"
	if marker == markerReviewPass {
		commentField = "body"
	}

	comment := ""
	if sendComment {
		comment = handoffComment(marker, nextRole, agentName, agentID, targetIssueID, "")
	}
	payload := map[string]any{}
	if commentField == "body" {
		payload["body"] = comment
	} else {
		payload["content"] = comment
	}
	reason := defaultReason
	if reason == "" {
		reason = "" + routeReason(nextRole, routeKind, marker)
	}

	return map[string]any{
		"route_kind":      routeKind,
		"current_marker":  marker,
		"current_role":    "",
		"next_role":       nextRole,
		"next_agent_name": agentName,
		"next_agent_id":   agentID,
		"target_issue_id": targetIssueID,
		"target_status":   nextStatus,
		"state_patches":   statePatches,
		"comment_content": comment,
		"comment_field":   commentField,
		"comment_payload": payload,
		"reason":          reason,
	}, true
}

func decorateRecovery(decision map[string]any, comments []any, markerIndex int) {
	nextID := str(decision["next_agent_id"])
	if nextID == "" {
		return
	}
	if markerIndex < 0 || markerIndex+1 >= len(comments) {
		return
	}
	mention := "mention://agent/" + nextID
	for _, raw := range comments[markerIndex+1:] {
		if strings.Contains(commentBody(raw), mention) {
			rk := str(decision["route_kind"])
			if !strings.Contains(rk, "duplicate_or_recovery") {
				decision["route_kind"] = rk + "_duplicate_or_recovery"
			}
			break
		}
	}
}

func isReviewFix(markers markerSeries, failMarker string, comments []any) bool {
	latestImpl := lastIndex(markers[markerImplementationComplete])
	if latestImpl < 0 {
		return false
	}
	latestFail := lastIndex(markers[markerReviewFail])
	latestPlan := lastIndex(markers[markerImplementationPlan])
	prevImpl := previousIndex(markers[markerImplementationComplete])
	cutoff := latestPlan
	if prevImpl > cutoff {
		cutoff = prevImpl
	}
	return latestFail > cutoff && latestFail < latestImpl
}

func requiresTestMarker(markers markerSeries, failMarker string, comments []any) bool {
	failIdx := lastIndex(markers[markerReviewFail])
	if failIdx < 0 {
		return false
	}
	body := commentBody(comments[failIdx])
	return strings.Contains(strings.ToLower(body), failMarker)
}

func markerToEvent(marker string) string {
	switch marker {
	case markerNotStarted:
		return ""
	case markerPlanningBlocked:
		return "planning_blocked"
	case markerImplementationPlan:
		return "implementation_plan"
	case markerImplementationComplete:
		return "implementation_complete"
	case markerTestsWritten:
		return "tests_written"
	case markerReviewPass:
		return "review_pass"
	case markerReviewFail:
		return "review_fail"
	}
	return ""
}

func canonicalEvent(raw string) string {
	s := strings.TrimSpace(strings.ToLower(raw))
	switch s {
	case "":
		return ""
	case "planning_blocked", "planning blocked":
		return "planning_blocked"
	case "implementation_plan", "implementation plan":
		return "implementation_plan"
	case "implementation_complete", "implementation complete":
		return "implementation_complete"
	case "tests_written", "tests written":
		return "tests_written"
	case "review_pass", "review pass":
		return "review_pass"
	case "review_fail", "review fail":
		return "review_fail"
	default:
		return s
	}
}

func canonicalRole(raw string) string {
	s := strings.TrimSpace(strings.ToLower(raw))
	switch s {
	case "implementer", "coding team implementer", "coding_implementer", "impl":
		return "implementer"
	case "test_writer", "test-writer", "test writer", "coding_team_test_writer", "coding team test writer":
		return "test_writer"
	case "reviewer", "coding team reviewer", "coding_reviewer":
		return "reviewer"
	case "orchestrator", "coding team orchestrator", "coding_orchestrator":
		return "orchestrator"
	case "planner", "coding team planner", "coding_planner":
		return "planner"
	case "pr_writer", "pr-writer", "pr writer", "coding pr writer", "coding_pr_writer":
		return "pr_writer"
	default:
		return ""
	}
}

func routeReason(nextRole, routeKind, marker string) string {
	if marker == markerReviewPass {
		return "review pass complete, proceed to review closeout"
	}
	switch routeKind {
	case "normal":
		return "normal flow transition: " + marker + " -> " + nextRole
	case "review_fix":
		return "review failure detected after implementation: send back to reviewer"
	case "review_fix_requires_tests":
		return "review failure with explicit test-writer marker; return to test_writer"
	case "review_fail":
		return "review failed, continue implementation"
	case "blocked":
		return "planning blocked by explicit marker"
	case "not_started":
		return "no handoff marker yet; route to planner"
	default:
		return "transition for " + marker
	}
}

func handoffComment(marker, nextRole, nextName, nextID, taskIssueID, masterIssueID string) string {
	mention := "[@" + nextName + "](mention://agent/" + nextID + ")"
	switch marker {
	case markerReviewPass:
		comment := mention + "\n\nTASK_COMPLETE\ntask_issue_id: ${TASK_ID}\nstatus: committed\nmaster_issue_id: ${MASTER_ID}"
		comment = strings.ReplaceAll(comment, "${TASK_ID}", taskIssueID)
		comment = strings.ReplaceAll(comment, "${MASTER_ID}", masterIssueID)
		return comment
	case markerReviewFail:
		if nextRole == "reviewer" {
			return mention + "\n\nReview failed. Please fix and repost ## Implementation Complete."
		}
		return mention + "\n\nPlease fix and continue."
	case markerTestsWritten:
		return mention + "\n\nTests are written. Please review."
	case markerImplementationComplete:
		if nextRole == "reviewer" {
			return mention + "\n\nImplementation fixes appear complete. Please review again."
		}
		if nextRole == "test_writer" {
			return mention + "\n\nImplementation is complete. Please write tests."
		}
	case markerImplementationPlan:
		return mention + "\n\nProceed with implementation now."
	case markerPlanningBlocked:
		return ""
	}
	return mention + "\n\nPlease continue with the next role handoff."
}

func resolveAgent(agentIDs, agentNames map[string]any, role string) (string, string, string) {
	aliases := map[string][]string{
		"implementer":  {"implementer", "coding_implementer", "agent_implementer"},
		"test_writer":  {"test_writer", "coding_test_writer", "agent_test_writer"},
		"reviewer":     {"reviewer", "coding_reviewer", "agent_reviewer"},
		"orchestrator": {"orchestrator", "coding_orchestrator", "agent_orchestrator"},
		"planner":      {"planner", "coding_planner", "agent_planner"},
		"pr_writer":    {"pr_writer", "coding_pr_writer", "agent_pr_writer"},
	}
	ids, ok := aliases[role]
	if !ok {
		return "", "", "unsupported role: " + role
	}
	for _, key := range ids {
		id := str(agentIDs[key])
		if id == "" {
			continue
		}
		name := canonicalRoleDisplayName(role)
		if nn := str(agentNames[key]); nn != "" {
			name = nn
		}
		return name, id, ""
	}
	return "", "", "missing agent id for role " + role
}

func canonicalRoleDisplayName(role string) string {
	switch role {
	case "implementer":
		return "Coding Team Implementer"
	case "test_writer":
		return "Coding Team Test Writer"
	case "reviewer":
		return "Coding Team Reviewer"
	case "orchestrator":
		return "Coding Team Orchestrator"
	case "planner":
		return "Coding Team Planner"
	case "pr_writer":
		return "Coding Team PR Writer"
	default:
		return role
	}
}

func errorMachine(code, msg string) map[string]any {
	return map[string]any{
		"status":     "error",
		"error_code": code,
		"summary":    msg,
		"machine_data": map[string]any{
			"decision": map[string]any{"error": msg},
			"evidence": map[string]any{"error_code": code},
			"warnings": []string{msg},
		},
	}
}

type markerSeries map[string][]int

func extractMarkerSeries(comments []any) markerSeries {
	out := markerSeries{
		markerPlanningBlocked:        []int{},
		markerImplementationPlan:     []int{},
		markerImplementationComplete: []int{},
		markerTestsWritten:           []int{},
		markerReviewPass:             []int{},
		markerReviewFail:             []int{},
	}

	for i, raw := range comments {
		body := strings.ToLower(commentBody(raw))
		if strings.Contains(body, "## planning blocked: clarification needed") {
			out[markerPlanningBlocked] = append(out[markerPlanningBlocked], i)
		}
		if strings.Contains(body, "## implementation plan") {
			out[markerImplementationPlan] = append(out[markerImplementationPlan], i)
		}
		if strings.Contains(body, "## implementation complete") {
			out[markerImplementationComplete] = append(out[markerImplementationComplete], i)
		}
		if strings.Contains(body, "## tests written") {
			out[markerTestsWritten] = append(out[markerTestsWritten], i)
		}
		if strings.Contains(body, "## review: pass") {
			out[markerReviewPass] = append(out[markerReviewPass], i)
		}
		if strings.Contains(body, "## review: fail") {
			out[markerReviewFail] = append(out[markerReviewFail], i)
		}
	}
	return out
}

func latestMarker(markers markerSeries) (string, int) {
	latest := -1
	marker := ""
	for key, indices := range markers {
		if len(indices) == 0 {
			continue
		}
		idx := indices[len(indices)-1]
		if idx > latest {
			latest = idx
			marker = key
		}
	}
	return marker, latest
}

func lastIndex(indices []int) int {
	if len(indices) == 0 {
		return -1
	}
	return indices[len(indices)-1]
}

func previousIndex(indices []int) int {
	if len(indices) < 2 {
		return -1
	}
	return indices[len(indices)-2]
}

func commentBody(raw any) string {
	comment := object(raw)
	for _, key := range []string{"content", "body", "text"} {
		if s := str(comment[key]); s != "" {
			return s
		}
	}
	return ""
}

func array(v any) []any {
	if a, ok := v.([]any); ok {
		return a
	}
	return []any{}
}

func object(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func boolValue(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		x = strings.TrimSpace(strings.ToLower(x))
		return x == "1" || x == "true" || x == "yes" || x == "y"
	case float64:
		return x != 0
	case int:
		return x != 0
	default:
		return false
	}
}

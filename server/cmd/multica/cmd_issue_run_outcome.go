package main

import (
	"fmt"
	"io"
	"strings"
)

// issueRunOutcome is what an issue write did about the run it could have
// started: queued, coalesced, deferred or blocked, with a stable reason code.
// Servers before #10845 send none.
type issueRunOutcome struct {
	TargetType string `json:"target_type"`
	TargetID   string `json:"target_id"`
	Status     string `json:"status"`
	ReasonCode string `json:"reason_code"`
}

// issueRunOutcomeNote says why an issue write started no new run, or returns
// "" when there is nothing to say: a run started, the server sent no outcome,
// or the caller passed --no-start and so already knows. An agent that assigned
// or promoted an issue reads this instead of assuming its run started.
func issueRunOutcomeNote(o *issueRunOutcome, noStart bool) string {
	if o == nil || noStart {
		return ""
	}
	switch o.Status {
	case "", "queued":
		return ""
	case "coalesced":
		return "no new run started: a pending run on this issue already covers it"
	case "deferred":
		if o.ReasonCode == "already_active" {
			return "no new run started: the assignee's active run on this issue covers it"
		}
		return "no run started: the issue is in backlog and runs once it moves to an active status"
	}
	return "no run started: " + blockedRunReason(o.ReasonCode)
}

func blockedRunReason(code string) string {
	text := map[string]string{
		"issue_in_triage":         "the issue is in Triage; accept it first",
		"agent_runtime_required":  "the assignee has no runtime; bind it to one",
		"target_unavailable":      "the assignee is archived or no longer exists",
		"runtime_offline":         "the assignee's runtime is offline",
		"runtime_unusable":        "the assignee's agent CLI cannot run on its runtime",
		"runtime_profile_missing": "the assignee's runtime is missing a required profile",
		"runtime_access_denied":   "the assignee's owner cannot use its runtime",
		"invocation_not_allowed":  "you may not invoke the assignee",
		"attribution_blocked":     "no responsible person could be resolved for the run",
		"project_access_blocked":  "the run's people cannot access the issue's project",
	}[code]
	if text == "" {
		return code
	}
	return fmt.Sprintf("%s (%s)", text, code)
}

// reportIssueRunOutcome prints the note for a single-issue write to w.
func reportIssueRunOutcome(w io.Writer, result map[string]any, noStart bool) {
	raw, _ := result["run_outcome"].(map[string]any)
	if raw == nil {
		return
	}
	note := issueRunOutcomeNote(&issueRunOutcome{
		Status:     strVal(raw, "status"),
		ReasonCode: strVal(raw, "reason_code"),
	}, noStart)
	if note != "" {
		fmt.Fprintf(w, "%s%s.\n", strings.ToUpper(note[:1]), note[1:])
	}
}

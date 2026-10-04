// Package eventrouting defines trusted provenance for events that may reach
// agent-routing code.
package eventrouting

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// Class identifies why an event exists.
type Class string

const (
	MemberIntent            Class = "member_intent"
	AgentResult             Class = "agent_result"
	ResultHandoff           Class = "result_handoff"
	ProviderError           Class = "provider_error"
	SystemFailureDiagnostic Class = "system_failure_diagnostic"
	RecoveryControl         Class = "recovery_control"
)

var (
	handoffConsumerIssue = regexp.MustCompile(`(?i)consumer_issue_id\s*=\s*` + "`?" + `([0-9a-f-]{36})` + "`?")
	handoffRevision      = regexp.MustCompile(`(?i)revision\s*=\s*` + "`?" + `([0-9]+)` + "`?")
)

// Actionable reports whether the platform may route this class to an agent or
// squad. Diagnostics and recovery bookkeeping are intentionally inert.
func (c Class) Actionable() bool {
	switch c {
	case MemberIntent, AgentResult, ResultHandoff:
		return true
	default:
		return false
	}
}

// CommentClass classifies a persisted comment from trusted row fields. Content
// can narrow agent output but can never make a system event actionable.
func CommentClass(authorType, commentType, content string, hasSourceTask bool) Class {
	switch authorType {
	case "member":
		return MemberIntent
	case "agent":
		upper := strings.ToUpper(strings.TrimSpace(content))
		switch {
		case strings.HasPrefix(upper, "RESULT_HANDOFF"):
			return ResultHandoff
		case strings.HasPrefix(upper, "NO_ACTION"), strings.HasPrefix(upper, "SKIP_FAILED_DELEGATION"):
			return RecoveryControl
		default:
			return AgentResult
		}
	case "system":
		if commentType == "progress_update" && hasSourceTask {
			return SystemFailureDiagnostic
		}
		return RecoveryControl
	default:
		return RecoveryControl
	}
}

// ResultHandoffTargets verifies the minimum routing contract. Prose markers
// without the expected consumer issue and a positive revision are inert.
func ResultHandoffTargets(content, issueID string) bool {
	consumer := handoffConsumerIssue.FindStringSubmatch(content)
	revision := handoffRevision.FindStringSubmatch(content)
	if len(consumer) != 2 || len(revision) != 2 || !strings.EqualFold(consumer[1], issueID) {
		return false
	}
	value, err := strconv.ParseInt(revision[1], 10, 32)
	return err == nil && value > 0
}

// ClassFromPayload derives routing metadata for legacy publishers from the
// canonical comment payload shape. New routing decisions use persisted fields.
func ClassFromPayload(actorType string, payload any) Class {
	commentType := "comment"
	content := ""
	hasSourceTask := false
	outer, ok := payload.(map[string]any)
	if !ok {
		encoded, err := json.Marshal(payload)
		if err == nil {
			_ = json.Unmarshal(encoded, &outer)
		}
	}
	if outer != nil {
		if comment, ok := outer["comment"].(map[string]any); ok {
			if value, ok := comment["author_type"].(string); ok && value != "" {
				actorType = value
			}
			if value, ok := comment["type"].(string); ok {
				commentType = value
			}
			if value, ok := comment["content"].(string); ok {
				content = value
			}
			if value, ok := comment["source_task_id"].(*string); ok {
				hasSourceTask = value != nil
			} else if value, exists := comment["source_task_id"]; exists {
				hasSourceTask = value != nil
			}
		}
	}
	return CommentClass(actorType, commentType, content, hasSourceTask)
}

// Package action defines the structured channel action contract shared by
// slash commands, dispatch, and channel turn state.
//
// Responsibilities:
//   - Represent deterministic channel actions before dispatch.
//   - Keep action kinds and sources stable for audit and idempotency.
//
// Boundaries:
//   - Does not parse natural-language user text.
//   - Does not build agent prompts or execute issue mutations.
package action

// Source identifies where a structured action came from.
type Source string

const (
	// SourceCommand means a slash/source-command path produced this action.
	SourceCommand Source = "command"
	// SourceRule means the deterministic command rule engine produced this
	// action after slash expansion.
	SourceRule Source = "rule"
	// SourceAgent means an agent turn or legacy semantic step produced this
	// action.
	SourceAgent Source = "chat"
)

// Kind is the high-level command category consumed by channel dispatch.
type Kind string

const (
	KindCreateIssue   Kind = "CreateIssue"
	KindAddComment    Kind = "AddComment"
	KindQueryIssue    Kind = "QueryIssue"
	KindQueryProgress Kind = "QueryProgress"
	KindIssueDetail   Kind = "IssueDetail"
	KindIssueTimeline Kind = "IssueTimeline"
	KindIssueLogs     Kind = "IssueLogs"
	KindSetStatus     Kind = "SetStatus"
	KindSetAssignee   Kind = "SetAssignee"
	KindSetPriority   Kind = "SetPriority"
	KindSetLabel      Kind = "SetLabel"
	KindConfirmAction Kind = "ConfirmAction"
	KindCancelAction  Kind = "CancelAction"
	KindDelete        Kind = "Delete"
	KindUnsupported   Kind = "Unsupported"
	KindUnknown       Kind = "Unknown"
	KindAskClarify    Kind = "ASK_CLARIFY"
)

// Intent is the dispatch-ready structured action attached to an inbound
// message. The name mirrors the existing port.InboundIntent wire contract.
type Intent struct {
	Kind       Kind
	Confidence float64
	Params     map[string]string
	Source     Source
}

// Result is a resolver's answer. Matched=false lets the command chain continue.
type Result struct {
	Matched bool
	Intent  Intent
	Reply   string
}

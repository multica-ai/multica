package workflows

// issue_loop_state.go holds the response shape used by the Chain v2 control
// strip. The live state itself is read through IssueLoopStateReader, declared
// in issue_loop.go and implemented by the loop bridge.

// PendingHumanCheck is one human check awaiting a decision, enough to render
// "Waiting on: <assignee> — <prompt>" plus an Approve/Reject action.
type PendingHumanCheck struct {
	CheckID      string
	Prompt       string
	AssigneeType string
	AssigneeID   string
}

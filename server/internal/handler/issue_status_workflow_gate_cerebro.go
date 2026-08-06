package handler

import (
	"context"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// IssueStatusWorkflowGateInvoker keeps the upstream issue handler unaware of
// the Cerebro Chain runtime while routing every requested status change
// through the same before.issue.status_change Workflow hook.
type IssueStatusWorkflowGateInvoker interface {
	BeforeIssueStatusChange(context.Context, db.Issue, string, string, string) (string, error)
}

package handler

import (
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// The Secretary may post at most service.MaxChallengesPerChildIssue comments on
// one child of a request. The ninth is refused with 429; an ordinary issue is
// not capped.
func TestCreateComment_SecretaryChallengeCapOnChildIssue(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	e := newInquiryEnv(t)
	secretary := e.Secretary
	squad := e.SquadID
	parent := e.issue(t, squad, "Request", "todo")
	child := e.issue(t, squad, "Child", "in_progress")
	dbfx.Exec(t, `UPDATE issue SET parent_issue_id = $1 WHERE id = $2`, parent, child)

	for i := 0; i < service.MaxChallengesPerChildIssue; i++ {
		e.reply(t, secretary, child, "challenge")
	}
	e.refuseReply(t, secretary, child, http.StatusTooManyRequests)

	// A plain issue with no parent is not a child, so the cap does not apply.
	for i := 0; i < service.MaxChallengesPerChildIssue+1; i++ {
		e.reply(t, secretary, e.IssueID, "note")
	}
}

// refuseReply posts a comment as the agent and expects the given status.
func (e inquiryEnv) refuseReply(t *testing.T, agentID, issueID string, want int) {
	t.Helper()
	taskID := dbfx.Task(t, agentID, testutil.Cols{"status": "running", "runtime_id": e.RuntimeID, "issue_id": issueID})
	req := newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{"content": "one more"})
	req.Header.Set("X-Workspace-ID", e.WorkspaceID)
	req = asAgent(req, agentID, taskID)
	req = withURLParam(req, "id", issueID)
	testutil.Call(t, testHandler.CreateComment, req).Want(want)
}

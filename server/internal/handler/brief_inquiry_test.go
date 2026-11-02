package handler

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// inquiryEnv is a workspace of its own, so the daily cap counts only what the
// test asks. It has the Secretary, a squad whose leader is e.AgentID, a worker
// in that squad, and one issue assigned to the squad.
type inquiryEnv struct {
	digestEnv
	Secretary string
	Leader    string
	Worker    string
	SquadID   string
	IssueID   string
}

func newInquiryEnv(t *testing.T) inquiryEnv {
	t.Helper()
	e := newDigestEnv(t)
	secretary := e.otherAgent(t)
	e.setSecretary(t, secretary)
	squad := e.squad(t, "Research "+uuid.NewString()[:8])
	dbfx.SquadMember(t, squad, "agent", e.AgentID, testutil.Cols{"role": "leader"})
	worker := e.otherAgent(t)
	dbfx.SquadMember(t, squad, "agent", worker, testutil.Cols{"role": "member"})
	issue := e.issue(t, squad, "Flagged issue", "in_progress")
	return inquiryEnv{digestEnv: e, Secretary: secretary, Leader: e.AgentID, Worker: worker, SquadID: squad, IssueID: issue}
}

// ask sends an inquiry as the given agent (or as the default member when empty).
func (e inquiryEnv) ask(t *testing.T, agentID string, body map[string]any) *testutil.Response {
	t.Helper()
	req := newRequest("POST", "/api/briefs/inquiries", body)
	req.Header.Set("X-Workspace-ID", e.WorkspaceID)
	if agentID != "" {
		// The Secretary runs for the boss: its task carries the human
		// originator, which the private leader's wake needs.
		taskID := dbfx.Task(t, agentID, testutil.Cols{
			"status": "running", "runtime_id": e.RuntimeID,
			"originator_source": "direct_human", "originator_user_id": testUserID, "accountable_user_id": testUserID,
		})
		req = asAgent(req, agentID, taskID)
	}
	return testutil.Call(t, testHandler.AskBriefInquiry, req)
}

func (e inquiryEnv) askBody(question string) map[string]any {
	return map[string]any{"squad_id": e.SquadID, "issue_id": e.IssueID, "question": question}
}

// reply posts a comment on an issue as an agent running a task on that issue.
func (e inquiryEnv) reply(t *testing.T, agentID, issueID, content string) {
	t.Helper()
	taskID := dbfx.Task(t, agentID, testutil.Cols{"status": "running", "runtime_id": e.RuntimeID, "issue_id": issueID})
	req := newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{"content": content})
	req.Header.Set("X-Workspace-ID", e.WorkspaceID)
	req = asAgent(req, agentID, taskID)
	req = withURLParam(req, "id", issueID)
	testutil.Call(t, testHandler.CreateComment, req).Want(http.StatusCreated)
}

func (e inquiryEnv) inquiryRow(t *testing.T, question string) (answered bool, excerpt *string) {
	t.Helper()
	var answeredAt *string
	dbfx.QueryRow(t, `SELECT answered_at::text, answer_excerpt FROM brief_inquiry WHERE workspace_id = $1 AND question = $2`,
		e.WorkspaceID, question).Scan(&answeredAt, &excerpt)
	return answeredAt != nil, excerpt
}

func TestAskBriefInquiry_OnlyTheSecretaryMayAsk(t *testing.T) {
	e := newInquiryEnv(t)
	e.ask(t, e.Leader, e.askBody("Is the pilot on track?")).Want(http.StatusForbidden)
	e.ask(t, "", e.askBody("Is the pilot on track?")).Want(http.StatusForbidden)
	if n := e.count(t); n != 0 {
		t.Fatalf("refused asks must write nothing, found %d inquiries", n)
	}
}

func (e inquiryEnv) count(t *testing.T) int {
	t.Helper()
	return dbfx.Count(t, `SELECT count(*) FROM brief_inquiry WHERE workspace_id = $1`, e.WorkspaceID)
}

func TestAskBriefInquiry_PostsTheQuestionAndWakesTheLeader(t *testing.T) {
	e := newInquiryEnv(t)
	var out BriefInquiryResponse
	e.ask(t, e.Secretary, e.askBody("Why did the pilot slip?")).Want(http.StatusCreated).JSON(&out)
	if out.SquadID != e.SquadID || out.Question != "Why did the pilot slip?" || out.AnsweredAt != nil || out.AnswerExcerpt != nil {
		t.Fatalf("unexpected inquiry: %+v", out)
	}
	if out.IssueID == nil || *out.IssueID != e.IssueID || out.CommentID == nil || out.AskedOn == "" {
		t.Fatalf("inquiry should name its issue, comment and day: %+v", out)
	}

	var content, authorType, authorID string
	dbfx.QueryRow(t, `SELECT content, author_type, author_id::text FROM comment WHERE id = $1`, *out.CommentID).
		Scan(&content, &authorType, &authorID)
	if authorType != "agent" || authorID != e.Secretary {
		t.Errorf("the question should be written by the Secretary, got %s/%s", authorType, authorID)
	}
	if !strings.Contains(content, "mention://agent/"+e.Leader) || !strings.Contains(content, "Why did the pilot slip?") {
		t.Errorf("the comment should mention the leader and carry the question: %q", content)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status IN ('queued', 'dispatched')`, e.IssueID, e.Leader); n != 1 {
		t.Errorf("the leader should be woken once by the question, got %d queued tasks", n)
	}
}

func TestAskBriefInquiry_Validation(t *testing.T) {
	e := newInquiryEnv(t)
	noIssue := map[string]any{"squad_id": e.SquadID, "question": "Anything?"}
	e.ask(t, e.Secretary, noIssue).Want(http.StatusBadRequest)
	e.ask(t, e.Secretary, map[string]any{"squad_id": e.SquadID, "issue_id": uuid.NewString(), "question": "Anything?"}).Want(http.StatusBadRequest)
	e.ask(t, e.Secretary, map[string]any{"squad_id": uuid.NewString(), "issue_id": e.IssueID, "question": "Anything?"}).Want(http.StatusNotFound)
	e.ask(t, e.Secretary, e.askBody("   ")).Want(http.StatusBadRequest)
	e.ask(t, e.Secretary, e.askBody(strings.Repeat("q", service.BriefInquiryQuestionMax+1))).Want(http.StatusBadRequest)
	if n := e.count(t); n != 0 {
		t.Fatalf("refused asks must write nothing, found %d inquiries", n)
	}
}

func TestAskBriefInquiry_DailyCapRefusesTheFourth(t *testing.T) {
	e := newInquiryEnv(t)
	for i := 1; i <= service.BriefInquiryDailyLimit; i++ {
		e.ask(t, e.Secretary, e.askBody("Question "+string(rune('A'+i)))).Want(http.StatusCreated)
	}
	res := e.ask(t, e.Secretary, e.askBody("One too many")).Want(http.StatusTooManyRequests)
	if res.Map()["code"] != "inquiry_limit" {
		t.Fatalf("code = %v, want inquiry_limit", res.Map()["code"])
	}
	if n := e.count(t); n != service.BriefInquiryDailyLimit {
		t.Fatalf("the refused ask must not be recorded: %d inquiries", n)
	}

	// The cap is per workspace: another workspace's Secretary is not refused.
	other := newInquiryEnv(t)
	other.ask(t, other.Secretary, other.askBody("Separate workspace")).Want(http.StatusCreated)
}

func TestAskBriefInquiry_SquadAgentReplyAnswersIt(t *testing.T) {
	e := newInquiryEnv(t)
	e.ask(t, e.Secretary, e.askBody("What changed on the pilot?")).Want(http.StatusCreated)
	if answered, _ := e.inquiryRow(t, "What changed on the pilot?"); answered {
		t.Fatal("an unanswered inquiry must not be marked answered")
	}

	// A reply from a member of the asked squad, on the same issue. The excerpt
	// is the first 280 characters.
	long := strings.Repeat("a", 300)
	e.reply(t, e.Worker, e.IssueID, long)
	answered, excerpt := e.inquiryRow(t, "What changed on the pilot?")
	if !answered || excerpt == nil || *excerpt != strings.Repeat("a", service.BriefInquiryAnswerExcerptMax) {
		t.Fatalf("answered=%v excerpt=%v, want the first 280 characters", answered, excerpt)
	}
}

func TestAskBriefInquiry_OnlySquadAgentsOnTheIssueAnswer(t *testing.T) {
	e := newInquiryEnv(t)
	e.ask(t, e.Secretary, e.askBody("Who owns the risk?")).Want(http.StatusCreated)

	outsider := e.otherAgent(t)
	e.reply(t, outsider, e.IssueID, "Not my squad, not my answer.")
	if answered, _ := e.inquiryRow(t, "Who owns the risk?"); answered {
		t.Error("an agent outside the asked squad must not answer")
	}

	elsewhere := e.issue(t, e.SquadID, "Another issue", "in_progress")
	e.reply(t, e.Worker, elsewhere, "Replying on a different issue.")
	if answered, _ := e.inquiryRow(t, "Who owns the risk?"); answered {
		t.Error("a reply on another issue must not answer the question")
	}

	// A second squad with the same worker is not this squad's answer.
	otherSquad := e.squad(t, "Other squad "+uuid.NewString()[:8])
	dbfx.SquadMember(t, otherSquad, "agent", outsider, testutil.Cols{"role": "member"})
	e.reply(t, outsider, e.IssueID, "A member of a different squad.")
	if answered, _ := e.inquiryRow(t, "Who owns the risk?"); answered {
		t.Error("a member of a different squad must not answer")
	}
}

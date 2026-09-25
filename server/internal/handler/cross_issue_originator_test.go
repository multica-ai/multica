package handler

import (
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// MUL-6490 / GH #7328 — the authorization chain must survive a cross-issue hop.
//
// The invariant under test (MUL-3963): a run always acts "on behalf of" exactly
// one human U, and every A2A delegation is judged by whether U is on the target's
// allow-list — never by the agent principal doing the asking. The chain's carrier
// is comment.source_task_id: an agent's comment records the run that wrote it, and
// the run that comment wakes inherits that run's originator.
//
// The bug: CreateComment only stamped source_task_id when the authoring run's task
// was on the SAME issue as the comment. An agent coordinating on an issue it just
// created wrote a NULL lineage, so the woken run resolved to unattributed and
// every @mention / assign / sub-issue inside it hit invocation_not_allowed — while
// the identical delegation on the originating issue succeeded.
//
// The fix propagates the chain unconditionally, which is monotonic: a run can only
// carry the human it ALREADY acts for, and each hop re-runs canInvokeAgent against
// that same human. What must stay impossible is SUBSTITUTING a human — falling
// back to the coordinator's owner, or adopting the target issue's originator —
// because that borrows a different person's authority. Both directions are pinned
// below, and the three entry points a woken run delegates through are driven
// through their real handlers: the risk this fix carries is precisely that one
// entry point resolves the chain differently from another.

// crossIssueChain is the {coordinator, two issues, one running task} shape every
// case here starts from. IssueX is where the coordinator's run lives; IssueY is
// the issue it coordinates on.
type crossIssueChain struct {
	CoordinatorID string
	IssueX        string
	IssueY        string
	TaskA         string // the coordinator's running task, on IssueX
}

// newCrossIssueChain seeds a coordinator agent owned by ownerUserID with a running
// task on IssueX whose originator is originatorUserID (pass nil for an
// unattributed run, e.g. a schedule/webhook autopilot dispatch).
func newCrossIssueChain(t *testing.T, ownerUserID string, originatorUserID any) crossIssueChain {
	t.Helper()
	coordinator := seedAllowListedAgent(t, "MUL-6490 coordinator", ownerUserID, "private")
	issueX := seedChainIssue(t, "MUL-6490 originating issue", coordinator)
	return crossIssueChain{
		CoordinatorID: coordinator,
		IssueX:        issueX,
		IssueY:        seedChainIssue(t, "MUL-6490 coordinated issue", coordinator),
		TaskA: dbfx.Task(t, coordinator, testutil.Cols{
			"runtime_id":          handlerTestRuntimeID(t),
			"issue_id":            issueX,
			"status":              "running",
			"originator_user_id":  originatorUserID,
			"accountable_user_id": originatorUserID,
		}),
	}
}

// seedChainIssue inserts an agent-created issue and registers teardown for the
// rows the HANDLERS will add to it. dbfx removes only what it inserted itself, and
// these tests exist to make real handlers write comments and enqueue tasks.
//
// The number comes from the workspace counter rather than dbfx's MAX(number)+1
// default: CreateIssue allocates from that counter, so a fixture issue that
// bypasses it collides with the sub-issue the test asks the real handler to make.
func seedChainIssue(t *testing.T, title, creatorAgentID string) string {
	t.Helper()
	issueID := dbfx.Issue(t, title, testutil.Cols{
		"creator_type": "agent",
		"creator_id":   creatorAgentID,
		"number":       nextWorkspaceIssueNumber(t),
	})
	dbfx.Cleanup(t, `DELETE FROM comment WHERE issue_id = $1`, issueID)
	dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
	return issueID
}

// seedAllowListedAgent creates an agent with the given permission mode. Any
// memberTargets make it public_to and allow-list exactly those users — the
// minimum-privilege configuration the report ran ("only Bohan may invoke this").
// The grants are removed explicitly because the project forbids DB cascades, so
// they would outlive the agent row dbfx deletes.
func seedAllowListedAgent(t *testing.T, name, ownerUserID, mode string, memberTargets ...string) string {
	t.Helper()
	agentID := dbfx.Agent(t, name, handlerTestRuntimeID(t), testutil.Cols{
		"permission_mode":      mode,
		"owner_id":             ownerUserID,
		"max_concurrent_tasks": 5,
		"instructions":         "",
		"custom_env":           testutil.Raw("'{}'::jsonb"),
		"custom_args":          testutil.Raw("'[]'::jsonb"),
		"mcp_config":           testutil.Raw("'[]'::jsonb"),
	})
	for _, target := range memberTargets {
		dbfx.InsertNoID(t, "agent_invocation_target", testutil.Cols{
			"agent_id":    agentID,
			"target_type": "member",
			"target_id":   target,
		}, "agent_id = $1 AND target_type = 'member' AND target_id = $2", agentID, target)
	}
	return agentID
}

// ---- entry points ---------------------------------------------------------
//
// The three ways a run delegates. Each goes through the real handler, because the
// failure this fix repairs is entry-point wiring: the gate and the enqueue used to
// resolve the chain from different places, and only the wired path shows that.

// agentComments posts a comment as the agent, speaking from taskID — the only way
// an agent's lineage reaches the comment row. A comment-triggered run must reply
// under its own trigger, so replyUnder carries that parent; it is empty for a run
// with no trigger comment to answer.
func agentComments(t *testing.T, agentID, taskID, issueID, content, replyUnder string) *testutil.Response {
	t.Helper()
	body := map[string]any{"content": content}
	if replyUnder != "" {
		body["parent_id"] = replyUnder
	}
	return testutil.Call(t, testHandler.CreateComment, testutil.WithURLParams(
		asRun(newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", body), agentID, taskID),
		"id", issueID,
	))
}

// agentAssigns points an existing issue at targetAgentID through UpdateIssue.
func agentAssigns(t *testing.T, agentID, taskID, issueID, targetAgentID string) *testutil.Response {
	t.Helper()
	return testutil.Call(t, testHandler.UpdateIssue, testutil.WithURLParams(
		asRun(newRequest(http.MethodPatch, "/api/issues/"+issueID, map[string]any{
			"assignee_type": "agent",
			"assignee_id":   targetAgentID,
		}), agentID, taskID),
		"id", issueID,
	))
}

// agentCreatesSubIssue creates a `todo` sub-issue assigned to targetAgentID, which
// dispatches that agent immediately — the third way the report saw the chain fail.
// The title is unique per target so the active-duplicate guard never masks the
// authorization outcome this asserts.
func agentCreatesSubIssue(t *testing.T, agentID, taskID, parentIssueID, targetAgentID string) *testutil.Response {
	t.Helper()
	resp := testutil.Call(t, testHandler.CreateIssue, asRun(
		newRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
			"title":           "MUL-6490 delegated sub-issue for " + targetAgentID,
			"status":          "todo",
			"priority":        "medium",
			"assignee_type":   "agent",
			"assignee_id":     targetAgentID,
			"parent_issue_id": parentIssueID,
		}), agentID, taskID))
	if resp.Code == http.StatusCreated {
		var created struct {
			ID string `json:"id"`
		}
		resp.JSON(&created)
		dbfx.Cleanup(t, `DELETE FROM issue WHERE id = $1`, created.ID)
		dbfx.Cleanup(t, `DELETE FROM comment WHERE issue_id = $1`, created.ID)
		dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id = $1`, created.ID)
	}
	return resp
}

// asRun makes a request speak as the agent from inside one of its runs. The
// member headers newRequest sets stay put on purpose: X-User-ID remains a
// workspace member while the ACTOR is the agent, which is exactly the
// confused-deputy shape the gate must judge by the chain's human, not the caller's
// session.
func asRun(req *http.Request, agentID, taskID string) *http.Request {
	return testutil.WithHeaders(req, "X-Agent-ID", agentID, "X-Task-ID", taskID)
}

// ---- reads ----------------------------------------------------------------

// queuedTaskFor returns the queued task for (issue, agent) and its originator.
// ok is false when the delegation was refused and nothing was enqueued.
func queuedTaskFor(t *testing.T, issueID, agentID string) (taskID string, originator pgtype.UUID, ok bool) {
	t.Helper()
	const where = `FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'`
	if dbfx.Count(t, `SELECT count(*) `+where, issueID, agentID) == 0 {
		return "", pgtype.UUID{}, false
	}
	dbfx.QueryRow(t, `SELECT id, originator_user_id `+where, issueID, agentID).Scan(&taskID, &originator)
	return taskID, originator, true
}

func commentSourceTaskOf(t *testing.T, commentID string) pgtype.UUID {
	t.Helper()
	var sourceTaskID pgtype.UUID
	dbfx.QueryRow(t, `SELECT source_task_id FROM comment WHERE id = $1`, commentID).Scan(&sourceTaskID)
	return sourceTaskID
}

func lastCommentOn(t *testing.T, issueID string) string {
	t.Helper()
	var commentID string
	dbfx.QueryRow(t,
		`SELECT id FROM comment WHERE issue_id = $1 ORDER BY created_at DESC, id DESC LIMIT 1`,
		issueID).Scan(&commentID)
	return commentID
}

// triggerCommentOf returns the comment a task was woken by, which is the only
// parent its replies may use (taskCoversReplyParent).
func triggerCommentOf(t *testing.T, taskID string) string {
	t.Helper()
	var triggerCommentID pgtype.UUID
	dbfx.QueryRow(t, `SELECT trigger_comment_id FROM agent_task_queue WHERE id = $1`, taskID).Scan(&triggerCommentID)
	return uuidToString(triggerCommentID)
}

func runTo(t *testing.T, taskID string) {
	t.Helper()
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'running' WHERE id = $1`, taskID)
}

func mention(agentID string) string {
	return "[@Worker](mention://agent/" + agentID + ") please take this"
}

// TestCrossIssueDelegation_OriginatorChainSurvivesTheHop is the reported bug, end
// to end over HTTP: the delegation that works on the coordinator's own issue must
// keep working when the coordinator delegates on another issue, and the woken run
// must carry the SAME human — that inherited originator is what every later
// mention / assign / sub-issue in it is judged by.
func TestCrossIssueDelegation_OriginatorChainSurvivesTheHop(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	// bohan owns the coordinator and is the only member on the worker's allow-list.
	bohan := testUserID
	worker := seedAllowListedAgent(t, "MUL-6490 worker", bohan, "public_to", bohan)

	for _, tc := range []struct {
		name  string
		cross bool
	}{
		{"same issue (already worked)", false},
		{"cross issue (the regression)", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			chain := newCrossIssueChain(t, bohan, bohan)
			target := chain.IssueX
			if tc.cross {
				target = chain.IssueY
			}

			agentComments(t, chain.CoordinatorID, chain.TaskA, target, mention(worker), "").
				Want(http.StatusCreated)

			// The comment must record the run that wrote it. This is the carrier;
			// a NULL here is the break the report saw.
			if got := commentSourceTaskOf(t, lastCommentOn(t, target)); uuidToString(got) != chain.TaskA {
				t.Fatalf("comment.source_task_id = %q, want the authoring run %q", uuidToString(got), chain.TaskA)
			}

			taskID, originator, ok := queuedTaskFor(t, target, worker)
			if !ok {
				t.Fatal("the allow-listed worker was not enqueued: the delegation was refused")
			}
			if uuidToString(originator) != bohan {
				t.Fatalf("woken run originator = %q (valid=%v), want %q — the chain lost its human across the hop",
					uuidToString(originator), originator.Valid, bohan)
			}

			// A third hop proves the chain keeps travelling rather than surviving
			// exactly one boundary. Entry-point coverage is in the sibling test.
			t.Run("the chain continues past the woken run", func(t *testing.T) {
				runTo(t, taskID)
				second := seedAllowListedAgent(t, "MUL-6490 second worker", bohan, "public_to", bohan)

				agentComments(t, worker, taskID, target, mention(second), triggerCommentOf(t, taskID)).
					Want(http.StatusCreated)
				if _, _, ok := queuedTaskFor(t, target, second); !ok {
					t.Fatal("@mention from inside the woken run was refused (invocation_not_allowed)")
				}
			})
		})
	}
}

// TestCrossIssueDelegation_EveryEntryPointJudgesTheSameHuman is the matrix that
// matters for this fix: {mention, assign, sub-issue} × {bohan's chain, alice's
// chain, no chain}, all from inside a run woken across an issue boundary.
//
// Two things are pinned at once. The chain must be CARRIED — all three entry
// points recover for the human who started it. And it must never be REPLACED:
// alice triggers a coordinator that BOHAN owns, so the owner-inheritance fallback
// the report proposed would silently upgrade alice's flow to bohan's authority.
// Every entry point resolves the acting human through its own wiring, so a fix
// that repairs one and not the others is a real outcome this catches.
func TestCrossIssueDelegation_EveryEntryPointJudgesTheSameHuman(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	bohan := testUserID
	_, _, alice := privateAgentTestFixture(t)

	// firstHop admits both members, so every case reaches the second hop and the
	// difference isolates to whose authority the chain carries. secondHop admits
	// ONLY bohan — it is the agent an owner fallback would wrongly unlock.
	firstHop := seedAllowListedAgent(t, "MUL-6490 shared worker", bohan, "public_to", bohan, alice)
	secondHop := seedAllowListedAgent(t, "MUL-6490 bohan-only worker", bohan, "public_to", bohan)

	for _, tc := range []struct {
		name    string
		chainOf any
		admit   bool
	}{
		{"bohan's chain reaches a bohan-only agent", bohan, true},
		{"alice's chain does not become bohan's", alice, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The coordinator is owned by BOHAN in every case: only the human at
			// the top of the chain may vary the outcome.
			chain := newCrossIssueChain(t, bohan, tc.chainOf)
			agentComments(t, chain.CoordinatorID, chain.TaskA, chain.IssueY, mention(firstHop), "").
				Want(http.StatusCreated)

			firstTaskID, originator, ok := queuedTaskFor(t, chain.IssueY, firstHop)
			if !ok {
				t.Fatal("first hop was refused although both members are allow-listed")
			}
			if uuidToString(originator) != tc.chainOf {
				t.Fatalf("first hop originator = %q, want %q", uuidToString(originator), tc.chainOf)
			}
			runTo(t, firstTaskID)

			t.Run("mention", func(t *testing.T) {
				agentComments(t, firstHop, firstTaskID, chain.IssueY, mention(secondHop),
					triggerCommentOf(t, firstTaskID)).Want(http.StatusCreated)
				// A blocked mention is reported in trigger_outcomes, not as an
				// error status, so the enqueue is what says yes or no.
				if _, _, got := queuedTaskFor(t, chain.IssueY, secondHop); got != tc.admit {
					t.Fatalf("mention admitted = %v, want %v", got, tc.admit)
				}
			})

			t.Run("assign", func(t *testing.T) {
				want := http.StatusForbidden
				if tc.admit {
					want = http.StatusOK
				}
				resp := agentAssigns(t, firstHop, firstTaskID, chain.IssueY, secondHop).Want(want)
				if !tc.admit {
					assertDenialReason(t, resp, "you do not have permission to assign work to this agent")
				}
			})

			t.Run("sub-issue", func(t *testing.T) {
				want := http.StatusForbidden
				if tc.admit {
					want = http.StatusCreated
				}
				resp := agentCreatesSubIssue(t, firstHop, firstTaskID, chain.IssueY, secondHop).Want(want)
				if !tc.admit {
					assertDenialReason(t, resp, "you do not have permission to assign work to this agent")
				}
			})
		})
	}

	// A chain with no human at its top grants nothing anywhere, which is the
	// fail-closed side of the same rule: propagation carries a human, it never
	// invents one. The first hop already stops here, so there is no second hop to
	// drive — a member-scoped allow-list has nobody to match.
	t.Run("an unattributed chain grants nothing", func(t *testing.T) {
		chain := newCrossIssueChain(t, bohan, nil)
		agentComments(t, chain.CoordinatorID, chain.TaskA, chain.IssueY, mention(firstHop), "").
			Want(http.StatusCreated)
		if _, _, ok := queuedTaskFor(t, chain.IssueY, firstHop); ok {
			t.Fatal("an unattributed chain must not satisfy a member-scoped allow-list")
		}
		agentAssigns(t, chain.CoordinatorID, chain.TaskA, chain.IssueY, firstHop).Want(http.StatusForbidden)
		agentCreatesSubIssue(t, chain.CoordinatorID, chain.TaskA, chain.IssueY, firstHop).Want(http.StatusForbidden)
	})
}

// TestCrossIssueDelegation_GateAndStampAgreeOnTheHuman closes the split-brain the
// bug created: the invoke gate resolved the human from the request's X-Task-ID
// (cross-issue OK) while the enqueued run resolved it from the stored comment
// (cross-issue NULL). The gate said yes and the run started with no authority, so
// the failure only surfaced one hop later. Both resolvers must answer identically
// for the same action.
func TestCrossIssueDelegation_GateAndStampAgreeOnTheHuman(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	bohan := testUserID
	worker := seedAllowListedAgent(t, "MUL-6490 agreement worker", bohan, "public_to", bohan)
	chain := newCrossIssueChain(t, bohan, bohan)

	agentComments(t, chain.CoordinatorID, chain.TaskA, chain.IssueY, mention(worker), "").
		Want(http.StatusCreated)
	commentID := lastCommentOn(t, chain.IssueY)

	// What the gate saw at admission time, from the request header.
	fromGate := testHandler.invokeOriginatorFromRequest(
		asRun(newRequest(http.MethodPost, "/api/issues/"+chain.IssueY+"/comments", nil), chain.CoordinatorID, chain.TaskA),
		"agent", chain.CoordinatorID)

	// What the enqueue path sees afterwards, from the persisted comment.
	fromStamp := uuidToString(testHandler.TaskService.ResolveOriginatorFromTriggerComment(
		t.Context(), parseUUID(testWorkspaceID), parseUUID(commentID)))

	if fromGate != fromStamp {
		t.Fatalf("gate resolved %q but the stored chain resolves %q — a run would start with authority the gate never granted",
			fromGate, fromStamp)
	}
	if fromGate != bohan {
		t.Fatalf("both resolvers agree on %q, want %q", fromGate, bohan)
	}
}

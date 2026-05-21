package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// seedSquadForBriefing creates a squad with the seeded test agent as
// leader. Returns the loaded db.Squad and a cleanup-registered ID.
func seedSquadForBriefing(t *testing.T, leaderID string, name, instructions string) db.Squad {
	t.Helper()
	ctx := context.Background()

	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id, instructions)
		VALUES ($1, $2, '', $3, $4, $5)
		RETURNING id
	`, testWorkspaceID, name, leaderID, testUserID, instructions).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM squad WHERE id = $1`, squadID)
	})

	uuid := util.MustParseUUID(squadID)
	squad, err := testHandler.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{
		ID:          uuid,
		WorkspaceID: util.MustParseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("load squad: %v", err)
	}
	return squad
}

func addAgentMember(t *testing.T, squadID pgtype.UUID, agentID, role string) {
	t.Helper()
	if _, err := testHandler.Queries.AddSquadMember(context.Background(), db.AddSquadMemberParams{
		SquadID:    squadID,
		MemberType: "agent",
		MemberID:   util.MustParseUUID(agentID),
		Role:       role,
	}); err != nil {
		t.Fatalf("add agent member: %v", err)
	}
}

func addHumanMember(t *testing.T, squadID pgtype.UUID, userID, role string) {
	t.Helper()
	if _, err := testHandler.Queries.AddSquadMember(context.Background(), db.AddSquadMemberParams{
		SquadID:    squadID,
		MemberType: "member",
		MemberID:   util.MustParseUUID(userID),
		Role:       role,
	}); err != nil {
		t.Fatalf("add human member: %v", err)
	}
}

// seededLeaderAgent loads the first seeded agent in the test workspace.
func seededLeaderAgent(t *testing.T) (id, name string) {
	t.Helper()
	if err := testPool.QueryRow(context.Background(), `
		SELECT id, name FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1
	`, testWorkspaceID).Scan(&id, &name); err != nil {
		t.Fatalf("load seeded agent: %v", err)
	}
	return id, name
}

// seededHumanMember returns the (member_row_id, user_id, user_name) of the
// test fixture's human member in the workspace.
func seededHumanMember(t *testing.T) (memberID, userID, userName string) {
	t.Helper()
	if err := testPool.QueryRow(context.Background(), `
		SELECT m.id, u.id, u.name
		FROM member m JOIN "user" u ON u.id = m.user_id
		WHERE m.workspace_id = $1 ORDER BY m.created_at ASC LIMIT 1
	`, testWorkspaceID).Scan(&memberID, &userID, &userName); err != nil {
		t.Fatalf("load seeded member: %v", err)
	}
	return
}

func TestBuildSquadLeaderBriefing_FullSquad(t *testing.T) {
	ctx := context.Background()
	leaderID, leaderName := seededLeaderAgent(t)

	squad := seedSquadForBriefing(t, leaderID, "Full Squad", "Always write tests.")

	helper1 := createHandlerTestAgent(t, "Helper One", []byte("[]"))
	helper2 := createHandlerTestAgent(t, "Helper Two", []byte("[]"))
	addAgentMember(t, squad.ID, helper1, "implementer")
	addAgentMember(t, squad.ID, helper2, "")

	memberRowID, userID, userName := seededHumanMember(t)
	_ = memberRowID
	addHumanMember(t, squad.ID, userID, "reviewer")

	out := buildSquadLeaderBriefing(ctx, testHandler.Queries, squad)

	for _, want := range []string{
		"## Squad Operating Protocol",
		"## Squad Roster",
		"Leader (you):",
		leaderName,
		"## Squad Instructions (Full Squad)",
		"Always write tests.",
		"`[@Helper One](mention://agent/" + helper1 + ")`",
		"`[@Helper Two](mention://agent/" + helper2 + ")`",
		`role: "implementer"`,
		`role: "reviewer"`,
		"`[@" + userName + "](mention://member/" + userID + ")`",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected briefing to contain %q\n--- briefing ---\n%s", want, out)
		}
	}

	// Helper Two has no role — must NOT render an empty role: "" segment.
	if strings.Contains(out, `Helper Two — agent, role: ""`) {
		t.Errorf("expected empty role to be omitted, got: %s", out)
	}
}

func TestBuildSquadLeaderBriefing_OnlyLeader(t *testing.T) {
	ctx := context.Background()
	leaderID, _ := seededLeaderAgent(t)
	squad := seedSquadForBriefing(t, leaderID, "Solo Squad", "")

	out := buildSquadLeaderBriefing(ctx, testHandler.Queries, squad)
	if !strings.Contains(out, "Members: (none — you are the only member of this squad)") {
		t.Errorf("expected lone-leader fallback line, got:\n%s", out)
	}
	// No user instructions → no Squad Instructions section.
	if strings.Contains(out, "## Squad Instructions") {
		t.Errorf("expected no Squad Instructions section when empty, got:\n%s", out)
	}
}

func TestBuildSquadLeaderBriefing_SkipsArchivedAgent(t *testing.T) {
	ctx := context.Background()
	leaderID, _ := seededLeaderAgent(t)
	squad := seedSquadForBriefing(t, leaderID, "Archive Squad", "")

	archived := createHandlerTestAgent(t, "Retired Bot", []byte("[]"))
	addAgentMember(t, squad.ID, archived, "")
	if _, err := testPool.Exec(ctx,
		`UPDATE agent SET archived_at = now(), archived_by = $1 WHERE id = $2`,
		testUserID, archived,
	); err != nil {
		t.Fatalf("archive agent: %v", err)
	}

	out := buildSquadLeaderBriefing(ctx, testHandler.Queries, squad)
	if strings.Contains(out, "Retired Bot") {
		t.Errorf("archived agent should not appear in roster:\n%s", out)
	}
	if strings.Contains(out, archived) {
		t.Errorf("archived agent UUID should not appear in roster:\n%s", out)
	}
}

// TestBuildSquadLeaderBriefing_MentionsRoundTrip is the contract test
// guaranteeing every emitted mention markdown string parses back through
// util.ParseMentions to its (type, id). If this ever breaks, the leader's
// dispatch comments will silently fail to trigger anyone.
func TestBuildSquadLeaderBriefing_MentionsRoundTrip(t *testing.T) {
	ctx := context.Background()
	leaderID, _ := seededLeaderAgent(t)
	squad := seedSquadForBriefing(t, leaderID, "Mention Round Trip", "")

	helper := createHandlerTestAgent(t, "Round Trip Bot", []byte("[]"))
	addAgentMember(t, squad.ID, helper, "")

	memberRowID, userID, _ := seededHumanMember(t)
	_ = memberRowID
	addHumanMember(t, squad.ID, userID, "")

	out := buildSquadLeaderBriefing(ctx, testHandler.Queries, squad)
	mentions := util.ParseMentions(out)

	wantIDs := map[string]string{
		leaderID: "agent",
		helper:   "agent",
		userID:   "member",
	}
	got := make(map[string]string, len(mentions))
	for _, m := range mentions {
		got[m.ID] = m.Type
	}
	for id, kind := range wantIDs {
		if got[id] != kind {
			t.Errorf("expected %s mention for id %s, got %q (all parsed: %#v)", kind, id, got[id], mentions)
		}
	}
}

// claimAndDecodeAgent runs ClaimTaskByRuntime for the given runtime and
// returns the agent block of the response. Fails the test on non-200.
func claimAndDecodeAgent(t *testing.T, runtimeID string) *TaskAgentData {
t.Helper()
w := httptest.NewRecorder()
req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/claim", nil, testWorkspaceID, "test-claim-squad-briefing")
req = withURLParam(req, "runtimeId", runtimeID)
testHandler.ClaimTaskByRuntime(w, req)
if w.Code != http.StatusOK {
t.Fatalf("ClaimTaskByRuntime: %d %s", w.Code, w.Body.String())
}
var resp struct {
Task *struct {
Agent *TaskAgentData `json:"agent"`
} `json:"task"`
}
if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
t.Fatalf("decode: %v", err)
}
if resp.Task == nil || resp.Task.Agent == nil {
t.Fatalf("expected task.agent in response, got: %s", w.Body.String())
}
return resp.Task.Agent
}

// queueSquadIssueTaskFor creates an issue assigned to the squad and a queued
// task for the given (agentID, runtimeID). Returns the issue + task IDs.
func queueSquadIssueTaskFor(t *testing.T, squadID, agentID, runtimeID string, issueNumber int) (issueID, taskID string) {
t.Helper()
ctx := context.Background()
if err := testPool.QueryRow(ctx, `
INSERT INTO issue (
workspace_id, title, status, priority, creator_id, creator_type,
assignee_type, assignee_id, number, position
) VALUES ($1, 'Squad briefing claim test', 'todo', 'medium', $2, 'member',
'squad', $3, $4, 0)
RETURNING id
`, testWorkspaceID, testUserID, squadID, issueNumber).Scan(&issueID); err != nil {
t.Fatalf("create squad-assigned issue: %v", err)
}
t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

if err := testPool.QueryRow(ctx, `
INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
VALUES ($1, $2, $3, 'queued', 0)
RETURNING id
`, agentID, runtimeID, issueID).Scan(&taskID); err != nil {
t.Fatalf("queue task: %v", err)
}
t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })
return
}

// TestClaimTask_LeaderGetsBriefing — when the squad leader claims a task on
// a squad-assigned issue, the response's agent.instructions must include
// the Operating Protocol + Roster + user instructions.
func TestClaimTask_LeaderGetsBriefing(t *testing.T) {
if testHandler == nil {
t.Skip("database not available")
}
ctx := context.Background()

var leaderID, runtimeID string
if err := testPool.QueryRow(ctx,
`SELECT id, runtime_id FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1`,
testWorkspaceID,
).Scan(&leaderID, &runtimeID); err != nil {
t.Fatalf("get leader agent: %v", err)
}

squad := seedSquadForBriefing(t, leaderID, "Briefing Claim Squad", "Be terse.")

helper := createHandlerTestAgent(t, "Briefing Helper", []byte("[]"))
addAgentMember(t, squad.ID, helper, "implementer")

queueSquadIssueTaskFor(t, util.UUIDToString(squad.ID), leaderID, runtimeID, 95001)

agent := claimAndDecodeAgent(t, runtimeID)
for _, want := range []string{
"## Squad Operating Protocol",
"## Squad Roster",
"Leader (you):",
"## Squad Instructions (Briefing Claim Squad)",
"Be terse.",
"`[@Briefing Helper](mention://agent/" + helper + ")`",
} {
if !strings.Contains(agent.Instructions, want) {
t.Errorf("expected agent.instructions to contain %q\n--- instructions ---\n%s", want, agent.Instructions)
}
}
}

// TestClaimTask_NonLeaderGetsNoBriefing — when a non-leader squad member
// claims a task on a squad-assigned issue, NO briefing is injected.
func TestClaimTask_NonLeaderGetsNoBriefing(t *testing.T) {
if testHandler == nil {
t.Skip("database not available")
}
ctx := context.Background()

var leaderID string
if err := testPool.QueryRow(ctx,
`SELECT id FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1`,
testWorkspaceID,
).Scan(&leaderID); err != nil {
t.Fatalf("get leader agent: %v", err)
}

squad := seedSquadForBriefing(t, leaderID, "Non-Leader Squad", "Squad guidance.")

// Create a second agent (NOT the leader) with its own runtime so the
// claim path picks its task without ambiguity.
helperID := createHandlerTestAgent(t, "Non Leader Helper", []byte("[]"))
addAgentMember(t, squad.ID, helperID, "")
var helperRuntime string
if err := testPool.QueryRow(ctx,
`SELECT runtime_id FROM agent WHERE id = $1`, helperID,
).Scan(&helperRuntime); err != nil {
t.Fatalf("get helper runtime: %v", err)
}

queueSquadIssueTaskFor(t, util.UUIDToString(squad.ID), helperID, helperRuntime, 95002)

agent := claimAndDecodeAgent(t, helperRuntime)
for _, mustNot := range []string{
"Squad Operating Protocol",
"Squad Roster",
"Squad Instructions (Non-Leader Squad)",
} {
if strings.Contains(agent.Instructions, mustNot) {
t.Errorf("non-leader claim should NOT contain %q\n--- instructions ---\n%s", mustNot, agent.Instructions)
}
}
}

// Avoid "imported and not used: pgtype" if helpers above are the only users.
var _ pgtype.UUID

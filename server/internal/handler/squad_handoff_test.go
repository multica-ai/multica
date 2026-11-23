package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestSquadHandoffRound(t *testing.T) {
	a, b, c := parseUUID("00000000-0000-0000-0000-00000000000a"), parseUUID("00000000-0000-0000-0000-00000000000b"), parseUUID("00000000-0000-0000-0000-00000000000c")
	row := func(kind string, from, to pgtype.UUID) db.ActivityLog {
		details, _ := json.Marshal(map[string]string{"kind": kind, "to_id": uuidToString(to)})
		return db.ActivityLog{ActorID: from, Details: details}
	}
	cases := []struct {
		name    string
		history []db.ActivityLog // newest first
		want    int
	}{
		{name: "first handoff", want: 1},
		{name: "after a leader dispatch", history: []db.ActivityLog{row(squadHandoffKindDispatch, c, a)}, want: 1},
		{name: "reply after one round", history: []db.ActivityLog{row(squadHandoffKindMember, b, a), row(squadHandoffKindMember, a, b)}, want: 2},
		{name: "third round", history: []db.ActivityLog{
			row(squadHandoffKindMember, b, a), row(squadHandoffKindMember, a, b),
			row(squadHandoffKindMember, b, a), row(squadHandoffKindMember, a, b),
		}, want: 3},
		{name: "a third member breaks the streak", history: []db.ActivityLog{
			row(squadHandoffKindMember, b, a), row(squadHandoffKindMember, c, b), row(squadHandoffKindMember, a, b),
		}, want: 1},
		{name: "a person in between breaks the streak", history: []db.ActivityLog{
			row(squadHandoffKindMember, b, a), row(squadHandoffKindHuman, a, c), row(squadHandoffKindMember, a, b),
		}, want: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := squadHandoffRound(tc.history, a, b); got != tc.want {
				t.Fatalf("round = %d, want %d", got, tc.want)
			}
		})
	}
}

type squadHandoffFixture struct {
	issueID, squadID                  string
	leaderID, memberA, memberB        string
	leaderRuntime, runtimeA, runtimeB string
	memberATask                       string
}

func newSquadHandoffFixture(t *testing.T, mode string) squadHandoffFixture {
	t.Helper()
	var f squadHandoffFixture
	f.leaderRuntime = dbfx.Runtime(t, "Handoff leader runtime")
	f.leaderID = dbfx.Agent(t, "Handoff Leader", f.leaderRuntime)
	f.runtimeA = dbfx.Runtime(t, "Handoff A runtime")
	f.memberA = dbfx.Agent(t, "Handoff Builder", f.runtimeA)
	f.runtimeB = dbfx.Runtime(t, "Handoff B runtime")
	f.memberB = dbfx.Agent(t, "Handoff Reviewer", f.runtimeB)
	f.squadID = dbfx.Squad(t, "Handoff squad", f.leaderID, testutil.Cols{"handoff_mode": mode})
	dbfx.SquadMember(t, f.squadID, "agent", f.memberA)
	dbfx.SquadMember(t, f.squadID, "agent", f.memberB)
	f.issueID = dbfx.Issue(t, "Squad handoff", testutil.Cols{
		"status": "in_progress", "assignee_type": "squad", "assignee_id": f.squadID,
	})
	f.memberATask = dbfx.Task(t, f.memberA, testutil.Cols{
		"runtime_id": f.runtimeA, "issue_id": f.issueID, "status": "running",
		"originator_user_id": testUserID, "accountable_user_id": testUserID,
	})
	return f
}

func (f squadHandoffFixture) postAsMemberA(t *testing.T, content string) {
	t.Helper()
	req := withURLParam(newRequest(http.MethodPost, "/api/issues/"+f.issueID+"/comments", map[string]any{
		"content": content,
	}), "id", f.issueID)
	req.Header.Set("X-Agent-ID", f.memberA)
	req.Header.Set("X-Task-ID", f.memberATask)
	testutil.Call(t, testHandler.CreateComment, req).Want(http.StatusCreated)
}

func (f squadHandoffFixture) queuedTasks(t *testing.T, agentID string) int {
	t.Helper()
	var n int
	dbfx.QueryRow(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'`, f.issueID, agentID).Scan(&n)
	return n
}

func (f squadHandoffFixture) handoffs(t *testing.T) []map[string]string {
	t.Helper()
	rows, err := testHandler.Queries.ListRecentSquadHandoffs(t.Context(), db.ListRecentSquadHandoffsParams{IssueID: parseUUID(f.issueID), RowLimit: 50})
	if err != nil {
		t.Fatal(err)
	}
	out := make([]map[string]string, 0, len(rows))
	for _, row := range rows {
		var d map[string]string
		if err := json.Unmarshal(row.Details, &d); err != nil {
			t.Fatal(err)
		}
		d["actor_id"] = uuidToString(row.ActorID)
		out = append(out, d)
	}
	return out
}

func (f squadHandoffFixture) seedMemberHandoff(t *testing.T, from, to string) {
	t.Helper()
	dbfx.Insert(t, "activity_log", testutil.Cols{
		"workspace_id": testWorkspaceID, "issue_id": f.issueID, "actor_type": "agent", "actor_id": from,
		"action":  service.SquadHandoffAction,
		"details": fmt.Sprintf(`{"kind":"member","to_type":"agent","to_id":%q}`, to),
	})
}

func handoffMention(kind, name, id string) string {
	return fmt.Sprintf("[@%s](mention://%s/%s)", name, kind, id)
}

func TestDirectSquadHandoffRouting(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	t.Run("member to member skips the leader", func(t *testing.T) {
		f := newSquadHandoffFixture(t, service.SquadHandoffModeDirect)
		f.postAsMemberA(t, handoffMention("agent", "Reviewer", f.memberB)+" please review the PR")
		if f.queuedTasks(t, f.memberB) != 1 || f.queuedTasks(t, f.leaderID) != 0 {
			t.Fatal("a member handoff must wake only the named member")
		}
		got := f.handoffs(t)
		if len(got) != 1 || got[0]["kind"] != "member" || got[0]["to_id"] != f.memberB || got[0]["actor_id"] != f.memberA || got[0]["round"] != "" {
			t.Fatalf("handoff records = %+v", got)
		}
	})

	t.Run("member to a person does not wake the leader", func(t *testing.T) {
		f := newSquadHandoffFixture(t, service.SquadHandoffModeDirect)
		f.postAsMemberA(t, handoffMention("member", "Owner", testUserID)+" which option should ship?")
		if f.queuedTasks(t, f.leaderID) != 0 {
			t.Fatal("asking a person must not wake the leader in direct mode")
		}
		got := f.handoffs(t)
		if len(got) != 1 || got[0]["kind"] != "human" || got[0]["to_id"] != testUserID {
			t.Fatalf("handoff records = %+v", got)
		}
	})

	t.Run("no next owner goes back to the leader", func(t *testing.T) {
		f := newSquadHandoffFixture(t, service.SquadHandoffModeDirect)
		f.postAsMemberA(t, "Done: the change is merged-ready.")
		if f.queuedTasks(t, f.leaderID) != 1 {
			t.Fatal("a reply naming no next owner must reach the leader")
		}
		got := f.handoffs(t)
		if len(got) != 1 || got[0]["kind"] != "leader" || got[0]["to_id"] != f.leaderID || got[0]["reason"] != "" {
			t.Fatalf("handoff records = %+v", got)
		}
	})

	t.Run("mentioning the leader is recorded and ends the round", func(t *testing.T) {
		f := newSquadHandoffFixture(t, service.SquadHandoffModeDirect)
		f.seedMemberHandoff(t, f.memberA, f.memberB)
		f.seedMemberHandoff(t, f.memberB, f.memberA)
		f.postAsMemberA(t, handoffMention("agent", "Leader", f.leaderID)+" the spec contradicts itself, please decide")
		if f.queuedTasks(t, f.leaderID) != 1 {
			t.Fatal("an explicit leader mention must reach the leader")
		}
		got := f.handoffs(t)
		if got[0]["kind"] != "leader" || got[0]["to_id"] != f.leaderID || got[0]["reason"] != "" {
			t.Fatalf("handoff records = %+v", got)
		}
		f.postAsMemberA(t, handoffMention("agent", "Reviewer", f.memberB)+" decided, please review")
		if got := f.handoffs(t); got[0]["kind"] != "member" || got[0]["round"] != "" {
			t.Fatalf("a pass after going to the leader must start at round 1, got %+v", got[0])
		}
	})

	t.Run("second round is recorded", func(t *testing.T) {
		f := newSquadHandoffFixture(t, service.SquadHandoffModeDirect)
		f.seedMemberHandoff(t, f.memberA, f.memberB)
		f.seedMemberHandoff(t, f.memberB, f.memberA)
		f.postAsMemberA(t, handoffMention("agent", "Reviewer", f.memberB)+" fixed, please re-review")
		got := f.handoffs(t)
		if f.queuedTasks(t, f.memberB) != 1 || got[0]["kind"] != "member" || got[0]["round"] != "2" {
			t.Fatalf("handoff records = %+v", got)
		}
	})

	t.Run("round limit sends the issue to the leader", func(t *testing.T) {
		f := newSquadHandoffFixture(t, service.SquadHandoffModeDirect)
		for i := 0; i < service.SquadHandoffRoundLimit; i++ {
			f.seedMemberHandoff(t, f.memberA, f.memberB)
			f.seedMemberHandoff(t, f.memberB, f.memberA)
		}
		f.postAsMemberA(t, handoffMention("agent", "Reviewer", f.memberB)+" one more pass please")
		if f.queuedTasks(t, f.memberB) != 0 || f.queuedTasks(t, f.leaderID) != 1 {
			t.Fatal("a handoff past the round limit must go to the leader instead")
		}
		got := f.handoffs(t)
		if got[0]["kind"] != "leader" || got[0]["reason"] != "round_limit" {
			t.Fatalf("handoff records = %+v", got)
		}
	})

	t.Run("daily limit sends the issue to the leader", func(t *testing.T) {
		f := newSquadHandoffFixture(t, service.SquadHandoffModeDirect)
		other := dbfx.Agent(t, "Handoff Third", f.runtimeB)
		dbfx.SquadMember(t, f.squadID, "agent", other)
		// Alternate pairs so no single pair reaches the round limit.
		for i := 0; i < service.SquadHandoffDailyLimit; i++ {
			if i%2 == 0 {
				f.seedMemberHandoff(t, other, f.memberB)
			} else {
				f.seedMemberHandoff(t, f.memberB, other)
			}
			f.seedMemberHandoff(t, f.memberA, other)
		}
		f.postAsMemberA(t, handoffMention("agent", "Reviewer", f.memberB)+" please review")
		if f.queuedTasks(t, f.memberB) != 0 || f.queuedTasks(t, f.leaderID) != 1 {
			t.Fatal("a handoff past the daily limit must go to the leader instead")
		}
		if got := f.handoffs(t); got[0]["reason"] != "daily_limit" {
			t.Fatalf("handoff records = %+v", got[0])
		}
	})

	t.Run("leader mode keeps today's routing", func(t *testing.T) {
		f := newSquadHandoffFixture(t, service.SquadHandoffModeLeader)
		f.postAsMemberA(t, handoffMention("member", "Owner", testUserID)+" which option should ship?")
		if f.queuedTasks(t, f.leaderID) != 1 {
			t.Fatal("leader mode must still wake the leader on a member reply")
		}
		if got := f.handoffs(t); len(got) != 0 {
			t.Fatalf("leader mode must not record handoffs, got %+v", got)
		}
	})
}

func TestClaim_SquadMemberTaskGetsMemberBrief(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	for _, mode := range []string{service.SquadHandoffModeLeader, service.SquadHandoffModeDirect} {
		t.Run(mode, func(t *testing.T) {
			f := newSquadHandoffFixture(t, mode)
			dbfx.Exec(t, `UPDATE squad SET instructions = 'Every change needs a second review.' WHERE id = $1`, f.squadID)
			dbfx.Exec(t, `UPDATE squad_member SET role = 'reviews PRs' WHERE squad_id = $1 AND member_id = $2`, f.squadID, f.memberB)
			want := dbfx.Task(t, f.memberB, testutil.Cols{
				"runtime_id": f.runtimeB, "issue_id": f.issueID, "status": "queued",
				"originator_user_id": testUserID, "accountable_user_id": testUserID,
			})
			got, instr, isLeader, raw := claimAgentInstructionsForTest(t, f.runtimeB)
			if got != want || isLeader {
				t.Fatalf("claimed %q leader=%v, want member task %q: %s", got, isLeader, want, raw)
			}
			for _, part := range []string{"## Squad Member Brief", `Your role in this squad: "reviews PRs"`, "Every change needs a second review.", "mention://agent/" + f.memberA} {
				if !strings.Contains(instr, part) {
					t.Fatalf("member brief misses %q:\n%s", part, instr)
				}
			}
			if strings.Contains(instr, "## Squad Operating Protocol") || strings.Contains(instr, "mention://agent/"+f.memberB) {
				t.Fatalf("member brief must not carry the leader protocol or offer the member itself:\n%s", instr)
			}
			direct := strings.Contains(instr, "hand the issue to each other directly")
			if direct != (mode == service.SquadHandoffModeDirect) {
				t.Fatalf("handoff section does not follow mode %s:\n%s", mode, instr)
			}
		})
	}
}

func TestUpdateSquadHandoffMode(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	f := newSquadHandoffFixture(t, service.SquadHandoffModeLeader)
	put := func(mode string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		testHandler.UpdateSquad(w, squadScopeReq("", http.MethodPut, "/api/squads/"+f.squadID, map[string]any{"handoff_mode": mode}, map[string]string{"id": f.squadID}))
		return w
	}
	if w := put("sometimes"); w.Code != http.StatusBadRequest {
		t.Fatalf("unknown mode: got %d", w.Code)
	}
	w := put(service.SquadHandoffModeDirect)
	var resp SquadResponse
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &resp) != nil || resp.HandoffMode != service.SquadHandoffModeDirect {
		t.Fatalf("set direct: %d %s", w.Code, w.Body.String())
	}
	// A write that leaves the mode out keeps it.
	w = httptest.NewRecorder()
	testHandler.UpdateSquad(w, squadScopeReq("", http.MethodPut, "/api/squads/"+f.squadID, map[string]any{"description": "kept"}, map[string]string{"id": f.squadID}))
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &resp) != nil || resp.HandoffMode != service.SquadHandoffModeDirect {
		t.Fatalf("partial update changed the mode: %d %s", w.Code, w.Body.String())
	}
}

func TestListSquadBriefs(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	f := newSquadHandoffFixture(t, service.SquadHandoffModeDirect)
	w := httptest.NewRecorder()
	testHandler.ListSquadBriefs(w, squadScopeReq("", http.MethodGet, "/api/squads/"+f.squadID+"/briefs", nil, map[string]string{"id": f.squadID}))
	var resp SquadBriefsResponse
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &resp) != nil {
		t.Fatalf("list briefs: %d %s", w.Code, w.Body.String())
	}
	if resp.HandoffMode != service.SquadHandoffModeDirect || len(resp.Briefs) != 3 {
		t.Fatalf("briefs = %+v", resp)
	}
	if !resp.Briefs[0].IsLeader || resp.Briefs[0].MemberID != f.leaderID || !strings.Contains(resp.Briefs[0].Brief, "## Handoff Mode: Direct") {
		t.Fatalf("leader brief = %+v", resp.Briefs[0])
	}
	for _, b := range resp.Briefs[1:] {
		if b.IsLeader || !strings.Contains(b.Brief, "## Squad Member Brief") {
			t.Fatalf("member brief = %+v", b)
		}
	}

	stranger := createPlainMember(t, "squad-briefs-stranger@multica.test")
	w = httptest.NewRecorder()
	testHandler.ListSquadBriefs(w, squadScopeReq(stranger, http.MethodGet, "/api/squads/"+f.squadID+"/briefs", nil, map[string]string{"id": f.squadID}))
	if w.Code != http.StatusForbidden {
		t.Fatalf("a member who cannot edit the squad: got %d", w.Code)
	}
}

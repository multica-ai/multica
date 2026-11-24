package handler

import (
	"context"
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

func TestWorkspaceWakeupsInventory(t *testing.T) {
	issue := dbfx.Issue(t, "wakeup inventory")
	agent := dbfx.Agent(t, "inventory target", testRuntimeID)
	dbfx.Cleanup(t, "DELETE FROM issue_wakeup WHERE issue_id=$1", issue)
	dbfx.Cleanup(t, "DELETE FROM issue_wakeup_receipt WHERE wakeup_id IN (SELECT id FROM issue_wakeup WHERE issue_id=$1)", issue)
	svc := service.IssueWakeupService{Tasks: testHandler.TaskService}
	var wakes []db.IssueWakeup
	for i := 0; i < 5; i++ {
		w, err := svc.Create(context.Background(), parseUUID(issue), parseUUID(testUserID), pgtype.UUID{}, service.WakeupInput{AgentID: agent, Kind: "event", EventTypes: []string{"task.completed"}, Instruction: "PRIVATE PROMPT"})
		if err != nil {
			t.Fatal(err)
		}
		wakes = append(wakes, w)
	}
	// The latest task pointer is terminal, but an older retry is still active.
	run := dbfx.Task(t, agent, testutil.Cols{"issue_id": issue, "runtime_id": testRuntimeID, "status": "running", "started_at": testutil.Raw("now()"), "context": fmt.Sprintf(`{"wakeup_id":%q}`, uuidToString(wakes[0].ID))})
	last := dbfx.Task(t, agent, testutil.Cols{"issue_id": issue, "runtime_id": testRuntimeID, "status": "completed", "context": fmt.Sprintf(`{"wakeup_id":%q}`, uuidToString(wakes[0].ID))})
	dbfx.Exec(t, "UPDATE issue_wakeup SET enabled=false,last_task_id=$2 WHERE id=$1", wakes[0].ID, last)
	if _, err := svc.Disable(context.Background(), parseUUID(issue), wakes[1].ID, parseUUID(testUserID)); err != nil {
		t.Fatal(err)
	}
	dbfx.Exec(t, "UPDATE issue_wakeup SET enabled=false WHERE id=$1", wakes[2].ID)
	sourceAgent := dbfx.Agent(t, "private source", testRuntimeID)
	sourceRun := dbfx.Task(t, sourceAgent, testutil.Cols{"issue_id": issue, "runtime_id": testRuntimeID, "status": "completed"})
	dbfx.Exec(t, "UPDATE issue_wakeup SET filter_agent_id=$2,filter_task_id=$3 WHERE id=$1", wakes[4].ID, sourceAgent, sourceRun)
	type row struct {
		ID              string  `json:"id"`
		CanManage       bool    `json:"can_manage"`
		ActiveRuns      int     `json:"active_runs"`
		FilterAgentID   *string `json:"filter_agent_id"`
		FilterAgentName *string `json:"filter_agent_name"`
		FilterTaskID    *string `json:"filter_task_id"`
		Task            *struct {
			ID string `json:"id"`
		} `json:"task"`
	}
	type page struct {
		Items  []row          `json:"items"`
		Total  int            `json:"total"`
		Counts map[string]int `json:"counts"`
		Agents []any          `json:"agents"`
	}
	read := func(req *http.Request, want int) page {
		t.Helper()
		rec := httptest.NewRecorder()
		testHandler.ListWorkspaceWakeups(rec, req)
		if rec.Code != want {
			t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
		}
		var out page
		if want == 200 {
			if strings.Contains(rec.Body.String(), "PRIVATE PROMPT") || strings.Contains(rec.Body.String(), "instruction") {
				t.Fatal("prompt in inventory")
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
		}
		return out
	}
	get := func(query string) page { return read(newRequest("GET", "/api/issue-wakeups?"+query, nil), 200) }
	p := get("")
	if p.Total != 3 || len(p.Items) != 3 || p.Counts["all"] != 5 || p.Counts["disabled"] != 1 || p.Counts["ended"] != 1 {
		t.Fatalf("bad inventory %+v", p)
	}
	found := false
	for _, r := range p.Items {
		if !r.CanManage {
			t.Fatal("owner cannot manage")
		}
		if r.ID == uuidToString(wakes[0].ID) {
			found = true
			if r.Task == nil || r.Task.ID != run || r.ActiveRuns != 1 {
				t.Fatalf("lost retry %+v", r)
			}
		}
	}
	if !found {
		t.Fatal("consumed active run disappeared")
	}
	a, b := get("scope=all&limit=2"), get("scope=all&limit=2&offset=2")
	if a.Total != 5 || b.Total != 5 || len(a.Items) != 2 || len(b.Items) != 2 {
		t.Fatal("bad pagination")
	}
	for _, x := range a.Items {
		for _, y := range b.Items {
			if x.ID == y.ID {
				t.Fatal("duplicate page row")
			}
		}
	}
	p = get("scope=all&offset=999")
	if p.Total != 5 || len(p.Items) != 0 {
		t.Fatal("empty page lost count")
	}
	if p = get("search=INVENTORY&kind=event"); p.Total != 3 {
		t.Fatal("search failed")
	}
	if p = get("kind=recurring"); p.Total != 0 || p.Counts["all"] != 5 {
		t.Fatal("filter changed inventory counts")
	}
	for _, q := range []string{"scope=oops", "kind=oops", "limit=0", "limit=101", "offset=-1", "offset=2147483648", "agent_id=bad"} {
		read(newRequest("GET", "/?"+q, nil), 400)
	}
	outsider := dbfx.User(t, "inventory outsider", "inventory-outsider@multica.test")
	req := newRequest("GET", "/?scope=all", nil)
	req.Header.Set("X-User-ID", outsider)
	read(req, 404)
	dbfx.Member(t, testWorkspaceID, outsider, "member")
	p = read(req, 200)
	if p.Total != 5 || len(p.Agents) != 1 || p.Counts["all"] != 5 {
		t.Fatal("shared issue rules disappeared from inventory")
	}
	for _, r := range p.Items {
		if r.CanManage || r.FilterAgentID != nil || r.FilterAgentName != nil || r.FilterTaskID != nil {
			t.Fatal("shared inventory granted management or private source access")
		}
	}
	// The public target is visible, but the rule remains managed by its creator.
	dbfx.Exec(t, "UPDATE agent SET visibility='workspace',permission_mode='public_to' WHERE id=$1", agent)
	dbfx.Insert(t, "agent_invocation_target", testutil.Cols{"agent_id": agent, "target_type": "workspace", "target_id": testWorkspaceID})
	p = read(req, 200)
	if p.Total != 5 {
		t.Fatalf("public inventory missing %+v", p)
	}
	for _, r := range p.Items {
		if r.CanManage {
			t.Fatal("member can manage another creator's rule")
		}
		if r.FilterAgentID != nil || r.FilterAgentName != nil || r.FilterTaskID != nil {
			t.Fatal("private source exposed")
		}
	}
	dbfx.Exec(t, "UPDATE agent_task_queue SET originator_user_id=$2,accountable_user_id=$2 WHERE id=$1", run, outsider)
	trusted := newRequest("GET", "/?scope=all", nil)
	trusted.Header.Set("X-Agent-ID", agent)
	trusted.Header.Set("X-Task-ID", run)
	trusted.Header.Set("X-Actor-Source", "task_token")
	for _, r := range read(trusted, 200).Items {
		if r.CanManage {
			t.Fatal("management flag borrowed runtime owner rights")
		}
	}
	other := dbfx.Workspace(t, "other inventory", "other-inventory")
	dbfx.Member(t, other, testUserID, "owner")
	req = newRequest("GET", "/?scope=all", nil)
	req.Header.Set("X-Workspace-ID", other)
	if p = read(req, 200); p.Total != 0 || len(p.Agents) != 0 {
		t.Fatal("cross workspace inventory")
	}
	// Terminal issues stay visible as ended, except already-running work.
	dbfx.Exec(t, "UPDATE issue SET status='done' WHERE id=$1", issue)
	p = get("")
	if p.Total != 1 || p.Items[0].Task == nil || p.Items[0].Task.ID != run {
		t.Fatalf("closed running state %+v", p)
	}
}

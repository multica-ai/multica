package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func createStagedChild(t *testing.T, parentID string, stage int, status string) IssueResponse {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.CreateIssue(w, newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title": "staged child " + time.Now().Format(time.RFC3339Nano), "status": status,
		"parent_issue_id": parentID, "stage": stage,
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("create staged child: %d %s", w.Code, w.Body.String())
	}
	var child IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&child); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupChildDoneIssue(child.ID) })
	return child
}

func listSystemWakeupsFor(t *testing.T, issueID string) []systemWakeupResponse {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.ListIssueSystemWakeups(w, withURLParam(newRequest("GET", "/api/issues/"+issueID+"/system-wakeups", nil), "id", issueID))
	if w.Code != http.StatusOK {
		t.Fatalf("list system wakeups: %d %s", w.Code, w.Body.String())
	}
	var rules []systemWakeupResponse
	if err := json.NewDecoder(w.Body).Decode(&rules); err != nil {
		t.Fatal(err)
	}
	return rules
}

func putChildDoneRule(t *testing.T, issueID string, body map[string]any, headers ...string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := withURLParams(newRequest("PUT", "/api/issues/"+issueID+"/system-wakeups/child_done", body), "id", issueID, "rule", "child_done")
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	testHandler.UpdateIssueSystemWakeup(w, req)
	return w
}

func TestSystemWakeupDescribesUnstagedWait(t *testing.T) {
	fx := newChildDoneFixture(t, "in_progress")
	rules := listSystemWakeupsFor(t, fx.parent.ID)
	if len(rules) != 1 {
		t.Fatalf("expected the child-done rule, got %+v", rules)
	}
	rule := rules[0]
	if rule.Rule != "child_done" || !rule.Enabled || rule.Staged || rule.Stage != nil || rule.Total != 1 || rule.Remaining != 1 {
		t.Fatalf("unexpected rule: %+v", rule)
	}
	if len(rule.Waiting) != 1 || rule.Waiting[0] != fx.child.Identifier || rule.Blocked != "no_assignee" || rule.DefaultInstruction != service.ChildDoneDefaultInstruction {
		t.Fatalf("waiting/blocked: %+v", rule)
	}
	updateChildStatus(t, fx.child.ID, "done")
	if rules = listSystemWakeupsFor(t, fx.parent.ID); len(rules) != 0 {
		t.Fatalf("finished sub-issues still reported: %+v", rules)
	}
	if rules = listSystemWakeupsFor(t, fx.child.ID); len(rules) != 0 {
		t.Fatalf("issue without sub-issues reports a rule: %+v", rules)
	}
}

func TestSystemWakeupDescribesLowestOpenStage(t *testing.T) {
	fx := newChildDoneFixture(t, "in_progress")
	// The fixture child is unstaged; staged siblings form the stages.
	createStagedChild(t, fx.parent.ID, 1, "done")
	open := createStagedChild(t, fx.parent.ID, 1, "in_progress")
	later := createStagedChild(t, fx.parent.ID, 2, "backlog")
	rules := listSystemWakeupsFor(t, fx.parent.ID)
	if len(rules) != 1 || !rules[0].Staged || rules[0].Stage == nil || *rules[0].Stage != 1 {
		t.Fatalf("expected stage 1: %+v", rules)
	}
	if rules[0].Total != 2 || rules[0].Remaining != 1 || rules[0].Waiting[0] != open.Identifier {
		t.Fatalf("stage progress: %+v", rules[0])
	}
	// Once every stage closed, the rule waits for the unstaged sub-issue.
	updateChildStatus(t, open.ID, "done")
	updateChildStatus(t, later.ID, "done")
	rules = listSystemWakeupsFor(t, fx.parent.ID)
	if len(rules) != 1 || rules[0].Staged || rules[0].Stage != nil || rules[0].Remaining != 1 || rules[0].Waiting[0] != fx.child.Identifier {
		t.Fatalf("expected the unstaged wait: %+v", rules)
	}
}

// Turning the rule off for one issue stops it there; an instruction set on the
// issue replaces the default in the run the assignee reads.
func TestSystemWakeupOverrideControlsChildDone(t *testing.T) {
	t.Run("disabled", func(t *testing.T) {
		fx := newChildDoneFixture(t, "in_progress")
		if w := putChildDoneRule(t, fx.parent.ID, map[string]any{"enabled": false, "instruction": ""}); w.Code != http.StatusOK {
			t.Fatalf("disable: %d %s", w.Code, w.Body.String())
		}
		if rules := listSystemWakeupsFor(t, fx.parent.ID); len(rules) != 1 || rules[0].Enabled || !rules[0].Customized || rules[0].ID == "" {
			t.Fatalf("override not reported: %+v", rules)
		}
		updateChildStatus(t, fx.child.ID, "done")
		if got := len(childDoneEntries(t, fx.parent.ID)); got != 0 {
			t.Fatalf("disabled rule still fired: %d entries", got)
		}
	})
	t.Run("instruction", func(t *testing.T) {
		fx := newChildDoneFixture(t, "in_progress")
		setIssueAssigneeDirect(t, fx.parent.ID, "agent", handlerTestAgentID(t))
		if w := putChildDoneRule(t, fx.parent.ID, map[string]any{"instruction": "  Ask Jiayuan to confirm the window first.  "}); w.Code != http.StatusOK {
			t.Fatalf("save: %d %s", w.Code, w.Body.String())
		}
		updateChildStatus(t, fx.child.ID, "done")
		runs := childDoneRuns(t, fx.parent.ID)
		if len(runs) != 1 || !strings.Contains(runs[0].Note, "Instruction:\nAsk Jiayuan to confirm the window first.\n") || strings.Contains(runs[0].Note, service.ChildDoneDefaultInstruction) {
			t.Fatalf("issue instruction not delivered: %+v", runs)
		}
	})
	t.Run("validation", func(t *testing.T) {
		fx := newChildDoneFixture(t, "in_progress")
		if w := putChildDoneRule(t, fx.parent.ID, map[string]any{"enabled": true, "instruction": strings.Repeat("x", 4001)}); w.Code != http.StatusBadRequest {
			t.Fatalf("oversized instruction: %d", w.Code)
		}
		if w := putChildDoneRule(t, fx.parent.ID, map[string]any{"enabled": true, "extra": 1}); w.Code != http.StatusBadRequest {
			t.Fatalf("unknown field: %d", w.Code)
		}
		w := httptest.NewRecorder()
		testHandler.UpdateIssueSystemWakeup(w, withURLParams(newRequest("PUT", "/api/issues/"+fx.parent.ID+"/system-wakeups/other", map[string]any{"enabled": true}), "id", fx.parent.ID, "rule", "other"))
		if w.Code != http.StatusNotFound {
			t.Fatalf("unknown rule: %d", w.Code)
		}
	})
	t.Run("agents cannot change it", func(t *testing.T) {
		fx := newChildDoneFixture(t, "in_progress")
		agentID := handlerTestAgentID(t)
		run := dbfx.Task(t, agentID, testutil.Cols{"issue_id": fx.parent.ID, "runtime_id": testRuntimeID, "status": "running"})
		if w := putChildDoneRule(t, fx.parent.ID, map[string]any{"enabled": false}, "X-Agent-ID", agentID, "X-Task-ID", run, "X-Actor-Source", "task_token"); w.Code != http.StatusForbidden {
			t.Fatalf("agent change: %d %s", w.Code, w.Body.String())
		}
	})
	t.Run("not through the person's rule endpoints", func(t *testing.T) {
		fx := newChildDoneFixture(t, "in_progress")
		if w := putChildDoneRule(t, fx.parent.ID, map[string]any{"enabled": true}); w.Code != http.StatusOK {
			t.Fatalf("create rule: %d", w.Code)
		}
		id := listSystemWakeupsFor(t, fx.parent.ID)[0].ID
		for name, handler := range map[string]http.HandlerFunc{"disable": testHandler.DisableIssueWakeup, "delete": testHandler.DeleteIssueWakeup, "trigger": testHandler.TriggerIssueWakeup} {
			rec := httptest.NewRecorder()
			handler(rec, withURLParams(newRequest("POST", "/", nil), "id", fx.parent.ID, "wakeupID", id))
			if rec.Code != http.StatusForbidden {
				t.Fatalf("%s a system rule: %d %s", name, rec.Code, rec.Body.String())
			}
		}
	})
}

// The workspace default applies to every rule nobody changed on its issue;
// owners and admins set it.
func TestWorkspaceSystemWakeupDefault(t *testing.T) {
	var settings []byte
	dbfx.QueryRow(t, "SELECT settings FROM workspace WHERE id=$1", testWorkspaceID).Scan(&settings)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), "UPDATE workspace SET settings=$2 WHERE id=$1", testWorkspaceID, settings)
	})
	follows := newChildDoneFixture(t, "in_progress")
	custom := newChildDoneFixture(t, "in_progress")
	if w := putChildDoneRule(t, custom.parent.ID, map[string]any{"enabled": true}); w.Code != http.StatusOK {
		t.Fatalf("customize: %d", w.Code)
	}
	if _, err := (&service.IssueWakeupService{Tasks: testHandler.TaskService}).BackfillChildDoneRules(context.Background(), 1000); err != nil {
		t.Fatal(err)
	}
	put := func(body map[string]any, user string) *httptest.ResponseRecorder {
		req := withURLParam(newRequest("PUT", "/api/system-wakeups/child_done", body), "rule", "child_done")
		req.Header.Set("X-User-ID", user)
		rec := httptest.NewRecorder()
		testHandler.UpdateWorkspaceSystemWakeup(rec, req)
		return rec
	}
	outsider := dbfx.User(t, "plain member", fmt.Sprintf("plain-%d@example.com", time.Now().UnixNano()))
	dbfx.Member(t, testWorkspaceID, outsider, "member")
	if rec := put(map[string]any{"enabled": false}, outsider); rec.Code != http.StatusForbidden {
		t.Fatalf("plain member changed the default: %d", rec.Code)
	}
	rec := put(map[string]any{"enabled": false, "instruction": "Wrap up in the parent."}, testUserID)
	if rec.Code != http.StatusOK {
		t.Fatalf("set default: %d %s", rec.Code, rec.Body.String())
	}
	var defaults []workspaceSystemWakeupResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &defaults); err != nil || len(defaults) != 1 || defaults[0].Enabled ||
		defaults[0].Instruction != "Wrap up in the parent." || defaults[0].BuiltinInstruction != service.ChildDoneDefaultInstruction || defaults[0].Customized < 1 {
		t.Fatalf("defaults = %+v (%v)", defaults, err)
	}
	if rules := listSystemWakeupsFor(t, follows.parent.ID); len(rules) != 1 || rules[0].Enabled || rules[0].DefaultInstruction != "Wrap up in the parent." {
		t.Fatalf("uncustomized rule did not follow the default: %+v", rules)
	}
	if rules := listSystemWakeupsFor(t, custom.parent.ID); len(rules) != 1 || !rules[0].Enabled {
		t.Fatalf("customized rule followed the default: %+v", rules)
	}
	updateChildStatus(t, follows.child.ID, "done")
	if got := len(childDoneEntries(t, follows.parent.ID)); got != 0 {
		t.Fatalf("rule off by default fired: %d", got)
	}
	// Turning the default back on does not fire for what closed meanwhile.
	if rec := put(map[string]any{"enabled": true}, testUserID); rec.Code != http.StatusOK {
		t.Fatalf("enable default: %d", rec.Code)
	}
	dbfx.Exec(t, "UPDATE issue SET status='in_review' WHERE id=$1", follows.parent.ID)
	runWakeupTick(t)
	if got := len(childDoneEntries(t, follows.parent.ID)); got != 0 {
		t.Fatalf("re-enabling fired for an old fact: %d", got)
	}
}

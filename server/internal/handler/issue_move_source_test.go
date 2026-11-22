package handler

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// The mounted UI host sends source controls even when a relative move does
// not start a run. Keep the narrow move contract compatible with that client.
func TestMoveIssueSourceControls(t *testing.T) {
	p := sourceTestProject(t, map[string]any{"title": "Move source contract"})
	a := createHandlerTestAgent(t, "Move consent agent", []byte("[]"))
	before := dbfx.Issue(t, "Before", testutil.Cols{"project_id": p.ID, "position": 10})
	after := dbfx.Issue(t, "After", testutil.Cols{"project_id": p.ID, "position": 20})
	i := dbfx.Issue(t, "Move consent", testutil.Cols{"project_id": p.ID, "status": "todo", "position": 30})
	move := func(body map[string]any, code int) {
		t.Helper()
		testutil.Call(t, testHandler.MoveIssue, withURLParam(newRequest("POST", "/api/issues/"+i+"/move", body), "id", i)).Want(code)
	}
	state := func(position float64, queued int, assigned bool) {
		t.Helper()
		var pos float64
		var hasAssignee bool
		var intent string
		dbfx.QueryRow(t, "SELECT position, assignee_id IS NOT NULL FROM issue WHERE id=$1", i).Scan(&pos, &hasAssignee)
		dbfx.QueryRow(t, "SELECT source_intent FROM project WHERE id=$1", p.ID).Scan(&intent)
		if pos != position || hasAssignee != assigned || queuedTaskCountFor(t, i, a) != queued || intent != "unspecified" {
			t.Fatalf("position=%v assigned=%v queued=%d intent=%s", pos, hasAssignee, queuedTaskCountFor(t, i, a), intent)
		}
	}
	move(map[string]any{"before_id": before, "after_id": after, "source_decision": "ask", "source_decisions": map[string]string{}}, 200)
	state(15, 0, false)
	// A run-producing move must fail before either the order or assignment is
	// written. The server is authoritative even if the host cache was cold.
	body := map[string]any{"before_id": after, "after_id": nil, "assignee_type": "agent", "assignee_id": a, "source_decision": "ask", "source_decisions": map[string]string{}}
	move(body, 409)
	state(15, 0, false)
	body["source_decisions"] = map[string]string{"unrelated-project": "no_code_once"}
	move(body, 409)
	state(15, 0, false)
	body["source_decisions"] = map[string]string{p.ID: "no_code_once"}
	move(body, 200)
	state(21, 1, true)
	var raw []byte
	dbfx.QueryRow(t, "SELECT context FROM agent_task_queue WHERE issue_id=$1 AND agent_id=$2", i, a).Scan(&raw)
	var ctx map[string]any
	if json.Unmarshal(raw, &ctx) != nil || ctx["source_no_code"] != true {
		t.Fatalf("move lost request-only consent: %s", raw)
	}
	move(body, 200)
	state(21, 1, true)
}

func TestMoveIssueSourceControlsPreserveValidation(t *testing.T) {
	i := dbfx.Issue(t, "Move validation", testutil.Cols{"position": 42})
	for _, tc := range []struct {
		name    string
		fields  map[string]any
		message string
	}{
		{"decision type", map[string]any{"source_decision": 1}, "invalid request body"},
		{"decisions type", map[string]any{"source_decisions": []string{"no_code_once"}}, "invalid request body"},
		{"decision value type", map[string]any{"source_decisions": map[string]any{"project": true}}, "invalid request body"},
		{"unknown field", map[string]any{"position": 7}, "unsupported move field: position"},
		{"invalid UUID", map[string]any{"before_id": "not-a-uuid"}, "invalid before_id"},
		{"self anchor", map[string]any{"before_id": i}, "before_id cannot be the moved issue"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := map[string]any{"before_id": nil, "after_id": nil, "source_decision": "ask", "source_decisions": map[string]string{}}
			for key, value := range tc.fields {
				body[key] = value
			}
			w := httptest.NewRecorder()
			testHandler.MoveIssue(w, withURLParam(newRequest("POST", "/api/issues/"+i+"/move", body), "id", i))
			if w.Code != 400 || !strings.Contains(w.Body.String(), tc.message) {
				t.Fatalf("got %d %s", w.Code, w.Body.String())
			}
			var position float64
			dbfx.QueryRow(t, "SELECT position FROM issue WHERE id=$1", i).Scan(&position)
			if position != 42 {
				t.Fatalf("invalid request moved issue to %v", position)
			}
		})
	}
}

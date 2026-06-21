// CEREBRO-PATCH(cerebro-permissions-cli-test): FIR-1609 tests for the
// `multica permissions` CLI request shaping, validation, and explain filters.
package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
)

const (
	permTestWorkspaceID = "11111111-1111-1111-1111-111111111111"
	permTestAgentID     = "22222222-2222-2222-2222-222222222222"
	permTestUserID      = "33333333-3333-3333-3333-333333333333"
)

// permTestServer records the last request the CLI sent and replies with a fixed
// table for GET. It returns a client pointed at it plus a pointer to the recorded
// request so each test can assert on method, path, query, and body.
type permRecordedReq struct {
	Method string
	Path   string
	Query  string
	Body   map[string]any
}

func permTestServer(t *testing.T) (*cli.APIClient, *permRecordedReq, func()) {
	t.Helper()
	rec := &permRecordedReq{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.Method = r.Method
		rec.Path = r.URL.Path
		rec.Query = r.URL.RawQuery
		if r.Body != nil {
			data, _ := io.ReadAll(r.Body)
			if len(data) > 0 {
				_ = json.Unmarshal(data, &rec.Body)
			}
		}
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tools": []map[string]any{
					{
						"tool_key": "Bash", "resource_pattern": "",
						"effective": map[string]any{
							"setting": "deny", "decided_by": "agent",
							"capped_by": "agent", "reason": "Capped by agent",
						},
					},
					{
						"tool_key": "Read", "resource_pattern": "",
						"effective": map[string]any{
							"setting": "allow", "decided_by": "workspace",
							"capped_by": "", "reason": "",
						},
					},
				},
			})
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	return cli.NewAPIClient(srv.URL, permTestWorkspaceID, "test-token"), rec, srv.Close
}

func TestRunPermSet_ShapesPutRequest(t *testing.T) {
	client, rec, closeFn := permTestServer(t)
	defer closeFn()

	req := permSetRequest{
		ToolKey:         "firtal_registry",
		Layer:           "agent",
		SubjectID:       permTestAgentID,
		ResourcePattern: "ds-123",
		Setting:         "allow",
		Condition:       &permConditionRequest{Actions: []string{"get_schema"}},
	}
	if err := runPermSet(context.Background(), client, req); err != nil {
		t.Fatalf("runPermSet: %v", err)
	}
	if rec.Method != http.MethodPut {
		t.Errorf("method = %s, want PUT", rec.Method)
	}
	wantPath := "/api/workspaces/" + permTestWorkspaceID + "/tool-policy"
	if rec.Path != wantPath {
		t.Errorf("path = %s, want %s", rec.Path, wantPath)
	}
	if rec.Body["tool_key"] != "firtal_registry" || rec.Body["layer"] != "agent" || rec.Body["setting"] != "allow" {
		t.Errorf("body fields wrong: %#v", rec.Body)
	}
	cond, ok := rec.Body["condition"].(map[string]any)
	if !ok {
		t.Fatalf("condition missing in body: %#v", rec.Body)
	}
	actions, _ := cond["actions"].([]any)
	if len(actions) != 1 || actions[0] != "get_schema" {
		t.Errorf("condition.actions = %v, want [get_schema]", actions)
	}
}

func TestRunPermSet_RejectsBadLayerAndDecision(t *testing.T) {
	client, _, closeFn := permTestServer(t)
	defer closeFn()

	if err := runPermSet(context.Background(), client, permSetRequest{
		ToolKey: "Bash", Layer: "galaxy", SubjectID: permTestAgentID, Setting: "deny",
	}); err == nil {
		t.Error("expected error for invalid layer, got nil")
	}
	if err := runPermSet(context.Background(), client, permSetRequest{
		ToolKey: "Bash", Layer: "agent", SubjectID: permTestAgentID, Setting: "maybe",
	}); err == nil {
		t.Error("expected error for invalid decision, got nil")
	}
}

func TestRunPermClear_ShapesDeleteRequest(t *testing.T) {
	client, rec, closeFn := permTestServer(t)
	defer closeFn()

	if err := runPermClear(context.Background(), client, "Bash", "agent", permTestAgentID, ""); err != nil {
		t.Fatalf("runPermClear: %v", err)
	}
	if rec.Method != http.MethodDelete {
		t.Errorf("method = %s, want DELETE", rec.Method)
	}
	for _, want := range []string{"tool_key=Bash", "layer=agent", "subject_id=" + permTestAgentID} {
		if !strings.Contains(rec.Query, want) {
			t.Errorf("query %q missing %q", rec.Query, want)
		}
	}
}

func TestRunPermClear_RejectsBadLayer(t *testing.T) {
	client, _, closeFn := permTestServer(t)
	defer closeFn()
	if err := runPermClear(context.Background(), client, "Bash", "galaxy", permTestAgentID, ""); err == nil {
		t.Error("expected error for invalid layer, got nil")
	}
}

func TestRunPermExplain_QueryAndBlockedOnlyFilter(t *testing.T) {
	client, rec, closeFn := permTestServer(t)
	defer closeFn()

	err := runPermExplain(context.Background(), client, permExplainOpts{
		agentID:     permTestAgentID,
		userID:      permTestUserID,
		groupIDs:    []string{"44444444-4444-4444-4444-444444444444"},
		blockedOnly: true,
		output:      "json",
	})
	if err != nil {
		t.Fatalf("runPermExplain: %v", err)
	}
	if rec.Method != http.MethodGet {
		t.Errorf("method = %s, want GET", rec.Method)
	}
	for _, want := range []string{"agent_id=" + permTestAgentID, "user_id=" + permTestUserID, "group_id=44444444"} {
		if !strings.Contains(rec.Query, want) {
			t.Errorf("query %q missing %q", rec.Query, want)
		}
	}
}

func TestFilterPermRows(t *testing.T) {
	rows := []permRow{
		{ToolKey: "Bash", Effective: permEffective{Setting: "deny"}},
		{ToolKey: "Read", Effective: permEffective{Setting: "allow"}},
		{ToolKey: "WebFetch", Effective: permEffective{Setting: "ask"}},
	}
	if got := filterPermRows(rows, "", true); len(got) != 2 {
		t.Errorf("blocked-only: got %d rows, want 2 (deny+ask)", len(got))
	}
	if got := filterPermRows(rows, "bash", false); len(got) != 1 || got[0].ToolKey != "Bash" {
		t.Errorf("tool filter: got %#v, want [Bash]", got)
	}
	if got := filterPermRows(rows, "", false); len(got) != 3 {
		t.Errorf("no filter: got %d rows, want 3", len(got))
	}
}

func TestPermWhy_AppendsGroupAttribution(t *testing.T) {
	row := permRow{
		Effective:      permEffective{Reason: "Capped by group"},
		CappedByGroups: []permGroupAttr{{Name: "ReadOnly", Owner: "Jesper"}},
	}
	why := permWhy(row)
	if !strings.Contains(why, "ReadOnly") || !strings.Contains(why, "owner: Jesper") {
		t.Errorf("permWhy = %q, want group name + owner", why)
	}
}

func TestPermExplainCommandFlags(t *testing.T) {
	explain, _, found := permissionsCmd.Find([]string{"explain"})
	if found != nil || explain == nil {
		t.Fatalf("explain subcommand not found: %v", found)
	}
	for _, name := range []string{"agent", "user", "group", "runtime", "system", "tool", "blocked-only", "output"} {
		if explain.Flags().Lookup(name) == nil {
			t.Errorf("explain missing flag --%s", name)
		}
	}
	set, _, _ := permissionsCmd.Find([]string{"set"})
	for _, name := range []string{"tool", "layer", "subject", "decision", "resource", "when-host", "when-action", "when-expr"} {
		if set.Flags().Lookup(name) == nil {
			t.Errorf("set missing flag --%s", name)
		}
	}
}

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"
)

const (
	testViewWriteLabelUUID   = "1abe1000-0000-0000-0000-000000000001"
	testViewWriteProjectUUID = "9a0e0000-0000-0000-0000-000000000001"
	testViewWriteMemberUUID  = "3e3be000-0000-0000-0000-000000000001"
	testViewWriteAgentUUID   = "a6e17000-0000-0000-0000-000000000001"
	testViewWriteViewUUID    = "6d000000-0000-0000-0000-000000000004"
)

type viewWriteRequest struct {
	method, path string
	body         map[string]any
}

// viewWriteServer serves the catalogs view flags resolve against and one
// stored view, and records every write. view and sidebar are what the
// server returns for the stored view and the sidebar preference.
type viewWriteServer struct {
	mu          sync.Mutex
	writes      []viewWriteRequest
	view        map[string]any
	sidebar     map[string]any
	writeStatus int
}

func (s *viewWriteServer) recorded(method string) []viewWriteRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []viewWriteRequest
	for _, w := range s.writes {
		if w.method == method {
			out = append(out, w)
		}
	}
	return out
}

func newViewWriteServer(t *testing.T) *viewWriteServer {
	t.Helper()
	s := &viewWriteServer{
		view: map[string]any{
			"id": testViewWriteViewUUID, "name": "Bugs", "scope_type": "workspace", "scope_id": nil,
			"scope_variant": nil, "visibility": "private", "owner_id": testViewWriteMemberUUID,
			"definition_version": 1, "revision": 3,
			"query": map[string]any{
				"statusFilters": []any{"todo"}, "labelFilters": []any{testViewWriteLabelUUID},
				"futureKey": "kept",
			},
			"display": map[string]any{"viewMode": "board", "cardProperties": map[string]any{"labels": true}},
		},
		sidebar: map[string]any{
			"scope_type": "sidebar",
			"prefs":      map[string]any{"order": []any{"builtin:my_issues"}, "hidden": []any{}},
			"updated_at": "2026-11-01T00:00:00Z",
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		write := func(v any) { _ = json.NewEncoder(w).Encode(v) }
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/issue-statuses":
			write(map[string]any{"statuses": []any{
				map[string]any{"id": "s1", "key": "todo", "name": "Todo"},
				map[string]any{"id": "s2", "key": "in_review", "name": "In Review"},
				map[string]any{"id": "s3", "key": "qa", "name": "Waiting for QA"},
			}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/labels":
			write(map[string]any{"labels": []any{map[string]any{"id": testViewWriteLabelUUID, "name": "Bug"}}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/projects":
			write(map[string]any{"projects": []any{map[string]any{"id": testViewWriteProjectUUID, "title": "Website"}}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/workspaces/ws-1/members":
			write([]any{map[string]any{"user_id": testViewWriteMemberUUID, "name": "Bohan", "email": "bohan@example.com"}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/agents":
			write([]any{map[string]any{"id": testViewWriteAgentUUID, "name": "Lambda"}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/squads":
			write([]any{})
		case r.Method == http.MethodGet && r.URL.Path == "/api/properties":
			write(map[string]any{"properties": []any{
				map[string]any{"id": "p-sev", "name": "Severity", "type": "select", "config": map[string]any{
					"options": []any{map[string]any{"id": "opt-high", "name": "High"}},
				}},
				map[string]any{"id": "p-notes", "name": "Notes", "type": "text"},
			}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/issue-views" && r.URL.Query().Get("scope_type") == "all":
			write([]any{s.view})
		case r.Method == http.MethodGet && r.URL.Path == "/api/issue-views/"+testViewWriteViewUUID:
			write(s.view)
		case r.Method == http.MethodGet && r.URL.Path == "/api/issue-view-preferences":
			write(s.sidebar)
		case r.Method == http.MethodPost || r.Method == http.MethodPatch || r.Method == http.MethodPut || r.Method == http.MethodDelete:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			s.mu.Lock()
			s.writes = append(s.writes, viewWriteRequest{method: r.Method, path: r.URL.Path, body: body})
			s.mu.Unlock()
			if s.writeStatus != 0 && r.URL.Path != "/api/issue-view-preferences" {
				w.WriteHeader(s.writeStatus)
				write(map[string]any{"error": "refused"})
				return
			}
			switch r.Method {
			case http.MethodPost:
				w.WriteHeader(http.StatusCreated)
				created := map[string]any{"id": "6e000000-0000-0000-0000-000000000005", "revision": 1}
				for k, v := range body {
					created[k] = v
				}
				write(created)
			case http.MethodDelete:
				w.WriteHeader(http.StatusNoContent)
			default:
				write(body)
			}
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.String())
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	setCLITestServerEnv(t, srv.URL)
	return s
}

func newViewWriteTestCmd(t *testing.T, use string, flags map[string][]string) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: use}
	switch use {
	case "create":
		cmd.Flags().String("name", "", "")
		cmd.Flags().String("scope", "", "")
		cmd.Flags().String("project", "", "")
		cmd.Flags().String("scope-variant", "", "")
		cmd.Flags().String("visibility", "private", "")
		addViewDefinitionFlags(cmd, false)
	case "update":
		cmd.Flags().String("name", "", "")
		cmd.Flags().String("scope-variant", "", "")
		cmd.Flags().String("visibility", "", "")
		addViewDefinitionFlags(cmd, true)
	case "duplicate":
		cmd.Flags().String("name", "", "")
		cmd.Flags().String("visibility", "private", "")
		addViewDefinitionFlags(cmd, true)
	}
	cmd.Flags().String("output", "json", "")
	for name, values := range flags {
		for _, value := range values {
			if err := cmd.Flags().Set(name, value); err != nil {
				t.Fatalf("set --%s=%s: %v", name, value, err)
			}
		}
	}
	return cmd
}

func TestViewCreateResolvesNamesAndStarsTheView(t *testing.T) {
	srv := newViewWriteServer(t)
	cmd := newViewWriteTestCmd(t, "create", map[string][]string{
		"name":            {"Open bugs"},
		"project":         {"website"},
		"status":          {"Todo", "in_review,qa"},
		"negate":          {"status"},
		"label":           {"bug"},
		"assignee":        {"Bohan", "Lambda"},
		"priority":        {"urgent"},
		"property":        {"Severity=High"},
		"view-mode":       {"table"},
		"sort":            {"title"},
		"agent-running":   {"true"},
		"show-sub-issues": {"false"},
	})
	if _, err := captureStdout(t, func() error { return runViewCreate(cmd, nil) }); err != nil {
		t.Fatalf("run: %v", err)
	}
	posts := srv.recorded(http.MethodPost)
	if len(posts) != 1 || posts[0].path != "/api/issue-views" {
		t.Fatalf("posts = %+v", posts)
	}
	body := posts[0].body
	if body["name"] != "Open bugs" || body["scope_type"] != "project" || body["scope_id"] != testViewWriteProjectUUID ||
		body["scope_variant"] != "all" || body["visibility"] != "private" || body["definition_version"] != float64(1) {
		t.Fatalf("view fields = %v", body)
	}
	wantQuery := map[string]any{
		"statusFilters":      []any{"todo", "in_review", "qa"},
		"negatedFilters":     []any{"status"},
		"labelFilters":       []any{testViewWriteLabelUUID},
		"priorityFilters":    []any{"urgent"},
		"propertyFilters":    map[string]any{"p-sev": []any{"opt-high"}},
		"agentRunningFilter": true,
		"agentIdleFilter":    false,
		"assigneeFilters": []any{
			map[string]any{"type": "member", "id": testViewWriteMemberUUID},
			map[string]any{"type": "agent", "id": testViewWriteAgentUUID},
		},
	}
	if !reflect.DeepEqual(body["query"], wantQuery) {
		t.Fatalf("query = %v\nwant    %v", body["query"], wantQuery)
	}
	wantDisplay := map[string]any{"viewMode": "table", "sortBy": "title", "sortDirection": "asc", "showSubIssues": false}
	if !reflect.DeepEqual(body["display"], wantDisplay) {
		t.Fatalf("display = %v\nwant      %v", body["display"], wantDisplay)
	}
	puts := srv.recorded(http.MethodPut)
	if len(puts) != 1 {
		t.Fatalf("sidebar writes = %d, want 1", len(puts))
	}
	prefs, _ := puts[0].body["prefs"].(map[string]any)
	if order, _ := prefs["order"].([]any); !reflect.DeepEqual(order, []any{"builtin:my_issues", "view:6e000000-0000-0000-0000-000000000005"}) {
		t.Fatalf("sidebar order = %v", prefs["order"])
	}
}

func TestViewCreateLeavesAnUnarrangedSidebarAlone(t *testing.T) {
	srv := newViewWriteServer(t)
	srv.sidebar = map[string]any{"scope_type": "sidebar", "prefs": map[string]any{}, "updated_at": ""}
	cmd := newViewWriteTestCmd(t, "create", map[string][]string{"name": {"Mine"}, "scope": {"my"}})
	if _, err := captureStdout(t, func() error { return runViewCreate(cmd, nil) }); err != nil {
		t.Fatalf("run: %v", err)
	}
	posts := srv.recorded(http.MethodPost)
	if len(posts) != 1 || posts[0].body["scope_variant"] != "any" || posts[0].body["visibility"] != "private" {
		t.Fatalf("posts = %+v", posts)
	}
	if puts := srv.recorded(http.MethodPut); len(puts) != 0 {
		t.Fatalf("an unarranged sidebar lists the owner's views already; got writes %+v", puts)
	}
}

func TestViewCreateRejectsBadInputBeforeWriting(t *testing.T) {
	for name, flags := range map[string]map[string][]string{
		"missing name":              {},
		"bad priority":              {"name": {"x"}, "priority": {"highest"}},
		"unknown status":            {"name": {"x"}, "status": {"nope"}},
		"unknown label":             {"name": {"x"}, "label": {"Feature"}},
		"project filter on project": {"name": {"x"}, "project": {"Website"}, "project-filter": {"Website"}},
		"relation on my view":       {"name": {"x"}, "scope": {"my"}, "relation": {"assigned"}},
		"shared my view":            {"name": {"x"}, "scope": {"my"}, "visibility": {"workspace"}},
		"bad my variant":            {"name": {"x"}, "scope": {"my"}, "scope-variant": {"agents"}},
		"manual order off status":   {"name": {"x"}, "grouping": {"assignee"}, "sort": {"position"}},
		"group by text property":    {"name": {"x"}, "grouping": {"property:Notes"}},
		"running and idle":          {"name": {"x"}, "agent-running": {"true"}, "agent-idle": {"true"}},
		"bad negate":                {"name": {"x"}, "negate": {"title"}},
	} {
		t.Run(name, func(t *testing.T) {
			srv := newViewWriteServer(t)
			if err := runViewCreate(newViewWriteTestCmd(t, "create", flags), nil); err == nil {
				t.Fatal("want an error")
			}
			if posts := srv.recorded(http.MethodPost); len(posts) != 0 {
				t.Fatalf("wrote despite the error: %+v", posts)
			}
		})
	}
}

func TestViewCreateDryRunPrintsWithoutWriting(t *testing.T) {
	srv := newViewWriteServer(t)
	cmd := newViewWriteTestCmd(t, "create", map[string][]string{"name": {"Preview"}, "status": {"todo"}, "dry-run": {"true"}})
	out, err := captureStdout(t, func() error { return runViewCreate(cmd, nil) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(out), &body); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}
	if query, _ := body["query"].(map[string]any); !reflect.DeepEqual(query["statusFilters"], []any{"todo"}) {
		t.Fatalf("dry-run body = %v", body)
	}
	if len(srv.recorded(http.MethodPost))+len(srv.recorded(http.MethodPut)) != 0 {
		t.Fatal("dry run wrote")
	}
}

func TestViewUpdateChangesOnlyWhatFlagsName(t *testing.T) {
	srv := newViewWriteServer(t)
	cmd := newViewWriteTestCmd(t, "update", map[string][]string{
		"status":     {""},
		"priority":   {"high"},
		"grouping":   {"assignee"},
		"visibility": {"workspace"},
	})
	if _, err := captureStdout(t, func() error { return runViewUpdate(cmd, []string{"bugs"}) }); err != nil {
		t.Fatalf("run: %v", err)
	}
	patches := srv.recorded(http.MethodPatch)
	if len(patches) != 1 || patches[0].path != "/api/issue-views/"+testViewWriteViewUUID {
		t.Fatalf("patches = %+v", patches)
	}
	body := patches[0].body
	if body["expected_revision"] != float64(3) || body["definition_version"] != float64(1) || body["visibility"] != "workspace" {
		t.Fatalf("patch fields = %v", body)
	}
	if _, ok := body["name"]; ok {
		t.Fatalf("name sent without --name: %v", body)
	}
	wantQuery := map[string]any{
		"statusFilters":   []any{},
		"priorityFilters": []any{"high"},
		"labelFilters":    []any{testViewWriteLabelUUID},
		"futureKey":       "kept",
	}
	if !reflect.DeepEqual(body["query"], wantQuery) {
		t.Fatalf("query = %v\nwant    %v", body["query"], wantQuery)
	}
	wantDisplay := map[string]any{"viewMode": "board", "grouping": "assignee", "cardProperties": map[string]any{"labels": true}}
	if !reflect.DeepEqual(body["display"], wantDisplay) {
		t.Fatalf("display = %v\nwant      %v", body["display"], wantDisplay)
	}
}

func TestViewUpdateWithoutDefinitionFlagsLeavesTheDefinitionOut(t *testing.T) {
	srv := newViewWriteServer(t)
	cmd := newViewWriteTestCmd(t, "update", map[string][]string{"name": {"Renamed"}})
	if _, err := captureStdout(t, func() error { return runViewUpdate(cmd, []string{"Bugs"}) }); err != nil {
		t.Fatalf("run: %v", err)
	}
	body := srv.recorded(http.MethodPatch)[0].body
	want := map[string]any{"name": "Renamed", "expected_revision": float64(3)}
	if !reflect.DeepEqual(body, want) {
		t.Fatalf("patch = %v, want %v", body, want)
	}
}

func TestViewUpdateAppliesTheDefinitionFileThenFlags(t *testing.T) {
	srv := newViewWriteServer(t)
	path := filepath.Join(t.TempDir(), "view.json")
	if err := os.WriteFile(path, []byte(`{"id":"ignored","definition_version":1,"query":{"unreadFilter":true},"display":{"viewMode":"list"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := newViewWriteTestCmd(t, "update", map[string][]string{"definition-file": {path}, "view-mode": {"gantt"}})
	if _, err := captureStdout(t, func() error { return runViewUpdate(cmd, []string{"Bugs"}) }); err != nil {
		t.Fatalf("run: %v", err)
	}
	body := srv.recorded(http.MethodPatch)[0].body
	if !reflect.DeepEqual(body["query"], map[string]any{"unreadFilter": true}) ||
		!reflect.DeepEqual(body["display"], map[string]any{"viewMode": "gantt"}) {
		t.Fatalf("patch = %v", body)
	}
}

func writeViewDefinitionFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "view.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// An export edited after someone else saved the view must not overwrite that
// save: the export's revision is the write's precondition.
func TestViewUpdateRefusesAStaleExportedDefinition(t *testing.T) {
	srv := newViewWriteServer(t)
	path := writeViewDefinitionFile(t, `{"id":"`+testViewWriteViewUUID+`","revision":2,"definition_version":1,"query":{"statusFilters":["todo"]},"display":{"viewMode":"list"}}`)
	err := runViewUpdate(newViewWriteTestCmd(t, "update", map[string][]string{"definition-file": {path}}), []string{"Bugs"})
	if err == nil || !strings.Contains(err.Error(), "revision 2") || !strings.Contains(err.Error(), "revision 3") {
		t.Fatalf("err = %v", err)
	}
	if patches := srv.recorded(http.MethodPatch); len(patches) != 0 {
		t.Fatalf("a stale export was written: %+v", patches)
	}
}

func TestViewDefinitionFileRevisionOnlyBindsItsOwnView(t *testing.T) {
	cases := map[string]struct {
		run          func(cmd *cobra.Command) error
		use, content string
		method       string
	}{
		"current export of this view": {
			use: "update", method: http.MethodPatch,
			content: `{"id":"` + testViewWriteViewUUID + `","revision":3,"display":{"viewMode":"list"}}`,
		},
		"template without a revision": {
			use: "update", method: http.MethodPatch,
			content: `{"display":{"viewMode":"list"}}`,
		},
		"export of another view": {
			use: "update", method: http.MethodPatch,
			content: `{"id":"6f000000-0000-0000-0000-000000000009","revision":1,"display":{"viewMode":"list"}}`,
		},
		"export used to create a new view": {
			use: "create", method: http.MethodPost,
			content: `{"id":"` + testViewWriteViewUUID + `","revision":1,"display":{"viewMode":"list"}}`,
		},
		"export used to duplicate": {
			use: "duplicate", method: http.MethodPost,
			content: `{"id":"` + testViewWriteViewUUID + `","revision":1,"display":{"viewMode":"list"}}`,
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			srv := newViewWriteServer(t)
			flags := map[string][]string{"definition-file": {writeViewDefinitionFile(t, c.content)}}
			if c.use == "create" {
				flags["name"] = []string{"From export"}
			}
			cmd := newViewWriteTestCmd(t, c.use, flags)
			_, err := captureStdout(t, func() error {
				switch c.use {
				case "create":
					return runViewCreate(cmd, nil)
				case "duplicate":
					return runViewDuplicate(cmd, []string{"Bugs"})
				default:
					return runViewUpdate(cmd, []string{"Bugs"})
				}
			})
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			writes := srv.recorded(c.method)
			if len(writes) != 1 || !reflect.DeepEqual(writes[0].body["display"], map[string]any{"viewMode": "list"}) {
				t.Fatalf("writes = %+v", writes)
			}
			if c.method == http.MethodPatch && writes[0].body["expected_revision"] != float64(3) {
				t.Fatalf("expected_revision = %v, want 3", writes[0].body["expected_revision"])
			}
		})
	}
}

func TestViewUpdateRefusesWhatItCannotRepresent(t *testing.T) {
	t.Run("advanced tree with flat filters", func(t *testing.T) {
		srv := newViewWriteServer(t)
		srv.view["query"] = map[string]any{"advancedFilter": map[string]any{"type": "group", "match": "all", "children": []any{}}}
		err := runViewUpdate(newViewWriteTestCmd(t, "update", map[string][]string{"status": {"todo"}}), []string{"Bugs"})
		if err == nil || !strings.Contains(err.Error(), "--clear-filters") {
			t.Fatalf("err = %v", err)
		}
		if len(srv.recorded(http.MethodPatch)) != 0 {
			t.Fatal("wrote despite the error")
		}
	})
	t.Run("advanced tree replaced by clear-filters", func(t *testing.T) {
		srv := newViewWriteServer(t)
		srv.view["query"] = map[string]any{"advancedFilter": map[string]any{"type": "group", "match": "all", "children": []any{}}}
		cmd := newViewWriteTestCmd(t, "update", map[string][]string{"status": {"todo"}, "clear-filters": {"true"}})
		if _, err := captureStdout(t, func() error { return runViewUpdate(cmd, []string{"Bugs"}) }); err != nil {
			t.Fatalf("run: %v", err)
		}
		if query := srv.recorded(http.MethodPatch)[0].body["query"]; !reflect.DeepEqual(query, map[string]any{"statusFilters": []any{"todo"}}) {
			t.Fatalf("query = %v", query)
		}
	})
	t.Run("newer definition version", func(t *testing.T) {
		srv := newViewWriteServer(t)
		srv.view["definition_version"] = 2
		err := runViewUpdate(newViewWriteTestCmd(t, "update", map[string][]string{"priority": {"high"}}), []string{"Bugs"})
		if err == nil || !strings.Contains(err.Error(), "definition_version") {
			t.Fatalf("err = %v", err)
		}
		if len(srv.recorded(http.MethodPatch)) != 0 {
			t.Fatal("wrote despite the error")
		}
	})
	t.Run("no flags", func(t *testing.T) {
		newViewWriteServer(t)
		if err := runViewUpdate(newViewWriteTestCmd(t, "update", nil), []string{"Bugs"}); err == nil {
			t.Fatal("want an error")
		}
	})
}

func TestViewWritesReportConflictsAndPermissionRefusals(t *testing.T) {
	srv := newViewWriteServer(t)
	srv.writeStatus = http.StatusConflict
	err := runViewUpdate(newViewWriteTestCmd(t, "update", map[string][]string{"name": {"New"}}), []string{"Bugs"})
	if err == nil || !strings.Contains(err.Error(), "re-run") || !strings.Contains(err.Error(), "revision 3") {
		t.Fatalf("conflict err = %v", err)
	}

	srv.writeStatus = http.StatusForbidden
	err = runViewUpdate(newViewWriteTestCmd(t, "update", map[string][]string{"name": {"New"}}), []string{"Bugs"})
	if err == nil || !strings.Contains(err.Error(), "owner") {
		t.Fatalf("update forbidden err = %v", err)
	}
	err = runViewDelete(newViewWriteTestCmd(t, "delete", nil), []string{"Bugs"})
	if err == nil || !strings.Contains(err.Error(), "owner") {
		t.Fatalf("delete forbidden err = %v", err)
	}
}

func TestViewDuplicateCopiesTheSourceIntoANewPrivateView(t *testing.T) {
	srv := newViewWriteServer(t)
	srv.view["visibility"] = "workspace"
	cmd := newViewWriteTestCmd(t, "duplicate", map[string][]string{"priority": {"urgent"}})
	if _, err := captureStdout(t, func() error { return runViewDuplicate(cmd, []string{"Bugs"}) }); err != nil {
		t.Fatalf("run: %v", err)
	}
	body := srv.recorded(http.MethodPost)[0].body
	if body["name"] != "Bugs (copy)" || body["visibility"] != "private" || body["scope_type"] != "workspace" {
		t.Fatalf("copy fields = %v", body)
	}
	query, _ := body["query"].(map[string]any)
	if !reflect.DeepEqual(query["statusFilters"], []any{"todo"}) || !reflect.DeepEqual(query["priorityFilters"], []any{"urgent"}) {
		t.Fatalf("copy query = %v", query)
	}
	if len(srv.recorded(http.MethodPatch)) != 0 {
		t.Fatal("the source view was written")
	}
}

func TestViewDeleteSendsTheResolvedID(t *testing.T) {
	srv := newViewWriteServer(t)
	out, err := captureStdout(t, func() error { return runViewDelete(newViewWriteTestCmd(t, "delete", nil), []string{"bugs"}) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	deletes := srv.recorded(http.MethodDelete)
	if len(deletes) != 1 || deletes[0].path != "/api/issue-views/"+testViewWriteViewUUID {
		t.Fatalf("deletes = %+v", deletes)
	}
	if !strings.Contains(out, `"deleted": true`) {
		t.Fatalf("output = %s", out)
	}
}

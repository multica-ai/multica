package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

type quickActionTestServer struct {
	actions    []quickActionDTO
	agents     []map[string]any
	squads     []map[string]any
	issueByKey map[string]string

	lastIncludeArchived bool
	posts               []map[string]any
	patches             []map[string]any
	patchIDs            []string
	deletes             []string
	runPaths            []string
	renderPaths         []string
}

const (
	qaTestIDMerge   = "11111111-1111-4111-8111-111111111111"
	qaTestIDReview  = "22222222-2222-4222-8222-222222222222"
	qaTestIDOld     = "33333333-3333-4333-8333-333333333333"
	qaTestIDAgent   = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	qaTestIDSquad   = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	qaTestIDIssue   = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
)

func newQuickActionTestServer(t *testing.T) *quickActionTestServer {
	t.Helper()
	s := &quickActionTestServer{
		actions: []quickActionDTO{
			{
				ID:           qaTestIDMerge,
				Name:         "Merge",
				Description:  "Approve and merge",
				AssigneeType: "squad",
				AssigneeID:   qaTestIDSquad,
				TargetName:   "JSquad",
				Prompt:       "merge",
				Visibility:   "private",
				Status:       "active",
				UseCount:     4,
			},
			{
				ID:           qaTestIDReview,
				Name:         "Review",
				AssigneeType: "agent",
				AssigneeID:   qaTestIDAgent,
				TargetName:   "Lambda",
				TargetPublic: true,
				Prompt:       "review this PR",
				Visibility:   "public",
				Status:       "active",
				UseCount:     12,
			},
			{
				ID:         qaTestIDOld,
				Name:       "Merge",
				Prompt:     "old merge prompt",
				Visibility: "private",
				Status:     "archived",
			},
		},
		agents: []map[string]any{
			{"id": qaTestIDAgent, "name": "Lambda"},
		},
		squads: []map[string]any{
			{"id": qaTestIDSquad, "name": "JSquad"},
		},
		issueByKey: map[string]string{
			"MUL-42": qaTestIDIssue,
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/quick-actions":
			includeArchived := r.URL.Query().Get("include_archived") == "true"
			s.lastIncludeArchived = includeArchived
			out := make([]quickActionDTO, 0, len(s.actions))
			for _, a := range s.actions {
				if a.Status == "archived" && !includeArchived {
					continue
				}
				out = append(out, a)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"quick_actions": out})

		case r.Method == http.MethodPost && r.URL.Path == "/api/quick-actions":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			s.posts = append(s.posts, body)
			created := quickActionDTO{
				ID:           "44444444-4444-4444-8444-444444444444",
				Name:         strVal(body, "name"),
				Description:  strVal(body, "description"),
				AssigneeType: strVal(body, "assignee_type"),
				AssigneeID:   strVal(body, "assignee_id"),
				Prompt:       strVal(body, "prompt"),
				Visibility:   strVal(body, "visibility"),
				Status:       "active",
			}
			if created.AssigneeType == "squad" && created.AssigneeID == qaTestIDSquad {
				created.TargetName = "JSquad"
			}
			if created.AssigneeType == "agent" && created.AssigneeID == qaTestIDAgent {
				created.TargetName = "Lambda"
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(created)

		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/quick-actions/"):
			id := strings.TrimPrefix(r.URL.Path, "/api/quick-actions/")
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			s.patches = append(s.patches, body)
			s.patchIDs = append(s.patchIDs, id)
			updated := quickActionDTO{ID: id}
			for _, a := range s.actions {
				if a.ID == id {
					updated = a
					break
				}
			}
			if v, ok := body["name"].(string); ok {
				updated.Name = v
			}
			if v, ok := body["description"].(string); ok {
				updated.Description = v
			}
			if v, ok := body["prompt"].(string); ok {
				updated.Prompt = v
			}
			if v, ok := body["visibility"].(string); ok {
				updated.Visibility = v
			}
			if v, ok := body["status"].(string); ok {
				updated.Status = v
			}
			if v, ok := body["assignee_type"].(string); ok {
				updated.AssigneeType = v
				if v == "" {
					updated.TargetName = ""
				}
			}
			if v, ok := body["assignee_id"].(string); ok {
				updated.AssigneeID = v
			}
			_ = json.NewEncoder(w).Encode(updated)

		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/quick-actions/"):
			id := strings.TrimPrefix(r.URL.Path, "/api/quick-actions/")
			s.deletes = append(s.deletes, id)
			w.WriteHeader(http.StatusNoContent)

		case r.Method == http.MethodGet && r.URL.Path == "/api/agents":
			_ = json.NewEncoder(w).Encode(s.agents)

		case r.Method == http.MethodGet && r.URL.Path == "/api/squads":
			_ = json.NewEncoder(w).Encode(s.squads)

		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/issues/"):
			ref := strings.TrimPrefix(r.URL.Path, "/api/issues/")
			if id, ok := s.issueByKey[ref]; ok {
				_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "identifier": ref})
				return
			}
			http.NotFound(w, r)

		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/render"):
			s.renderPaths = append(s.renderPaths, r.URL.Path)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"content": "[@JSquad](mention://squad/" + qaTestIDSquad + ")\n\nmerge",
			})

		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/run"):
			s.runPaths = append(s.runPaths, r.URL.Path)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "comment-1",
				"trigger_outcomes": []any{
					map[string]any{"target_name": "Xeon", "status": "queued"},
				},
			})

		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "mat_test-token")
	return s
}

func quickActionTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "quick-action"}
	cmd.Flags().String("output", "table", "")
	cmd.Flags().Bool("include-archived", false, "")
	cmd.Flags().Bool("full-id", false, "")
	cmd.Flags().String("name", "", "")
	cmd.Flags().String("description", "", "")
	cmd.Flags().String("prompt", "", "")
	cmd.Flags().String("prompt-file", "", "")
	cmd.Flags().Bool("prompt-stdin", false, "")
	cmd.Flags().Bool("allow-external-file", false, "")
	cmd.Flags().String("agent", "", "")
	cmd.Flags().String("squad", "", "")
	cmd.Flags().Bool("clear-target", false, "")
	cmd.Flags().String("visibility", "private", "")
	cmd.Flags().String("status", "", "")
	cmd.Flags().Bool("dry-run", false, "")
	return cmd
}

func TestResolveQuickActionRef(t *testing.T) {
	active := quickActionDTO{ID: qaTestIDMerge, Name: "Merge", Status: "active"}
	archived := quickActionDTO{ID: qaTestIDOld, Name: "Merge", Status: "archived"}
	otherActive := quickActionDTO{ID: "11111111-9999-4999-8999-999999999999", Name: "MERGE", Status: "active"}

	for _, tc := range []struct {
		name    string
		actions []quickActionDTO
		ref     string
		wantID  string
		wantErr string
	}{
		{name: "exact UUID", actions: []quickActionDTO{active, archived}, ref: qaTestIDOld, wantID: qaTestIDOld},
		{name: "active name preferred over archived", actions: []quickActionDTO{archived, active}, ref: "merge", wantID: qaTestIDMerge},
		{name: "single archived name", actions: []quickActionDTO{archived}, ref: "Merge", wantID: qaTestIDOld},
		{name: "ambiguous active names", actions: []quickActionDTO{active, otherActive}, ref: "Merge", wantErr: "more than one quick action"},
		{name: "unique UUID prefix", actions: []quickActionDTO{active, archived}, ref: "33333333", wantID: qaTestIDOld},
		{name: "ambiguous UUID prefix", actions: []quickActionDTO{active, otherActive}, ref: "11111111", wantErr: "ambiguous"},
		{name: "not found", actions: []quickActionDTO{active}, ref: "Deploy", wantErr: "not found"},
		{name: "empty ref", actions: []quickActionDTO{active}, ref: "   ", wantErr: "required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveQuickActionRef(tc.actions, tc.ref)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("resolve(%q) err = %v, want substring %q", tc.ref, err, tc.wantErr)
				}
				return
			}
			if err != nil || got.ID != tc.wantID {
				t.Fatalf("resolve(%q) = %q, %v; want %q", tc.ref, got.ID, err, tc.wantID)
			}
		})
	}
}

func TestQuickActionListAndGet(t *testing.T) {
	api := newQuickActionTestServer(t)

	cmd := quickActionTestCmd()
	_ = cmd.Flags().Set("output", "json")
	out, err := captureStdout(t, func() error { return runQuickActionList(cmd, nil) })
	if err != nil {
		t.Fatalf("runQuickActionList: %v", err)
	}
	if api.lastIncludeArchived {
		t.Fatal("default list must not pass include_archived=true")
	}
	var got []quickActionDTO
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal list: %v\n%s", err, out)
	}
	if len(got) != 2 {
		t.Fatalf("active list len = %d, want 2", len(got))
	}

	// Table output renders squad/agent/none/missing targets clearly.
	api.actions = append(api.actions,
		quickActionDTO{ID: "55555555-5555-4555-8555-555555555555", Name: "Note", Visibility: "private", Status: "active"},
		quickActionDTO{ID: "66666666-6666-4666-8666-666666666666", Name: "Orphan", AssigneeType: "agent", AssigneeID: qaTestIDAgent, TargetMissing: true, Visibility: "private", Status: "active"},
	)
	tableCmd := quickActionTestCmd()
	_ = tableCmd.Flags().Set("include-archived", "true")
	tableOut, err := captureStdout(t, func() error { return runQuickActionList(tableCmd, nil) })
	if err != nil {
		t.Fatalf("runQuickActionList table: %v", err)
	}
	for _, want := range []string{"JSquad (squad)", "Lambda (agent)", "(missing)", "archived"} {
		if !strings.Contains(tableOut, want) {
			t.Errorf("table output missing %q:\n%s", want, tableOut)
		}
	}

	// Get resolves active name over archived and prints detail in table mode.
	getCmd := quickActionTestCmd()
	_ = getCmd.Flags().Set("output", "table")
	getOut, err := captureStdout(t, func() error { return runQuickActionGet(getCmd, []string{"Merge"}) })
	if err != nil {
		t.Fatalf("runQuickActionGet: %v", err)
	}
	if !strings.Contains(getOut, qaTestIDMerge) || !strings.Contains(getOut, "JSquad (squad)") {
		t.Errorf("get detail output unexpected:\n%s", getOut)
	}
}

func TestQuickActionCreateSquadAgentTargetlessAndPromptFile(t *testing.T) {
	api := newQuickActionTestServer(t)

	// Squad target with default private visibility.
	cmd := quickActionTestCmd()
	_ = cmd.Flags().Set("output", "json")
	_ = cmd.Flags().Set("name", "Handoff")
	_ = cmd.Flags().Set("squad", "JSquad")
	_ = cmd.Flags().Set("prompt", "Take over.")
	out, err := captureStdout(t, func() error { return runQuickActionCreate(cmd, nil) })
	if err != nil {
		t.Fatalf("create squad quick action: %v", err)
	}
	if len(api.posts) != 1 {
		t.Fatalf("posts = %d, want 1", len(api.posts))
	}
	p := api.posts[0]
	if p["assignee_type"] != "squad" || p["assignee_id"] != qaTestIDSquad || p["visibility"] != "private" {
		t.Fatalf("unexpected squad post body: %#v", p)
	}
	if !strings.Contains(out, `"target_name": "JSquad"`) {
		t.Errorf("unexpected JSON output: %s", out)
	}

	// Targetless action reading prompt from --prompt-file inside workdir.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	promptPath := filepath.Join(wd, "tmp-quick-action-prompt-test.txt")
	if err := os.WriteFile(promptPath, []byte("merge and deploy dev\n"), 0o600); err != nil {
		t.Fatalf("write prompt file: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(promptPath) })

	cmd2 := quickActionTestCmd()
	_ = cmd2.Flags().Set("output", "json")
	_ = cmd2.Flags().Set("name", "Merge Dev")
	_ = cmd2.Flags().Set("prompt-file", promptPath)
	_ = cmd2.Flags().Set("visibility", "public")
	if _, err := captureStdout(t, func() error { return runQuickActionCreate(cmd2, nil) }); err != nil {
		t.Fatalf("create targetless quick action: %v", err)
	}
	p2 := api.posts[1]
	if _, hasType := p2["assignee_type"]; hasType {
		t.Fatalf("targetless create must omit assignee_type, got %#v", p2)
	}
	if p2["prompt"] != "merge and deploy dev" || p2["visibility"] != "public" {
		t.Fatalf("unexpected targetless post body: %#v", p2)
	}

	// Validation errors.
	bad := quickActionTestCmd()
	_ = bad.Flags().Set("name", "Both")
	_ = bad.Flags().Set("prompt", "hi")
	_ = bad.Flags().Set("agent", "Lambda")
	_ = bad.Flags().Set("squad", "JSquad")
	if err := runQuickActionCreate(bad, nil); err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("expected mutually exclusive error, got %v", err)
	}
}

func TestQuickActionUpdateClearTargetArchiveRestoreDelete(t *testing.T) {
	api := newQuickActionTestServer(t)

	// Clear target sends both empty assignee_type and empty assignee_id.
	upd := quickActionTestCmd()
	_ = upd.Flags().Set("output", "json")
	_ = upd.Flags().Set("clear-target", "true")
	_ = upd.Flags().Set("description", "")
	if _, err := captureStdout(t, func() error { return runQuickActionUpdate(upd, []string{"Merge"}) }); err != nil {
		t.Fatalf("update clear-target: %v", err)
	}
	if len(api.patchIDs) != 1 || api.patchIDs[0] != qaTestIDMerge {
		t.Fatalf("patchIDs = %v, want [%s]", api.patchIDs, qaTestIDMerge)
	}
	body := api.patches[0]
	if body["assignee_type"] != "" || body["assignee_id"] != "" || body["description"] != "" {
		t.Fatalf("unexpected clear-target patch body: %#v", body)
	}

	// Reject update with no fields.
	emptyUpd := quickActionTestCmd()
	if err := runQuickActionUpdate(emptyUpd, []string{"Merge"}); err == nil || !strings.Contains(err.Error(), "no fields to update") {
		t.Fatalf("expected no fields error, got %v", err)
	}

	// Archive and restore.
	archCmd := quickActionTestCmd()
	if _, err := captureStdout(t, func() error { return makeQuickActionStatusRun("archived")(archCmd, []string{"Review"}) }); err != nil {
		t.Fatalf("archive: %v", err)
	}
	if api.patchIDs[1] != qaTestIDReview || api.patches[1]["status"] != "archived" {
		t.Fatalf("unexpected archive patch: id=%s body=%#v", api.patchIDs[1], api.patches[1])
	}

	restCmd := quickActionTestCmd()
	if _, err := captureStdout(t, func() error { return makeQuickActionStatusRun("active")(restCmd, []string{qaTestIDOld}) }); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if api.patchIDs[2] != qaTestIDOld || api.patches[2]["status"] != "active" {
		t.Fatalf("unexpected restore patch: id=%s body=%#v", api.patchIDs[2], api.patches[2])
	}

	// Delete.
	delCmd := quickActionTestCmd()
	_ = delCmd.Flags().Set("output", "json")
	delOut, err := captureStdout(t, func() error { return runQuickActionDelete(delCmd, []string{qaTestIDOld}) })
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(api.deletes) != 1 || api.deletes[0] != qaTestIDOld {
		t.Fatalf("deletes = %v, want [%s]", api.deletes, qaTestIDOld)
	}
	if !strings.Contains(delOut, `"deleted": true`) {
		t.Errorf("unexpected delete JSON: %s", delOut)
	}
}

func TestQuickActionRunAndDryRun(t *testing.T) {
	api := newQuickActionTestServer(t)

	dryCmd := quickActionTestCmd()
	_ = dryCmd.Flags().Set("dry-run", "true")
	dryOut, err := captureStdout(t, func() error { return runQuickActionRun(dryCmd, []string{"Merge", "MUL-42"}) })
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	wantRender := "/api/issues/" + qaTestIDIssue + "/quick-actions/" + qaTestIDMerge + "/render"
	if len(api.renderPaths) != 1 || api.renderPaths[0] != wantRender {
		t.Fatalf("renderPaths = %v, want [%s]", api.renderPaths, wantRender)
	}
	if !strings.Contains(dryOut, "mention://squad/"+qaTestIDSquad) {
		t.Errorf("dry-run output unexpected:\n%s", dryOut)
	}

	runCmd := quickActionTestCmd()
	runOut, err := captureStdout(t, func() error { return runQuickActionRun(runCmd, []string{"Merge", "MUL-42"}) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	wantRun := "/api/issues/" + qaTestIDIssue + "/quick-actions/" + qaTestIDMerge + "/run"
	if len(api.runPaths) != 1 || api.runPaths[0] != wantRun {
		t.Fatalf("runPaths = %v, want [%s]", api.runPaths, wantRun)
	}
	if !strings.Contains(runOut, "comment-1") || !strings.Contains(runOut, "Xeon: queued") {
		t.Errorf("run output unexpected:\n%s", runOut)
	}
}

func TestQuickActionLeafHelpCarriesAddressingAndHumanOnlyRules(t *testing.T) {
	tests := []struct {
		cmd       *cobra.Command
		ref       bool
		humanOnly bool
	}{
		{quickActionGetCmd, true, false},
		{quickActionCreateCmd, false, true},
		{quickActionUpdateCmd, true, true},
		{quickActionArchiveCmd, true, true},
		{quickActionRestoreCmd, true, true},
		{quickActionDeleteCmd, true, true},
		{quickActionRunCmd, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.cmd.CommandPath(), func(t *testing.T) {
			help := renderHelp(t, tt.cmd)
			if got := strings.Contains(help, quickActionRefNote); got != tt.ref {
				t.Errorf("addressing note present = %v, want %v; rendered:\n%s", got, tt.ref, help)
			}
			if got := strings.Contains(help, quickActionHumanOnlyNote); got != tt.humanOnly {
				t.Errorf("human-only note present = %v, want %v; rendered:\n%s", got, tt.humanOnly, help)
			}
		})
	}
}

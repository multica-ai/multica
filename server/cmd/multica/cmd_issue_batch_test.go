package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

func newIssueBatchUpdateTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "update"}
	addIssueBatchUpdateFlags(cmd)
	return cmd
}

type issueBatchTestServer struct {
	supported bool
	posts     []map[string]any
	response  map[string]any
}

func (s *issueBatchTestServer) start(t *testing.T, labels, properties []map[string]any) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/config":
			cfg := map[string]any{}
			if s.supported {
				cfg["issue_batch_ops_supported"] = true
			}
			json.NewEncoder(w).Encode(cfg)
		case "/api/labels":
			json.NewEncoder(w).Encode(map[string]any{"labels": labels})
		case "/api/properties":
			json.NewEncoder(w).Encode(map[string]any{"properties": properties})
		case "/api/issues/batch-update", "/api/issues/batch-delete":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			s.posts = append(s.posts, body)
			json.NewEncoder(w).Encode(s.response)
		default:
			if id, ok := strings.CutPrefix(r.URL.Path, "/api/issues/"); ok && r.Method == http.MethodGet {
				json.NewEncoder(w).Encode(map[string]any{"id": id, "identifier": "MUL-9"})
				return
			}
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "mat_test-token")
}

func TestIssueBatchUpdateFailsClosedOnOldServer(t *testing.T) {
	s := &issueBatchTestServer{}
	s.start(t, nil, nil)
	cmd := newIssueBatchUpdateTestCmd()
	_ = cmd.Flags().Set("status", "done")
	_ = cmd.Flags().Set("dry-run", "true")
	err := runIssueBatchUpdate(cmd, []string{uuid.NewString()})
	if err == nil || !strings.Contains(err.Error(), "does not support batch results") {
		t.Fatalf("error = %v, want capability failure", err)
	}
	if len(s.posts) != 0 {
		t.Fatalf("old server received %d batch POST(s); a dry run there would write", len(s.posts))
	}
}

func TestIssueBatchUpdateSendsLabelAndPropertyOpsAndFailsOnSkips(t *testing.T) {
	bugID, frontendID, staleID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	propertyID := uuid.NewString()
	a, b := uuid.NewString(), uuid.NewString()
	s := &issueBatchTestServer{supported: true, response: map[string]any{
		"updated": 1,
		"results": []map[string]any{
			{"issue_id": a, "identifier": "MUL-1", "outcome": "updated"},
			{"issue_id": b, "identifier": "MUL-2", "outcome": "skipped", "code": "parent_cycle", "message": "MUL-2 is an ancestor"},
		},
	}}
	s.start(t,
		[]map[string]any{{"id": bugID, "name": "Bug"}, {"id": frontendID, "name": "Frontend"}, {"id": staleID, "name": "Stale"}},
		[]map[string]any{{"id": propertyID, "name": "Notes", "type": "text", "config": map[string]any{}, "archived": false}},
	)

	cmd := newIssueBatchUpdateTestCmd()
	_ = cmd.Flags().Set("priority", "high")
	_ = cmd.Flags().Set("add-label", "bug")
	_ = cmd.Flags().Set("add-label", frontendID)
	_ = cmd.Flags().Set("remove-label", "Stale")
	_ = cmd.Flags().Set("property", "Notes=needs repro")
	_ = cmd.Flags().Set("output", "json")
	err := runIssueBatchUpdate(cmd, []string{a, b, a})
	if err == nil || !strings.Contains(err.Error(), "1 of 2 issues were skipped") {
		t.Fatalf("error = %v, want a skipped-issues failure", err)
	}
	if len(s.posts) != 1 {
		t.Fatalf("batch POST count = %d, want 1", len(s.posts))
	}
	body := s.posts[0]
	if got := body["issue_ids"]; !reflect.DeepEqual(got, []any{a, b}) {
		t.Errorf("issue_ids = %#v, want de-duplicated in order", got)
	}
	if got := body["updates"]; !reflect.DeepEqual(got, map[string]any{"priority": "high"}) {
		t.Errorf("updates = %#v", got)
	}
	wantLabels := map[string]any{"add": []any{bugID, frontendID}, "remove": []any{staleID}, "clear": false}
	if got := body["labels"]; !reflect.DeepEqual(got, wantLabels) {
		t.Errorf("labels = %#v, want %#v", got, wantLabels)
	}
	wantProps := map[string]any{"set": map[string]any{propertyID: "needs repro"}}
	if got := body["properties"]; !reflect.DeepEqual(got, wantProps) {
		t.Errorf("properties = %#v, want %#v", got, wantProps)
	}
	if _, ok := body["dry_run"]; ok {
		t.Errorf("dry_run sent without --dry-run")
	}
}

func TestIssueBatchUpdateRejectsMissingResults(t *testing.T) {
	s := &issueBatchTestServer{supported: true, response: map[string]any{"updated": 1}}
	s.start(t, nil, nil)
	cmd := newIssueBatchUpdateTestCmd()
	_ = cmd.Flags().Set("status", "done")
	err := runIssueBatchUpdate(cmd, []string{uuid.NewString()})
	if err == nil || !strings.Contains(err.Error(), "did not return per-issue results") {
		t.Fatalf("error = %v", err)
	}
}

func TestIssueBatchUpdateRequiresAChange(t *testing.T) {
	s := &issueBatchTestServer{supported: true}
	s.start(t, nil, nil)
	cmd := newIssueBatchUpdateTestCmd()
	err := runIssueBatchUpdate(cmd, []string{uuid.NewString()})
	if err == nil || !strings.Contains(err.Error(), "nothing to change") {
		t.Fatalf("error = %v", err)
	}
	if len(s.posts) != 0 {
		t.Fatalf("sent %d POST(s) for an empty change", len(s.posts))
	}
}

func TestIssueBatchUpdateClearAndReplaceLabels(t *testing.T) {
	bugID := uuid.NewString()
	id := uuid.NewString()
	s := &issueBatchTestServer{supported: true, response: map[string]any{
		"updated": 0, "dry_run": true,
		"results": []map[string]any{{"issue_id": id, "outcome": "would_update"}},
	}}
	s.start(t, []map[string]any{{"id": bugID, "name": "Bug"}}, nil)
	cmd := newIssueBatchUpdateTestCmd()
	_ = cmd.Flags().Set("clear-labels", "true")
	_ = cmd.Flags().Set("add-label", "Bug")
	_ = cmd.Flags().Set("dry-run", "true")
	if err := runIssueBatchUpdate(cmd, []string{id}); err != nil {
		t.Fatalf("runIssueBatchUpdate: %v", err)
	}
	body := s.posts[0]
	if body["dry_run"] != true {
		t.Errorf("dry_run = %#v", body["dry_run"])
	}
	if got := body["labels"]; !reflect.DeepEqual(got, map[string]any{"add": []any{bugID}, "remove": []any{}, "clear": true}) {
		t.Errorf("labels = %#v", got)
	}
}

func TestIssueDeleteDryRunSendsDryRunWithoutPrompt(t *testing.T) {
	id := uuid.NewString()
	s := &issueBatchTestServer{supported: true, response: map[string]any{
		"deleted": 0, "dry_run": true,
		"results": []map[string]any{{"issue_id": id, "identifier": "MUL-9", "outcome": "would_delete"}},
	}}
	s.start(t, nil, nil)
	if err := issueDeleteCmd.Flags().Set("dry-run", "true"); err != nil {
		t.Fatalf("set --dry-run: %v", err)
	}
	t.Cleanup(func() { _ = issueDeleteCmd.Flags().Set("dry-run", "false") })

	if err := runIssueDelete(issueDeleteCmd, []string{id}); err != nil {
		t.Fatalf("runIssueDelete: %v", err)
	}
	if len(s.posts) != 1 || s.posts[0]["dry_run"] != true {
		t.Fatalf("posts = %#v, want one batch-delete with dry_run", s.posts)
	}
}

package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestConditionalBatchRevisionProtectsCombinedFields(t *testing.T) {
	labelID := dbfx.Insert(t, "issue_label", testutil.Cols{"workspace_id": testWorkspaceID, "name": "conditional batch label", "color": "#aabbcc", "resource_type": "issue"})
	for _, tc := range []struct {
		name     string
		expected int
		seed     bool
		status   int
	}{
		{"matching replacement", 7, false, 200}, {"stale replacement", 6, false, 409},
		{"matching no-op", 7, true, 200}, {"stale no-op", 6, true, 409},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := dbfx.Issue(t, "conditional batch", testutil.Cols{"revision": 7})
			t.Cleanup(func() { dbfx.Exec(t, `DELETE FROM issue_to_label WHERE issue_id=$1`, id) })
			if tc.seed {
				dbfx.Exec(t, `INSERT INTO issue_to_label(issue_id,label_id) VALUES($1,$2)`, id, labelID)
			}
			body := map[string]any{"issue_ids": []string{id}, "labels": map[string]any{"clear": true, "add": []string{labelID}}}
			req := newRequest(http.MethodPost, "/api/issues/conditional-batch-update", body)
			req.URL.RawQuery = fmt.Sprintf("expected_revision=%d", tc.expected)
			response := httptest.NewRecorder()
			testHandler.BatchUpdateIssues(response, req)
			if response.Code != tc.status {
				t.Fatalf("status=%d want=%d body=%s", response.Code, tc.status, response.Body)
			}
			var revision int64
			var attached bool
			dbfx.QueryRow(t, `SELECT revision,EXISTS(SELECT 1 FROM issue_to_label WHERE issue_id=$1 AND label_id=$2) FROM issue WHERE id=$1`, id, labelID).Scan(&revision, &attached)
			if tc.status == 409 {
				if revision != 7 || attached != tc.seed {
					t.Fatalf("stale write changed revision=%d attached=%t", revision, attached)
				}
				return
			}
			var result struct {
				Results []struct {
					IssueRevision int64 `json:"issue_revision"`
				} `json:"results"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if len(result.Results) != 1 || result.Results[0].IssueRevision != revision || !attached {
				t.Fatalf("missing mutation revision: %s", response.Body)
			}
			if tc.seed && revision != 7 {
				t.Fatalf("no-op consumed revision %d", revision)
			}
		})
	}
}

func TestConditionalBatchRejectsManyIssues(t *testing.T) {
	a := dbfx.Issue(t, "conditional batch A")
	b := dbfx.Issue(t, "conditional batch B")
	req := newRequest(http.MethodPost, "/api/issues/conditional-batch-update", map[string]any{"issue_ids": []string{a, b}, "updates": map[string]any{"priority": "high"}})
	req.URL.RawQuery = "expected_revision=1"
	response := httptest.NewRecorder()
	testHandler.BatchUpdateIssues(response, req)
	if response.Code != 400 {
		t.Fatalf("scalar revision must reject many issues: status=%d body=%s", response.Code, response.Body)
	}
}

func TestConditionalBatchSerializesWithPrimaryWriter(t *testing.T) {
	labelID := dbfx.Insert(t, "issue_label", testutil.Cols{"workspace_id": testWorkspaceID, "name": "batch race label", "color": "#aabbcc", "resource_type": "issue"})
	id := dbfx.Issue(t, "batch versus primary", testutil.Cols{"revision": 7})
	t.Cleanup(func() { dbfx.Exec(t, `DELETE FROM issue_to_label WHERE issue_id=$1`, id) })
	start := make(chan struct{})
	results := make(chan int, 2)
	go func() {
		<-start
		req := newRequest(http.MethodPost, "/api/issues/conditional-batch-update", map[string]any{"issue_ids": []string{id}, "labels": map[string]any{"clear": true, "add": []string{labelID}}})
		req.URL.RawQuery = "expected_revision=7"
		response := httptest.NewRecorder()
		testHandler.BatchUpdateIssues(response, req)
		results <- response.Code
	}()
	go func() {
		<-start
		req := testutil.WithURLParams(newRequest(http.MethodPut, "/api/issues/"+id, map[string]any{"title": "other writer", "expected_revision": 7}), "id", id)
		response := httptest.NewRecorder()
		testHandler.UpdateIssue(response, req)
		results <- response.Code
	}()
	close(start)
	a, b := <-results, <-results
	if !((a == 200 && b == 409) || (a == 409 && b == 200)) {
		t.Fatalf("writers=%d,%d; want one success one conflict", a, b)
	}
	var revision int64
	dbfx.QueryRow(t, `SELECT revision FROM issue WHERE id=$1`, id).Scan(&revision)
	if revision != 8 {
		t.Fatalf("revision=%d; want exactly one writer", revision)
	}
}

func TestConditionalBatchCombinedMutationAdvancesRevisionOnce(t *testing.T) {
	property := createTestProperty(t, map[string]any{"name": "conditional combined property", "type": "text"})
	first := dbfx.Insert(t, "issue_label", testutil.Cols{"workspace_id": testWorkspaceID, "name": "old combined label", "color": "#aabbcc", "resource_type": "issue"})
	second := dbfx.Insert(t, "issue_label", testutil.Cols{"workspace_id": testWorkspaceID, "name": "new combined label", "color": "#aabbcc", "resource_type": "issue"})
	for _, fields := range []bool{false, true} {
		t.Run(fmt.Sprint(fields), func(t *testing.T) {
			id := dbfx.Issue(t, "combined revision once", testutil.Cols{"revision": 7})
			dbfx.Exec(t, `INSERT INTO issue_to_label(issue_id,label_id) VALUES($1,$2)`, id, first)
			t.Cleanup(func() { dbfx.Exec(t, `DELETE FROM issue_to_label WHERE issue_id=$1`, id) })
			body := map[string]any{"issue_ids": []string{id}, "labels": map[string]any{"clear": true, "add": []string{second}}, "properties": map[string]any{"set": map[string]string{property.ID: "after"}}}
			if fields {
				body["updates"] = map[string]any{"priority": "high"}
			}
			req := newRequest(http.MethodPost, "/api/issues/conditional-batch-update", body)
			req.URL.RawQuery = "expected_revision=7"
			response := httptest.NewRecorder()
			testHandler.BatchUpdateIssues(response, req)
			if response.Code != 200 {
				t.Fatalf("status=%d body=%s", response.Code, response.Body)
			}
			var revision int64
			dbfx.QueryRow(t, `SELECT revision FROM issue WHERE id=$1`, id).Scan(&revision)
			if revision != 8 {
				t.Fatalf("one conditional mutation advanced to %d, want 8; body=%s", revision, response.Body)
			}
		})
	}
}

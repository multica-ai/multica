package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestIssueMetadataRevisionPrecondition(t *testing.T) {
	for _, tc := range []struct {
		name, method, key string
		expected          int64
		value             any
		wantStatus        int
		wantRevision      int64
		wantValue         string
	}{
		{"set matching", http.MethodPut, "state", 7, "after", 200, 8, "after"},
		{"set stale", http.MethodPut, "state", 6, "after", 409, 7, "before"},
		{"set matching same value", http.MethodPut, "state", 7, "before", 200, 7, "before"},
		{"set stale same value", http.MethodPut, "state", 6, "before", 409, 7, "before"},
		{"delete matching", http.MethodDelete, "state", 7, nil, 200, 8, ""},
		{"delete stale", http.MethodDelete, "state", 6, nil, 409, 7, "before"},
		{"delete matching absent key", http.MethodDelete, "missing", 7, nil, 200, 7, "before"},
		{"delete stale absent key", http.MethodDelete, "missing", 6, nil, 409, 7, "before"},
		{"reject invalid revision", http.MethodPut, "state", 0, "after", 400, 7, "before"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := dbfx.Issue(t, "conditional metadata", testutil.Cols{"metadata": testutil.Raw(`'{"state":"before"}'::jsonb`), "revision": 7})
			var body any
			if tc.method == http.MethodPut {
				body = map[string]any{"value": tc.value}
			}
			request := mutationRequest(tc.method, id, tc.key, body)
			request.URL.RawQuery = fmt.Sprintf("expected_revision=%d", tc.expected)
			response := httptest.NewRecorder()
			if tc.method == http.MethodPut {
				testHandler.SetIssueMetadataKey(response, request)
			} else {
				testHandler.DeleteIssueMetadataKey(response, request)
			}
			if response.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d; body=%s", response.Code, tc.wantStatus, response.Body)
			}
			var revision int64
			var metadata map[string]any
			var raw []byte
			dbfx.QueryRow(t, `SELECT revision,metadata FROM issue WHERE id=$1`, id).Scan(&revision, &raw)
			if err := json.Unmarshal(raw, &metadata); err != nil {
				t.Fatal(err)
			}
			if revision != tc.wantRevision {
				t.Errorf("revision=%d, want %d", revision, tc.wantRevision)
			}
			if got, _ := metadata["state"].(string); got != tc.wantValue {
				t.Errorf("state=%q, want %q", got, tc.wantValue)
			}
			var output struct {
				Code           string `json:"code"`
				ActualRevision int64  `json:"actual_revision"`
				IssueRevision  int64  `json:"issue_revision"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &output); err != nil {
				t.Fatal(err)
			}
			if tc.wantStatus == 409 && (output.Code != "revision_conflict" || output.ActualRevision != 7) {
				t.Errorf("conflict=%+v, want revision_conflict at 7", output)
			}
			if tc.wantStatus == 200 && output.IssueRevision != tc.wantRevision {
				t.Errorf("response revision=%d, want %d", output.IssueRevision, tc.wantRevision)
			}
		})
	}
}

func TestIssueRevisionSerializesMetadataAndFieldWriters(t *testing.T) {
	id := dbfx.Issue(t, "concurrent conditional fields", testutil.Cols{"metadata": testutil.Raw(`'{"state":"before"}'::jsonb`), "revision": 7})
	start := make(chan struct{})
	result := make(chan struct {
		name   string
		status int
	}, 2)
	go func() {
		<-start
		request := mutationRequest(http.MethodPut, id, "state", map[string]any{"value": "after"})
		request.URL.RawQuery = "expected_revision=7"
		response := httptest.NewRecorder()
		testHandler.SetIssueMetadataKey(response, request)
		result <- struct {
			name   string
			status int
		}{"metadata", response.Code}
	}()
	go func() {
		<-start
		request := testutil.WithURLParams(newRequest(http.MethodPut, "/api/issues/"+id, map[string]any{"title": "Changed title", "expected_revision": 7}), "id", id)
		response := httptest.NewRecorder()
		testHandler.UpdateIssue(response, request)
		result <- struct {
			name   string
			status int
		}{"title", response.Code}
	}()
	close(start)
	outcomes := map[string]int{}
	for range 2 {
		r := <-result
		outcomes[r.name] = r.status
	}
	if !((outcomes["metadata"] == 200 && outcomes["title"] == 409) || (outcomes["metadata"] == 409 && outcomes["title"] == 200)) {
		t.Fatalf("writers=%v, want exactly one success and one conflict", outcomes)
	}
	var revision int64
	var title, state string
	dbfx.QueryRow(t, `SELECT revision,title,metadata->>'state' FROM issue WHERE id=$1`, id).Scan(&revision, &title, &state)
	if revision != 8 {
		t.Errorf("revision=%d, want 8", revision)
	}
	if outcomes["metadata"] == 200 && (state != "after" || title != "concurrent conditional fields") {
		t.Errorf("losing title write leaked: title=%q state=%q", title, state)
	}
	if outcomes["title"] == 200 && (title != "Changed title" || state != "before") {
		t.Errorf("losing metadata write leaked: title=%q state=%q", title, state)
	}
}

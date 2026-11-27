package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestIssuePropertyRevisionPrecondition(t *testing.T) {
	property := createTestProperty(t, map[string]any{"name": "Conditional revision text", "type": "text"})
	for _, tc := range []struct {
		name, method string
		expected     int64
		seed         bool
		value        string
		wantStatus   int
		wantRevision int64
		wantValue    string
	}{
		{"set matching", http.MethodPut, 7, true, "after", 200, 8, "after"},
		{"set stale", http.MethodPut, 6, true, "after", 409, 7, "before"},
		{"set matching same value", http.MethodPut, 7, true, "before", 200, 7, "before"},
		{"set stale same value", http.MethodPut, 6, true, "before", 409, 7, "before"},
		{"unset matching", http.MethodDelete, 7, true, "", 200, 8, ""},
		{"unset stale", http.MethodDelete, 6, true, "", 409, 7, "before"},
		{"unset matching absent value", http.MethodDelete, 7, false, "", 200, 7, ""},
		{"unset stale absent value", http.MethodDelete, 6, false, "", 409, 7, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cols := testutil.Cols{"revision": 7}
			if tc.seed {
				cols["properties"] = testutil.Raw(fmt.Sprintf("jsonb_build_object('%s','before')", property.ID))
			}
			id := dbfx.Issue(t, "conditional property", cols)
			var body any
			if tc.method == http.MethodPut {
				body = map[string]any{"value": tc.value}
			}
			req := withIssuePropertyParams(newRequest(tc.method, "/api/issues/"+id+"/properties/"+property.ID, body), id, property.ID)
			req.URL.RawQuery = fmt.Sprintf("expected_revision=%d", tc.expected)
			response := httptest.NewRecorder()
			if tc.method == http.MethodPut {
				testHandler.SetIssueProperty(response, req)
			} else {
				testHandler.DeleteIssueProperty(response, req)
			}
			if response.Code != tc.wantStatus {
				t.Errorf("status=%d, want %d; body=%s", response.Code, tc.wantStatus, response.Body)
			}
			var revision int64
			var values []byte
			dbfx.QueryRow(t, `SELECT revision,properties FROM issue WHERE id=$1`, id).Scan(&revision, &values)
			var got map[string]any
			if err := json.Unmarshal(values, &got); err != nil {
				t.Fatal(err)
			}
			if revision != tc.wantRevision {
				t.Errorf("revision=%d, want %d", revision, tc.wantRevision)
			}
			if value, _ := got[property.ID].(string); value != tc.wantValue {
				t.Errorf("value=%q, want %q", value, tc.wantValue)
			}
			var result struct {
				Code           string `json:"code"`
				ActualRevision int64  `json:"actual_revision"`
				IssueRevision  int64  `json:"issue_revision"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if tc.wantStatus == 409 && (result.Code != "revision_conflict" || result.ActualRevision != 7) {
				t.Errorf("conflict=%+v", result)
			}
			if tc.wantStatus == 200 && result.IssueRevision != tc.wantRevision {
				t.Errorf("response revision=%d, want %d", result.IssueRevision, tc.wantRevision)
			}
		})
	}
}

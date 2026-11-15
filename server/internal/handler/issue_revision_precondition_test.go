package handler

import (
	"encoding/json"
	"fmt"
	"github.com/multica-ai/multica/server/internal/testutil"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequireIssueExpectedRevision(t *testing.T) {
	for _, tc := range []struct {
		name, query string
		want        int
	}{
		{"missing", "", 400},
		{"valid", "expected_revision=7", 204},
		{"empty", "expected_revision=", 400},
		{"zero", "expected_revision=0", 400},
		{"negative", "expected_revision=-1", 400},
		{"non-integer", "expected_revision=abc", 400},
		{"duplicate", "expected_revision=7&expected_revision=8", 400},
		{"overflow", "expected_revision=9223372036854775808", 400},
		{"malformed encoding", "expected_revision=%zz", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			next := RequireIssueExpectedRevision(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(http.StatusNoContent) }))
			req := httptest.NewRequest(http.MethodPut, "/api/issues/id/conditional/metadata/state", nil)
			req.URL.RawQuery = tc.query
			response := httptest.NewRecorder()
			next.ServeHTTP(response, req)
			if response.Code != tc.want {
				t.Errorf("status=%d, want %d", response.Code, tc.want)
			}
			if called != (tc.want == http.StatusNoContent) {
				t.Errorf("mutation callback invoked=%t, status=%d", called, response.Code)
			}
		})
	}
}

func TestConditionalPrimaryIssueNoopKeepsRevision(t *testing.T) {
	for _, expected := range []int64{7, 6} {
		t.Run(fmt.Sprint(expected), func(t *testing.T) {
			id := dbfx.Issue(t, "unchanged conditional title", testutil.Cols{"revision": 7})
			req := testutil.WithURLParams(newRequest(http.MethodPut, "/api/issues/"+id, map[string]any{"title": "unchanged conditional title", "expected_revision": expected}), "id", id)
			response := httptest.NewRecorder()
			testHandler.UpdateIssue(response, req)
			want := http.StatusOK
			if expected != 7 {
				want = http.StatusConflict
			}
			if response.Code != want {
				t.Fatalf("status=%d, want %d: %s", response.Code, want, response.Body)
			}
			var revision int64
			dbfx.QueryRow(t, `SELECT revision FROM issue WHERE id=$1`, id).Scan(&revision)
			if revision != 7 {
				t.Fatalf("no-op consumed revision: %d", revision)
			}
			if expected == 7 {
				var result struct {
					Revision int64 `json:"revision"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if result.Revision != 7 {
					t.Fatalf("response revision=%d", result.Revision)
				}
			}
		})
	}
}

func TestConditionalPrimaryRequiresMatchingBodyRevision(t *testing.T) {
	for _, body := range []map[string]any{{"title": "after"}, {"title": "after", "expected_revision": 7}} {
		id := dbfx.Issue(t, "conditional body guard", testutil.Cols{"revision": 7})
		req := testutil.WithURLParams(newRequest(http.MethodPut, "/api/issues/"+id+"/conditional", body), "id", id)
		req.URL.RawQuery = "expected_revision=8"
		response := httptest.NewRecorder()
		testHandler.UpdateIssue(response, req)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("status=%d body=%s; query and body preconditions must agree", response.Code, response.Body)
		}
	}
}

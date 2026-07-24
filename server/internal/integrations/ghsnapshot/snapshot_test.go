package ghsnapshot

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// graphqlServer stands in for GitHub's GraphQL endpoint. tokenHandler mints a
// token; the queryFn produces the `data` payload for each GraphQL request,
// receiving the request variables so it can page.
func graphqlServer(t *testing.T, queryFn func(vars map[string]any) string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/access_tokens") {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"token":"ghs_secret","expires_at":"` +
				time.Now().Add(time.Hour).UTC().Format(time.RFC3339) + `"}`))
			return
		}
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Variables map[string]any `json:"variables"`
		}
		_ = json.Unmarshal(body, &req)
		_, _ = w.Write([]byte(`{"data":` + queryFn(req.Variables) + `}`))
	}))
}

// TestFetchPRSnapshotPaginatesContexts is the acceptance-criterion-2 test:
// contexts spanning two pages must all be collected — never assume <100.
func TestFetchPRSnapshotPaginatesContexts(t *testing.T) {
	srv := graphqlServer(t, func(vars map[string]any) string {
		cursor, _ := vars["cursor"].(string)
		if cursor == "" {
			// Page 1: two contexts, more to come.
			return `{"repository":{"pullRequest":{
				"headRefOid":"sha1","mergeable":"MERGEABLE","mergeStateStatus":"CLEAN",
				"commits":{"nodes":[{"commit":{"statusCheckRollup":{"state":"FAILURE","contexts":{
					"pageInfo":{"hasNextPage":true,"endCursor":"CUR2"},
					"nodes":[
						{"__typename":"CheckRun","name":"backend","status":"COMPLETED","conclusion":"FAILURE","detailsUrl":"u1"},
						{"__typename":"CheckRun","name":"frontend","status":"COMPLETED","conclusion":"SUCCESS","detailsUrl":"u2"}
					]}}}}]}}}}`
		}
		// Page 2 (cursor==CUR2): one more context, plus a legacy StatusContext.
		return `{"repository":{"pullRequest":{
			"headRefOid":"sha1","mergeable":"MERGEABLE","mergeStateStatus":"CLEAN",
			"commits":{"nodes":[{"commit":{"statusCheckRollup":{"state":"FAILURE","contexts":{
				"pageInfo":{"hasNextPage":false,"endCursor":""},
				"nodes":[
					{"__typename":"CheckRun","name":"e2e","status":"IN_PROGRESS","conclusion":null,"detailsUrl":"u3"},
					{"__typename":"StatusContext","context":"vercel","state":"SUCCESS","targetUrl":"u4"}
				]}}}}]}}}}`
	})
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	snap, err := FetchPRSnapshot(context.Background(), c, 1, "o", "r", 5)
	if err != nil {
		t.Fatalf("FetchPRSnapshot: %v", err)
	}
	if snap.HeadSHA != "sha1" || snap.Mergeable != "MERGEABLE" || snap.MergeStateStatus != "CLEAN" {
		t.Fatalf("head/merge = %+v", snap)
	}
	if !snap.HasChecks || snap.RollupState != "FAILURE" {
		t.Fatalf("rollup = %q hasChecks=%v", snap.RollupState, snap.HasChecks)
	}
	if len(snap.Contexts) != 4 {
		t.Fatalf("collected %d contexts across pages, want 4", len(snap.Contexts))
	}
	// StatusContext normalization: SUCCESS → completed/success, is_status_context.
	last := snap.Contexts[3]
	if last.Name != "vercel" || last.Status != "completed" || last.Conclusion != "success" || !last.IsStatusContext {
		t.Fatalf("status context normalized wrong: %+v", last)
	}
	// In-progress CheckRun stays running.
	if snap.Contexts[2].Status != "in_progress" || snap.Contexts[2].Conclusion != "" {
		t.Fatalf("in-progress run normalized wrong: %+v", snap.Contexts[2])
	}
	if snap.Decided() {
		t.Fatal("snapshot with an in-progress run must be undecided")
	}
}

// TestFetchPRSnapshotNullRollup is the acceptance-criterion-5 test: a null
// statusCheckRollup means "no checks yet" (HasChecks=false), never passed.
func TestFetchPRSnapshotNullRollup(t *testing.T) {
	srv := graphqlServer(t, func(vars map[string]any) string {
		return `{"repository":{"pullRequest":{
			"headRefOid":"sha9","mergeable":"UNKNOWN","mergeStateStatus":"UNKNOWN",
			"commits":{"nodes":[{"commit":{"statusCheckRollup":null}}]}}}}`
	})
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	snap, err := FetchPRSnapshot(context.Background(), c, 1, "o", "r", 5)
	if err != nil {
		t.Fatalf("FetchPRSnapshot: %v", err)
	}
	if snap.HasChecks {
		t.Fatal("null rollup must yield HasChecks=false")
	}
	if snap.RollupState != "" || len(snap.Contexts) != 0 {
		t.Fatalf("null rollup must have empty rollup/contexts, got %+v", snap)
	}
	if snap.Decided() {
		t.Fatal("UNKNOWN mergeable must be undecided")
	}
}

func TestSnapshotDecided(t *testing.T) {
	cases := []struct {
		name string
		snap PRSnapshot
		want bool
	}{
		{"clean passed", PRSnapshot{Mergeable: "MERGEABLE", HasChecks: true, RollupState: "SUCCESS",
			Contexts: []CheckContext{{Status: "completed", Conclusion: "success"}}}, true},
		{"conflicting decided", PRSnapshot{Mergeable: "CONFLICTING", HasChecks: false}, true},
		{"mergeable unknown", PRSnapshot{Mergeable: "UNKNOWN", HasChecks: true, RollupState: "SUCCESS"}, false},
		{"rollup pending", PRSnapshot{Mergeable: "MERGEABLE", HasChecks: true, RollupState: "PENDING"}, false},
		{"running context", PRSnapshot{Mergeable: "MERGEABLE", HasChecks: true, RollupState: "SUCCESS",
			Contexts: []CheckContext{{Status: "in_progress"}}}, false},
		{"no checks but mergeable", PRSnapshot{Mergeable: "MERGEABLE", HasChecks: false}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.snap.Decided(); got != tc.want {
				t.Fatalf("Decided() = %v, want %v", got, tc.want)
			}
		})
	}
}

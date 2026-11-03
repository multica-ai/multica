package ghsnapshot

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Only the REST endpoints covered by the App's existing permissions are served.
// Fork PRs deliberately have a head repository outside the installation.
func snapshotServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/app/installations/1/access_tokens" {
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"token": "ghs_secret", "expires_at": time.Now().Add(time.Hour).Format(time.RFC3339),
				"permissions": map[string]string{"pull_requests": "read", "checks": "read", "statuses": "read"},
			})
			return
		}
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer ghs_secret" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		handler(w, r)
	}))
}

func writeTestPR(w http.ResponseWriter, sha string, mergeable any, state string) {
	_ = json.NewEncoder(w).Encode(map[string]any{
		"head":      map[string]any{"sha": sha, "repo": map[string]any{"full_name": "external/fork"}},
		"mergeable": mergeable, "mergeable_state": state,
	})
}

func TestFetchPRSnapshotRESTPagination(t *testing.T) {
	prCalls, runPages, statusPages := 0, 0, 0
	srv := snapshotServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r/pulls/5":
			prCalls++
			writeTestPR(w, "sha1", true, "clean")
		case "/repos/o/r/commits/sha1/check-runs":
			runPages++
			if r.URL.Query().Get("per_page") != "100" || r.URL.Query().Get("filter") != "latest" {
				t.Errorf("check query = %s", r.URL.RawQuery)
			}
			var runs []map[string]any
			if r.URL.Query().Get("page") == "1" {
				for i := 1; i <= 100; i++ {
					runs = append(runs, map[string]any{"id": i, "head_sha": "sha1", "name": fmt.Sprintf("job%d", i), "status": "completed", "conclusion": "success", "details_url": "https://example.com/run"})
				}
			} else {
				runs = []map[string]any{
					{"id": 101, "head_sha": "sha1", "name": "expired", "status": "completed", "conclusion": "stale"},
					{"id": 102, "head_sha": "sha1", "name": "running", "status": "in_progress", "conclusion": nil},
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"total_count": 102, "check_runs": runs})
		case "/repos/o/r/commits/sha1/status":
			statusPages++
			var statuses []map[string]any
			if r.URL.Query().Get("page") == "1" {
				for i := 1; i <= 100; i++ {
					statuses = append(statuses, map[string]any{"context": fmt.Sprintf("status%d", i), "state": "success", "target_url": "https://example.com/status"})
				}
			} else {
				statuses = []map[string]any{{"context": "deploy", "state": "pending"}}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"total_count": 101, "statuses": statuses})
		default:
			t.Errorf("request outside base repository REST endpoints: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	defer srv.Close()
	snap, err := FetchPRSnapshot(context.Background(), newTestClient(t, srv.URL), 1, "o", "r", 5)
	if err != nil {
		t.Fatal(err)
	}
	if snap.HeadSHA != "sha1" || snap.Mergeable != "MERGEABLE" || snap.MergeStateStatus != "CLEAN" || !snap.HasChecks || snap.RollupState != "FAILURE" {
		t.Fatalf("snapshot = %+v", snap)
	}
	if len(snap.Contexts) != 203 || prCalls != 2 || runPages != 2 || statusPages != 2 {
		t.Fatalf("contexts=%d PR calls=%d check pages=%d status pages=%d", len(snap.Contexts), prCalls, runPages, statusPages)
	}
	if snap.Contexts[100].Conclusion != "stale" || snap.Contexts[101].Status != "in_progress" || snap.Contexts[101].Conclusion != "" || snap.Decided() {
		t.Fatalf("failure plus running checks = %+v", snap)
	}
	if last := snap.Contexts[202]; last.Status != "in_progress" || !last.IsStatusContext {
		t.Fatalf("legacy pending status = %+v", last)
	}
}

func TestFetchPRSnapshotRESTResults(t *testing.T) {
	cases := []struct {
		name, runs, statuses, want string
	}{
		{"no checks", `[]`, `[]`, ""},
		{"only check run", `[{"id":1,"head_sha":"sha1","name":"ci","status":"completed","conclusion":"success"}]`, `[]`, "SUCCESS"},
		{"only status", `[]`, `[{"context":"deploy","state":"success"}]`, "SUCCESS"},
		{"status error", `[]`, `[{"context":"deploy","state":"error"}]`, "FAILURE"},
		{"stale fails", `[{"id":1,"head_sha":"sha1","name":"ci","status":"completed","conclusion":"stale"}]`, `[]`, "FAILURE"},
		{"unknown conclusion", `[{"id":1,"head_sha":"sha1","name":"ci","status":"completed","conclusion":"future_result"}]`, `[]`, "PENDING"},
		{"empty conclusion", `[{"id":1,"head_sha":"sha1","name":"ci","status":"completed","conclusion":null}]`, `[]`, "PENDING"},
		{"unknown status", `[]`, `[{"context":"deploy","state":"future_state"}]`, "PENDING"},
		{"failure before pending", `[{"id":1,"head_sha":"sha1","name":"ci","status":"completed","conclusion":"failure"}]`, `[{"context":"deploy","state":"pending"}]`, "FAILURE"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := snapshotServer(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/repos/o/r/pulls/5":
					writeTestPR(w, "sha1", nil, "unknown")
				case "/repos/o/r/commits/sha1/check-runs":
					var entries []json.RawMessage
					_ = json.Unmarshal([]byte(tc.runs), &entries)
					_, _ = fmt.Fprintf(w, `{"total_count":%d,"check_runs":%s}`, len(entries), tc.runs)
				case "/repos/o/r/commits/sha1/status":
					var entries []json.RawMessage
					_ = json.Unmarshal([]byte(tc.statuses), &entries)
					_, _ = fmt.Fprintf(w, `{"total_count":%d,"statuses":%s}`, len(entries), tc.statuses)
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			})
			defer srv.Close()
			snap, err := FetchPRSnapshot(context.Background(), newTestClient(t, srv.URL), 1, "o", "r", 5)
			if err != nil {
				t.Fatal(err)
			}
			if tc.name == "unknown conclusion" || tc.name == "empty conclusion" {
				if snap.Contexts[0].Status == "completed" || snap.Contexts[0].Conclusion != "" {
					t.Fatalf("unknown result must stay running: %+v", snap.Contexts[0])
				}
			}
			if snap.RollupState != tc.want || snap.HasChecks != (tc.want != "") || snap.Mergeable != "UNKNOWN" || snap.MergeStateStatus != "UNKNOWN" || snap.Decided() {
				t.Fatalf("snapshot = %+v, want rollup %q", snap, tc.want)
			}
		})
	}
}

func TestFetchPRSnapshotRESTRejectsPartialResults(t *testing.T) {
	for _, failure := range []string{"head changed", "missing head", "check page failed", "status failed", "page limit", "truncated page", "count changed", "missing collection", "duplicate run", "wrong head"} {
		t.Run(failure, func(t *testing.T) {
			prCalls := 0
			srv := snapshotServer(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/repos/o/r/pulls/5":
					prCalls++
					sha := "sha1"
					if failure == "head changed" && prCalls == 2 {
						sha = "sha2"
					}
					if failure == "missing head" {
						sha = ""
					}
					writeTestPR(w, sha, false, "dirty")
				case "/repos/o/r/commits/sha1/check-runs":
					page := r.URL.Query().Get("page")
					if failure == "check page failed" && page == "2" {
						w.WriteHeader(500)
						return
					}
					if failure == "missing collection" {
						_, _ = w.Write([]byte(`{"total_count":0}`))
						return
					}
					if failure == "truncated page" {
						_, _ = w.Write([]byte(`{"total_count":1,"check_runs":[]}`))
						return
					}
					count := 0
					var runs []map[string]any = []map[string]any{}
					if failure == "check page failed" || failure == "page limit" || failure == "count changed" || failure == "duplicate run" {
						count = 101
						if failure == "page limit" {
							count = 100*maxSnapshotContextPages + 1
						}
						if failure == "count changed" && page == "2" {
							count = 102
						}
						for i := 0; i < 100; i++ {
							var n int
							_, _ = fmt.Sscan(page, &n)
							id := (n-1)*100 + i + 1
							if failure == "duplicate run" {
								id = 1
							}
							runs = append(runs, map[string]any{"id": id, "head_sha": "sha1", "name": fmt.Sprint(id), "status": "completed", "conclusion": "success"})
						}
					}
					if failure == "wrong head" {
						count = 1
						runs = []map[string]any{{"id": 1, "head_sha": "sha2", "name": "ci", "status": "completed", "conclusion": "success"}}
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"total_count": count, "check_runs": runs})
				case "/repos/o/r/commits/sha1/status":
					if failure == "status failed" {
						w.WriteHeader(404)
						return
					}
					_, _ = w.Write([]byte(`{"total_count":0,"statuses":[]}`))
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			})
			defer srv.Close()
			snap, err := FetchPRSnapshot(context.Background(), newTestClient(t, srv.URL), 1, "o", "r", 5)
			if err == nil || snap != nil {
				t.Fatalf("snapshot=%+v err=%v; partial results must be rejected", snap, err)
			}
			if strings.Contains(err.Error(), "ghs_secret") {
				t.Fatal("error leaked installation token")
			}
		})
	}
}

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

// Reorder may probe beyond a genuine single-page total because legacy servers
// use that same value when COUNT fails. Fixtures must honor offset as the API does.
func writeReorderColumnPage(w http.ResponseWriter, r *http.Request, all []any) {
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	page := all[min(offset, len(all)):min(offset+100, len(all))]
	_ = json.NewEncoder(w).Encode(map[string]any{"issues": page, "total": len(all)})
}

func TestRunIssueReorderPaginationIntegrity(t *testing.T) {
	for _, mode := range []string{"healthy", "failed_count", "missing_total", "smaller_pages", "next_page_error", "duplicate_page", "malformed_page", "missing_page"} {
		t.Run(mode, func(t *testing.T) {
			t.Chdir(t.TempDir())
			all := make([]map[string]any, 145)
			for i := range all {
				all[i] = mkIssue(fmt.Sprintf("11111111-1111-4111-8111-%012d", i+1), fmt.Sprintf("MUL-%d", i+1), "todo", float64(i))
				all[i]["project_id"] = "project-1"
			}
			target := all[0]
			position, requests, writes := -1.0, 0, 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/api/issues":
					requests++
					q := r.URL.Query()
					for key, want := range map[string]string{"workspace_id": "ws-1", "project_id": "project-1", "status": "todo", "sort": "position", "limit": "100"} {
						if q.Get(key) != want {
							t.Errorf("%s=%q, want %q", key, q.Get(key), want)
						}
					}
					offset, _ := strconv.Atoi(q.Get("offset"))
					if mode == "next_page_error" && offset > 0 {
						http.Error(w, "failed to count issues", http.StatusInternalServerError)
						return
					}
					if mode == "duplicate_page" {
						offset = 0
					}
					pageSize := 100
					if mode == "smaller_pages" {
						pageSize = 20
					}
					page := all[min(offset, len(all)):min(offset+pageSize, len(all))]
					body := map[string]any{"issues": page, "total": len(all)}
					switch mode {
					case "failed_count", "smaller_pages":
						body["total"] = len(page)
					case "missing_total":
						delete(body, "total")
					case "malformed_page":
						body["issues"] = []any{nil}
					case "missing_page":
						delete(body, "issues")
					}
					_ = json.NewEncoder(w).Encode(body)
				case r.Method == http.MethodGet && r.URL.Path == "/api/issues/"+strVal(target, "id"):
					_ = json.NewEncoder(w).Encode(target)
				case r.Method == http.MethodPut && r.URL.Path == "/api/issues/"+strVal(target, "id"):
					writes++
					var body map[string]any
					_ = json.NewDecoder(r.Body).Decode(&body)
					position = floatVal(body, "position")
					_ = json.NewEncoder(w).Encode(target)
				default:
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
			t.Setenv("MULTICA_SERVER_URL", srv.URL)
			t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
			t.Setenv("MULTICA_TOKEN", "test-token")
			cmd := newIssueReorderTestCmd()
			_ = cmd.Flags().Set("bottom", "true")
			_, err := captureStdout(t, func() error { return runIssueReorder(cmd, []string{strVal(target, "id")}) })
			switch mode {
			case "next_page_error", "duplicate_page", "malformed_page", "missing_page":
				if err == nil || writes != 0 {
					t.Fatalf("invalid column must prevent writes: err=%v, writes=%d", err, writes)
				}
			default:
				if err != nil {
					t.Fatal(err)
				}
				if position != 145 || writes != 1 {
					t.Fatalf("bottom position=%v, writes=%d, pages=%d; want one write at 145", position, writes, requests)
				}
			}
		})
	}
}

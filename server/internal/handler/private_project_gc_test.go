package handler

import (
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// A member's credential on the daemon GC routes must not read the lifecycle
// of an issue in a private project they cannot see (MUL-8472). A daemon token
// keeps reading it: its runtime may run that project's tasks for others.
func TestPrivateProjectDaemonIssueGCChecks(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	f := newPrivateProjectFixture(t)
	missingID := "00000000-0000-0000-0000-000000000099"

	for _, tc := range []struct {
		name      string
		userID    string
		token     bool
		wantFound bool
	}{
		{"user outsider", f.outsider, false, false},
		{"user project member", f.insider, false, true},
		{"user workspace owner", testUserID, false, true},
		{"daemon token of a runtime owner outside the project", f.outsider, true, true},
		{"daemon token project member", f.insider, true, true},
		{"workspace token", "", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := func(method, path string, body any) *http.Request {
				r := asUser(newRequest(method, path, body), tc.userID)
				if tc.token {
					ctx := middleware.WithDaemonContext(r.Context(), testWorkspaceID, "gc-test-daemon")
					ctx = middleware.WithDaemonTokenOwnerID(ctx, tc.userID)
					r = r.WithContext(ctx)
				}
				return r
			}
			t.Run("single", func(t *testing.T) {
				get := func(id string, want int) string {
					r := withURLParam(request(http.MethodGet, "/api/daemon/issues/"+id+"/gc-check", nil), "issueId", id)
					return testutil.Call(t, testHandler.GetIssueGCCheck, r).Want(want).Body.String()
				}
				want := http.StatusOK
				if !tc.wantFound {
					want = http.StatusNotFound
				}
				secret := get(f.secretIssue, want)
				missing := get(missingID, http.StatusNotFound)
				if !tc.wantFound && secret != missing {
					t.Fatalf("hidden issue response %s differs from missing %s", secret, missing)
				}
				get(f.publicIssue, http.StatusOK)
			})
			t.Run("batch", func(t *testing.T) {
				ids := []string{f.secretIssue, missingID, f.publicIssue, f.secretIssue}
				r := withURLParam(request(http.MethodPost, "/api/daemon/workspaces/"+testWorkspaceID+"/issues/gc-check", map[string]any{"issue_ids": ids}), "workspaceId", testWorkspaceID)
				var out struct {
					Issues []batchIssueGCCheckItem `json:"issues"`
				}
				testutil.Call(t, testHandler.BatchIssueGCCheck, r).Want(http.StatusOK).JSON(&out)
				if len(out.Issues) != len(ids) {
					t.Fatalf("got %d results, want %d", len(out.Issues), len(ids))
				}
				for i, item := range out.Issues {
					wantFound := i == 2 || (i != 1 && tc.wantFound)
					if item.ID != ids[i] || item.Found != wantFound {
						t.Errorf("result %d = %+v, want id %s, found %t", i, item, ids[i], wantFound)
					}
					if wantFound {
						if item.Status == "" || item.Category == "" || item.UpdatedAt == nil {
							t.Errorf("visible issue lacks lifecycle metadata: %+v", item)
						}
					} else if item.Status != "" || item.Category != "" || item.UpdatedAt != nil {
						t.Errorf("hidden/missing issue exposes lifecycle metadata: %+v", item)
					}
				}
			})
		})
	}
}

package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestStripActivityIssueRef(t *testing.T) {
	cases := []struct {
		name, action, details, want string
	}{
		{"marked keeps the rest", "duplicate_marked", `{"original_id":"a","original_identifier":"MUL-1","to":"done"}`, `{"to":"done"}`},
		{"added", "duplicate_added", `{"duplicate_id":"a","duplicate_identifier":"MUL-2"}`, `{}`},
		{"unrelated action untouched", "status_changed", `{"from":"todo","to":"done"}`, `{"from":"todo","to":"done"}`},
		{"no ref keys untouched", "duplicate_removed", `{"reason":"x"}`, `{"reason":"x"}`},
		{"unreadable details dropped", "duplicate_marked", `not json`, `{}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := string(StripActivityIssueRef(tc.action, []byte(tc.details))); got != tc.want {
				t.Errorf("StripActivityIssueRef(%s, %s) = %s, want %s", tc.action, tc.details, got, tc.want)
			}
		})
	}
}

// A duplicate mark names an issue in another project. Readers who can see the
// project feed or the issue's timeline but not that other project must not
// learn the other issue's id or identifier; readers who can see both still do.
func TestActivityRefsToHiddenIssuesAreRedacted(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	f := newPrivateProjectFixture(t)
	openProject := dbfx.Project(t, f.marker+" open project")
	openIssue := dbfx.Issue(t, f.marker+" open issue", testutil.Cols{"project_id": openProject})

	// The open issue is a duplicate of the secret one, and the secret issue
	// has the public one as its duplicate.
	insertActivity := func(issueID, action string, details map[string]string) {
		t.Helper()
		body, _ := json.Marshal(details)
		dbfx.Exec(t, `
			INSERT INTO activity_log (workspace_id, issue_id, actor_type, actor_id, action, details)
			VALUES ($1, $2, 'member', $3, $4, $5::jsonb)
		`, testWorkspaceID, issueID, testUserID, action, string(body))
	}
	insertActivity(openIssue, "duplicate_marked", map[string]string{"original_id": f.secretIssue, "original_identifier": "SECRET-1"})
	insertActivity(f.publicIssue, "duplicate_added", map[string]string{"duplicate_id": f.secretIssue, "duplicate_identifier": "SECRET-1"})
	insertActivity(f.publicIssue, "duplicate_removed", map[string]string{"duplicate_id": openIssue, "duplicate_identifier": "OPEN-1"})

	projectFeed := func(userID string) map[string]map[string]string {
		t.Helper()
		var resp ProjectActivityResponse
		req := withURLParam(asUser(newRequest(http.MethodGet, "/api/projects/"+openProject+"/activity", nil), userID), "id", openProject)
		rec := httptest.NewRecorder()
		testHandler.ListProjectActivity(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("project activity as %s: %d %s", userID, rec.Code, rec.Body.String())
		}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode project activity: %v", err)
		}
		out := map[string]map[string]string{}
		for _, e := range resp.Entries {
			var d map[string]string
			_ = json.Unmarshal(e.Details, &d)
			out[e.Action] = d
		}
		return out
	}
	timeline := func(userID, issueID string) map[string]map[string]string {
		t.Helper()
		var entries []TimelineEntry
		req := withURLParam(asUser(newRequest(http.MethodGet, "/api/issues/"+issueID+"/timeline", nil), userID), "id", issueID)
		rec := httptest.NewRecorder()
		testHandler.ListTimeline(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("timeline as %s: %d %s", userID, rec.Code, rec.Body.String())
		}
		if err := json.NewDecoder(rec.Body).Decode(&entries); err != nil {
			t.Fatalf("decode timeline: %v", err)
		}
		out := map[string]map[string]string{}
		for _, e := range entries {
			if e.Action == nil {
				continue
			}
			var d map[string]string
			_ = json.Unmarshal(e.Details, &d)
			out[*e.Action] = d
		}
		return out
	}

	for _, reader := range []struct{ name, id string }{{"outsider", f.outsider}, {"outside admin", f.outsideAdmin}} {
		t.Run(reader.name, func(t *testing.T) {
			if d := projectFeed(reader.id)["duplicate_marked"]; d == nil || d["original_id"] != "" || d["original_identifier"] != "" {
				t.Errorf("project feed duplicate_marked = %v, want the secret original redacted", d)
			}
			if d := timeline(reader.id, openIssue)["duplicate_marked"]; d == nil || d["original_id"] != "" || d["original_identifier"] != "" {
				t.Errorf("timeline duplicate_marked = %v, want the secret original redacted", d)
			}
			public := timeline(reader.id, f.publicIssue)
			if d := public["duplicate_added"]; d == nil || d["duplicate_id"] != "" || d["duplicate_identifier"] != "" {
				t.Errorf("timeline duplicate_added = %v, want the secret duplicate redacted", d)
			}
			if d := public["duplicate_removed"]; d["duplicate_id"] != openIssue || d["duplicate_identifier"] != "OPEN-1" {
				t.Errorf("timeline duplicate_removed = %v, want the visible duplicate kept", d)
			}
		})
	}

	t.Run("insider", func(t *testing.T) {
		if d := projectFeed(f.insider)["duplicate_marked"]; d["original_id"] != f.secretIssue || d["original_identifier"] != "SECRET-1" {
			t.Errorf("project feed duplicate_marked = %v, want the original kept", d)
		}
		if d := timeline(f.insider, f.publicIssue)["duplicate_added"]; d["duplicate_id"] != f.secretIssue || d["duplicate_identifier"] != "SECRET-1" {
			t.Errorf("timeline duplicate_added = %v, want the duplicate kept", d)
		}
	})
}

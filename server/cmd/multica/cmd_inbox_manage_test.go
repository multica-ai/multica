package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

const (
	inboxItemA   = "aaaaaaaa-0000-0000-0000-00000000000a"
	inboxItemB   = "bbbbbbbb-0000-0000-0000-00000000000b"
	inboxItemC   = "cccccccc-0000-0000-0000-00000000000c"
	inboxIssue1  = "11111111-0000-0000-0000-000000000001"
	inboxIssue2  = "22222222-0000-0000-0000-000000000002"
	inboxMember  = "33333333-0000-0000-0000-000000000003"
	inboxAgent   = "44444444-0000-0000-0000-000000000004"
	inboxProject = "55555555-0000-0000-0000-000000000005"
)

type inboxRequest struct {
	method, uri, body string
}

// fakeInboxServer answers each "METHOD path" from routes and records every
// request in order.
func fakeInboxServer(t *testing.T, routes map[string]func(w http.ResponseWriter, r *http.Request)) *[]inboxRequest {
	t.Helper()
	requests := &[]inboxRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		*requests = append(*requests, inboxRequest{r.Method, r.URL.RequestURI(), string(body)})
		if r.Header.Get("Authorization") != "Bearer mat_inbox-test" || r.Header.Get("X-Workspace-ID") != "ws-1" {
			t.Errorf("lost workspace or task credential on %s %s", r.Method, r.URL)
		}
		if handler, ok := routes[r.Method+" "+r.URL.Path]; ok {
			handler(w, r)
			return
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "mat_inbox-test")
	t.Setenv("MULTICA_AGENT_ID", "agent-1")
	t.Setenv("MULTICA_TASK_ID", "task-1")
	return requests
}

func reply(body string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }
}

func runInboxCommand(t *testing.T, register func(*cobra.Command), run func(*cobra.Command, []string) error, args []string, flags map[string]string) (string, error) {
	t.Helper()
	cmd := &cobra.Command{Use: "test"}
	register(cmd)
	for name, value := range flags {
		if err := cmd.Flags().Set(name, value); err != nil {
			t.Fatal(err)
		}
	}
	return captureStdout(t, func() error { return run(cmd, args) })
}

// withEmptyStdin stands in for an agent run, which cannot answer a prompt.
func withEmptyStdin(t *testing.T) {
	t.Helper()
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdin
	os.Stdin = devNull
	t.Cleanup(func() { os.Stdin = old; devNull.Close() })
}

func TestInboxListFiltersAndEnrichesRows(t *testing.T) {
	recent := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	old := time.Now().Add(-72 * time.Hour).UTC().Format(time.RFC3339)
	requests := fakeInboxServer(t, map[string]func(http.ResponseWriter, *http.Request){
		"GET /api/inbox": reply(`[
			{"id":"` + inboxItemA + `","type":"new_comment","title":"Comment on X","issue_id":"` + inboxIssue1 + `","read":false,"created_at":"` + recent + `",
			 "actor_type":"member","actor_id":"` + inboxMember + `","body":"Looks good,\n  ship it","details":{"comment_id":"c1"}},
			{"id":"` + inboxItemB + `","type":"mentioned","title":"Mention on Y","issue_id":"` + inboxIssue2 + `","read":true,"created_at":"` + recent + `",
			 "actor_type":"agent","actor_id":"` + inboxAgent + `"},
			{"id":"old","type":"new_comment","title":"Old","issue_id":"` + inboxIssue1 + `","read":false,"created_at":"` + old + `"}
		]`),
		"GET /api/issues": func(w http.ResponseWriter, r *http.Request) {
			if ids := r.URL.Query().Get("ids"); ids != inboxIssue1 {
				t.Errorf("issue lookup ids = %q, want only the listed issue", ids)
			}
			_, _ = w.Write([]byte(`{"issues":[{"id":"` + inboxIssue1 + `","identifier":"MUL-1","title":"X"}]}`))
		},
		"GET /api/workspaces/ws-1/members": reply(`[{"user_id":"` + inboxMember + `","name":"Jiayuan"}]`),
	})
	out, err := runInboxCommand(t, registerInboxListFlags, func(cmd *cobra.Command, _ []string) error { return runInboxList(cmd, nil) }, nil,
		map[string]string{"unread": "true", "since": "1d", "issue": inboxIssue1, "output": "json"})
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0]["id"] != inboxItemA {
		t.Fatalf("filters kept %v, want only the recent unread row on the issue", rows)
	}
	row := rows[0]
	if row["issue_identifier"] != "MUL-1" || row["actor"] != "member:Jiayuan" || row["summary"] != "Looks good, ship it" || row["comment_id"] != "c1" {
		t.Fatalf("row not enriched: %v", row)
	}
	if _, ok := row["body"]; ok {
		t.Fatal("raw body leaked into list output")
	}
	if len(*requests) != 3 {
		t.Fatalf("requests = %v; want inbox, one issue lookup and one member lookup", *requests)
	}
}

// The archive used to come from a list capped at 200 issue groups, so an older
// archived issue could not be found to restore it.
func TestInboxListArchivedReadsEveryPage(t *testing.T) {
	const groups = 250
	var pages []url.Values
	fakeInboxServer(t, map[string]func(http.ResponseWriter, *http.Request){
		"GET /api/inbox/archived/page": func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			pages = append(pages, q)
			start := 0
			if cursor := q.Get("cursor"); cursor != "" {
				start, _ = strconv.Atoi(cursor)
			}
			limit, _ := strconv.Atoi(q.Get("limit"))
			end := min(start+limit, groups)
			items := make([]string, 0, end-start)
			for i := start; i < end; i++ {
				items = append(items, fmt.Sprintf(`{"id":"%08d-0000-0000-0000-000000000000","type":"system","title":"Notice %d","read":true,"created_at":"2026-11-01T00:00:00Z"}`, i, i))
			}
			next := "null"
			if end < groups {
				next = fmt.Sprintf(`"%d"`, end)
			}
			fmt.Fprintf(w, `{"items":[%s],"next_cursor":%s,"has_more":%t}`, strings.Join(items, ","), next, end < groups)
		},
	})
	out, err := runInboxCommand(t, registerInboxListFlags, func(cmd *cobra.Command, _ []string) error { return runInboxList(cmd, nil) }, nil,
		map[string]string{"archived": "true", "output": "json"})
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != groups || rows[groups-1]["title"] != fmt.Sprintf("Notice %d", groups-1) {
		t.Fatalf("listed %d archived groups, want all %d", len(rows), groups)
	}
	if len(pages) != 3 || pages[0].Get("cursor") != "" || pages[1].Get("cursor") != "100" || pages[2].Get("cursor") != "200" {
		t.Fatalf("pages = %v", pages)
	}
}

func TestInboxListArchivedSendsFiltersToServer(t *testing.T) {
	var query url.Values
	fakeInboxServer(t, map[string]func(http.ResponseWriter, *http.Request){
		"GET /api/projects/" + inboxProject: reply(`{"id":"` + inboxProject + `"}`),
		"GET /api/inbox/archived/page": func(w http.ResponseWriter, r *http.Request) {
			query = r.URL.Query()
			_, _ = w.Write([]byte(`{"items":[{"id":"` + inboxItemA + `","type":"new_comment","title":"Old","issue_id":"` + inboxIssue1 + `","issue_project_id":"` + inboxProject + `","read":false,"created_at":"` + time.Now().UTC().Format(time.RFC3339) + `"}],"next_cursor":null,"has_more":false}`))
		},
		"GET /api/issues": reply(`{"issues":[{"id":"` + inboxIssue1 + `","identifier":"MUL-1","title":"X"}]}`),
	})
	out, err := runInboxCommand(t, registerInboxListFlags, func(cmd *cobra.Command, _ []string) error { return runInboxList(cmd, nil) }, nil,
		map[string]string{"archived": "true", "issue": inboxIssue1, "project": inboxProject, "unread": "true", "since": "1d", "type": "new_comment", "output": "json"})
	if err != nil || !strings.Contains(out, inboxItemA) {
		t.Fatalf("archived: %s %v", out, err)
	}
	since, err := time.Parse(time.RFC3339Nano, query.Get("received_after"))
	if query.Get("group_id") != inboxIssue1 || query.Get("projects") != inboxProject || query.Get("unread_only") != "true" || query.Get("notification_types") != "new_comment" ||
		err != nil || time.Since(since) < 23*time.Hour || time.Since(since) > 25*time.Hour {
		t.Fatalf("archive page query = %v", query)
	}
}

// The archive lists each issue under one notification, so a mention under a
// later status change is only found when the type reaches the server.
func TestInboxListArchivedTypeFindsOlderNotification(t *testing.T) {
	fakeInboxServer(t, map[string]func(http.ResponseWriter, *http.Request){
		"GET /api/inbox/archived/page": func(w http.ResponseWriter, r *http.Request) {
			id, kind := inboxItemB, "status_changed"
			if r.URL.Query().Get("notification_types") == "mentioned" {
				id, kind = inboxItemA, "mentioned"
			}
			fmt.Fprintf(w, `{"items":[{"id":"%s","type":"%s","title":"T","issue_id":"%s","read":true,"created_at":"2026-11-01T00:00:00Z"}],"next_cursor":null,"has_more":false}`, id, kind, inboxIssue1)
		},
		"GET /api/issues": reply(`{"issues":[{"id":"` + inboxIssue1 + `","identifier":"MUL-1","title":"X"}]}`),
	})
	out, err := runInboxCommand(t, registerInboxListFlags, func(cmd *cobra.Command, _ []string) error { return runInboxList(cmd, nil) }, nil,
		map[string]string{"archived": "true", "type": "mentioned", "issue": inboxIssue1, "output": "json"})
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0]["id"] != inboxItemA || rows[0]["type"] != "mentioned" {
		t.Fatalf("archived --type mentioned = %v, want the older mention", rows)
	}
}

func TestInboxListSnoozed(t *testing.T) {
	requests := fakeInboxServer(t, map[string]func(http.ResponseWriter, *http.Request){
		"GET /api/inbox/snoozed": reply(`[{"issue_id":"` + inboxIssue1 + `","item_id":"` + inboxItemB + `","snoozed_until":"2026-12-01T09:00:00Z","snoozed_at":"2026-11-18T00:00:00Z","updates":2}]`),
		"GET /api/issues":        reply(`{"issues":[{"id":"` + inboxIssue1 + `","identifier":"MUL-1","title":"X"}]}`),
	})
	list := func(cmd *cobra.Command, _ []string) error { return runInboxList(cmd, nil) }

	out, err := runInboxCommand(t, registerInboxListFlags, list, nil, map[string]string{"snoozed": "true", "output": "json"})
	if err != nil {
		t.Fatal(err)
	}
	var snoozed []map[string]any
	if err := json.Unmarshal([]byte(out), &snoozed); err != nil {
		t.Fatal(err)
	}
	if len(snoozed) != 1 || snoozed[0]["id"] != inboxItemB || snoozed[0]["issue_identifier"] != "MUL-1" || snoozed[0]["title"] != "X" || snoozed[0]["updates"] != float64(2) {
		t.Fatalf("snoozed row = %v", snoozed)
	}

	before := len(*requests)
	for _, flags := range []map[string]string{
		{"archived": "true", "snoozed": "true"},
		{"snoozed": "true", "unread": "true"},
		{"since": "next tuesday"},
	} {
		if _, err := runInboxCommand(t, registerInboxListFlags, list, nil, flags); err == nil {
			t.Errorf("flags %v accepted", flags)
		}
	}
	if len(*requests) != before {
		t.Fatal("invalid flags reached the server")
	}
}

func TestInboxItemWritesReportCountAndFailures(t *testing.T) {
	requests := fakeInboxServer(t, map[string]func(http.ResponseWriter, *http.Request){
		// A reads its whole issue; C, on the same issue, has nothing left to read.
		"POST /api/inbox/" + inboxItemA + "/read": reply(`{"id":"` + inboxItemA + `","issue_id":"` + inboxIssue1 + `","count":3}`),
		"POST /api/inbox/" + inboxItemC + "/read": reply(`{"id":"` + inboxItemC + `","issue_id":"` + inboxIssue1 + `","count":0}`),
		"POST /api/inbox/" + inboxItemB + "/read": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"inbox access requires the runtime owner's verified human trigger"}`))
		},
	})
	out, err := runInboxCommand(t, registerInboxReadFlags, runInboxRead, []string{inboxItemA, strings.ToUpper(inboxItemA), inboxItemB, inboxItemC}, map[string]string{"output": "json"})
	if err == nil || !strings.Contains(err.Error(), "1 of 3") {
		t.Fatalf("partial failure error = %v", err)
	}
	var result inboxWriteResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != "read" || result.Count != 3 || len(result.Items) != 2 ||
		result.Items[0].ID != inboxItemA || result.Items[0].Count != 3 || *result.Items[0].IssueID != inboxIssue1 ||
		result.Items[1].ID != inboxItemC || result.Items[1].Count != 0 ||
		len(result.Failed) != 1 || result.Failed[0].ID != inboxItemB || !strings.Contains(result.Failed[0].Error, "verified human trigger") {
		t.Fatalf("result = %+v", result)
	}
	if len(*requests) != 3 {
		t.Fatalf("duplicate id not collapsed or failure stopped the batch: %v", *requests)
	}

	*requests = nil
	out, err = runInboxCommand(t, registerInboxReadFlags, runInboxRead, []string{inboxItemA, inboxItemC}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{inboxItemA + ": 3 notifications", inboxItemC + ": no change", "Marked read 3 notifications"} {
		if !strings.Contains(out, line) {
			t.Errorf("table output missing %q: %s", line, out)
		}
	}

	*requests = nil
	if _, err := runInboxCommand(t, registerInboxReadFlags, runInboxRead, []string{inboxItemA, "aaaa"}, nil); err == nil || !strings.Contains(err.Error(), "not a full UUID") {
		t.Fatalf("short id error = %v", err)
	}
	if len(*requests) != 0 {
		t.Fatal("an invalid id must stop the command before any write")
	}
}

func TestInboxArchiveUnarchiveUnreadUnsnoozeHitTheirEndpoints(t *testing.T) {
	for _, tc := range []struct {
		name     string
		register func(*cobra.Command)
		run      func(*cobra.Command, []string) error
		action   string
	}{
		{"unread", registerInboxOutputFlag, runInboxUnread, "unread"},
		{"archive", registerInboxArchiveFlags, runInboxArchive, "archive"},
		{"unarchive", registerInboxOutputFlag, runInboxUnarchive, "unarchive"},
		{"unsnooze", registerInboxOutputFlag, runInboxUnsnooze, "unsnooze"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// An older server answers without a count; the notification itself still counts.
			for body, want := range map[string]int64{`{"issue_id":"` + inboxIssue1 + `","count":4}`: 4, `{"issue_id":"` + inboxIssue1 + `"}`: 1} {
				requests := fakeInboxServer(t, map[string]func(http.ResponseWriter, *http.Request){
					"POST /api/inbox/" + inboxItemA + "/" + tc.action: reply(body),
				})
				out, err := runInboxCommand(t, tc.register, tc.run, []string{inboxItemA}, map[string]string{"output": "json"})
				if err != nil {
					t.Fatal(err)
				}
				var result inboxWriteResult
				if err := json.Unmarshal([]byte(out), &result); err != nil || result.Action != tc.action || result.Count != want {
					t.Fatalf("answer %s: result = %s (%v), want count %d", body, out, err, want)
				}
				if len(*requests) != 1 {
					t.Fatalf("requests = %v", *requests)
				}
			}
		})
	}
}

func TestInboxSnoozeSendsUntil(t *testing.T) {
	requests := fakeInboxServer(t, map[string]func(http.ResponseWriter, *http.Request){
		"POST /api/inbox/" + inboxItemA + "/snooze": reply(`{"issue_id":"` + inboxIssue1 + `","count":3}`),
		"POST /api/inbox/" + inboxItemC + "/snooze": reply(`{"issue_id":"` + inboxIssue1 + `","count":3}`),
		"POST /api/inbox/" + inboxItemB + "/snooze": reply(`{"issue_id":"` + inboxIssue2 + `","count":2}`),
	})
	until := time.Now().Add(48 * time.Hour).UTC().Truncate(time.Second).Format(time.RFC3339)
	out, err := runInboxCommand(t, registerInboxSnoozeFlags, runInboxSnooze, []string{inboxItemA, inboxItemC, inboxItemB}, map[string]string{"until": until, "output": "json"})
	if err != nil {
		t.Fatal(err)
	}
	if len(*requests) != 3 || (*requests)[0].body != `{"until":"`+until+`"}` {
		t.Fatalf("requests = %v", *requests)
	}
	var result inboxWriteResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	// C re-snoozes A's issue to the same time, which changes nothing.
	if result.SnoozedUntil != until || result.Count != 5 || len(result.Items) != 3 || result.Items[1].Count != 0 {
		t.Fatalf("result = %+v", result)
	}

	*requests = nil
	for _, flags := range []map[string]string{{}, {"until": "2020-01-01T00:00:00Z"}, {"until": "someday"}} {
		if _, err := runInboxCommand(t, registerInboxSnoozeFlags, runInboxSnooze, []string{inboxItemA}, flags); err == nil {
			t.Errorf("--until %v accepted", flags)
		}
	}
	if len(*requests) != 0 {
		t.Fatal("an invalid --until reached the server")
	}
}

func TestInboxReadIssueAndBulkModes(t *testing.T) {
	requests := fakeInboxServer(t, map[string]func(http.ResponseWriter, *http.Request){
		"GET /api/issues/MUL-1":                           reply(`{"id":"` + inboxIssue1 + `","identifier":"MUL-1"}`),
		"POST /api/inbox/issues/" + inboxIssue1 + "/read": reply(`{"count":4}`),
		"POST /api/inbox/mark-all-read":                   reply(`{"count":35}`),
		"POST /api/inbox/archive-all":                     reply(`{"count":7}`),
		"POST /api/inbox/archive-all-read":                reply(`{"count":5}`),
		"POST /api/inbox/archive-completed":               reply(`{"count":2}`),
	})
	count := func(out string) int64 {
		t.Helper()
		var result inboxWriteResult
		if err := json.Unmarshal([]byte(out), &result); err != nil {
			t.Fatalf("%s: %v", out, err)
		}
		return result.Count
	}

	out, err := runInboxCommand(t, registerInboxReadFlags, runInboxRead, nil, map[string]string{"issue": "MUL-1", "output": "json"})
	if err != nil || count(out) != 4 {
		t.Fatalf("read --issue: %s %v", out, err)
	}

	withEmptyStdin(t)
	*requests = nil
	for _, tc := range []struct {
		register func(*cobra.Command)
		run      func(*cobra.Command, []string) error
		flag     string
	}{
		{registerInboxReadFlags, runInboxRead, "all"},
		{registerInboxArchiveFlags, runInboxArchive, "all"},
		{registerInboxArchiveFlags, runInboxArchive, "read"},
		{registerInboxArchiveFlags, runInboxArchive, "completed"},
	} {
		if _, err := runInboxCommand(t, tc.register, tc.run, nil, map[string]string{tc.flag: "true"}); err == nil || !strings.Contains(err.Error(), "--yes") {
			t.Fatalf("--%s without --yes: %v", tc.flag, err)
		}
	}
	if len(*requests) != 0 {
		t.Fatalf("an unconfirmed bulk change reached the server: %v", *requests)
	}

	for _, tc := range []struct {
		register func(*cobra.Command)
		run      func(*cobra.Command, []string) error
		flag     string
		want     int64
	}{
		{registerInboxReadFlags, runInboxRead, "all", 35},
		{registerInboxArchiveFlags, runInboxArchive, "all", 7},
		{registerInboxArchiveFlags, runInboxArchive, "read", 5},
		{registerInboxArchiveFlags, runInboxArchive, "completed", 2},
	} {
		out, err := runInboxCommand(t, tc.register, tc.run, nil, map[string]string{tc.flag: "true", "yes": "true", "output": "json"})
		if err != nil || count(out) != tc.want {
			t.Fatalf("--%s --yes: %s %v", tc.flag, out, err)
		}
	}

	*requests = nil
	for _, tc := range []struct {
		register func(*cobra.Command)
		run      func(*cobra.Command, []string) error
		args     []string
		flags    map[string]string
	}{
		{registerInboxReadFlags, runInboxRead, nil, nil},
		{registerInboxReadFlags, runInboxRead, []string{inboxItemA}, map[string]string{"all": "true", "yes": "true"}},
		{registerInboxReadFlags, runInboxRead, nil, map[string]string{"issue": "MUL-1", "all": "true"}},
		{registerInboxArchiveFlags, runInboxArchive, nil, map[string]string{"read": "true", "completed": "true", "yes": "true"}},
	} {
		if _, err := runInboxCommand(t, tc.register, tc.run, tc.args, tc.flags); err == nil {
			t.Errorf("ambiguous selection accepted: args=%v flags=%v", tc.args, tc.flags)
		}
	}
	if len(*requests) != 0 {
		t.Fatalf("an ambiguous selection reached the server: %v", *requests)
	}
}

func TestInboxCount(t *testing.T) {
	fakeInboxServer(t, map[string]func(http.ResponseWriter, *http.Request){
		"GET /api/inbox/unread-count":   reply(`{"count":36}`),
		"GET /api/inbox/unread-summary": reply(`[{"workspace_id":"ws-1","count":36},{"workspace_id":"ws-2","count":1}]`),
	})
	out, err := runInboxCommand(t, registerInboxOutputFlag, runInboxCount, nil, map[string]string{"output": "json"})
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		WorkspaceID string `json:"workspace_id"`
		Unread      int64  `json:"unread"`
		Workspaces  []struct {
			WorkspaceID string `json:"workspace_id"`
			Count       int64  `json:"count"`
		} `json:"workspaces"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil || result.WorkspaceID != "ws-1" || result.Unread != 36 || len(result.Workspaces) != 2 {
		t.Fatalf("count = %s (%v)", out, err)
	}
}

func TestInboxCommandsRegisteredWithoutDelete(t *testing.T) {
	for _, name := range []string{"list", "count", "read", "unread", "archive", "unarchive", "snooze", "unsnooze"} {
		cmd, _, err := rootCmd.Find([]string{"inbox", name})
		if err != nil || cmd.Name() != name || cmd.Parent().Name() != "inbox" {
			t.Errorf("inbox %s not registered: %v", name, err)
		}
	}
	for _, cmd := range inboxCmd.Commands() {
		if cmd.Name() == "delete" || cmd.HasAlias("delete") || cmd.HasAlias("rm") {
			t.Fatal("notifications must not be deletable from the CLI")
		}
	}
}

func TestParseInboxTime(t *testing.T) {
	loc := time.FixedZone("UTC+8", 8*3600)
	afternoon := time.Date(2026, 11, 18, 15, 30, 0, 0, loc)
	night := time.Date(2026, 11, 18, 23, 10, 0, 0, loc)
	for _, tc := range []struct {
		value string
		now   time.Time
		back  bool
		want  time.Time
	}{
		{"2026-12-01T09:00:00Z", afternoon, false, time.Date(2026, 12, 1, 9, 0, 0, 0, time.UTC)},
		{"30m", afternoon, false, afternoon.Add(30 * time.Minute)},
		{"3h", afternoon, false, afternoon.Add(3 * time.Hour)},
		{"1h30m", afternoon, false, afternoon.Add(90 * time.Minute)},
		{"1d", afternoon, false, time.Date(2026, 11, 19, 15, 30, 0, 0, loc)},
		{"1w", afternoon, false, time.Date(2026, 11, 25, 15, 30, 0, 0, loc)},
		{"1d", night, false, time.Date(2026, 11, 19, 9, 0, 0, 0, loc)},
		{"3h", night, false, night.Add(3 * time.Hour)},
		{"Tomorrow", afternoon, false, time.Date(2026, 11, 19, 9, 0, 0, 0, loc)},
		{"2d", night, true, time.Date(2026, 11, 16, 23, 10, 0, 0, loc)},
		{"24h", afternoon, true, afternoon.Add(-24 * time.Hour)},
	} {
		got, err := parseInboxTime(tc.value, tc.now, tc.back)
		if err != nil || !got.Equal(tc.want) {
			t.Errorf("parseInboxTime(%q, back=%v) = %v, %v; want %v", tc.value, tc.back, got, err, tc.want)
		}
	}
	for _, value := range []string{"", "soon", "0d", "-1h", "tomorrow-ish"} {
		if _, err := parseInboxTime(value, afternoon, false); err == nil {
			t.Errorf("parseInboxTime(%q) accepted", value)
		}
	}
	if _, err := parseInboxTime("tomorrow", afternoon, true); err == nil {
		t.Error("--since tomorrow accepted")
	}
}

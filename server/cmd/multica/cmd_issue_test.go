package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// pipeStdin replaces os.Stdin with a pipe seeded by the given body for the
// duration of fn, so resolveTextFlag's --content-stdin / --description-stdin
// branch can be exercised in unit tests without spawning a subprocess.
func pipeStdin(t *testing.T, body string, fn func()) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	if _, err := w.WriteString(body); err != nil {
		t.Fatalf("write pipe: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close pipe writer: %v", err)
	}
	orig := os.Stdin
	os.Stdin = r
	defer func() {
		os.Stdin = orig
		_ = r.Close()
	}()
	fn()
}

// newFlagTestCmd builds a throwaway cobra.Command carrying the inline +
// stdin + file flag triplet that resolveTextFlag expects.
func newFlagTestCmd(name string) *cobra.Command {
	c := &cobra.Command{Use: "test"}
	c.Flags().String(name, "", "")
	c.Flags().Bool(name+"-stdin", false, "")
	c.Flags().String(name+"-file", "", "")
	return c
}

func TestResolveTextFlag(t *testing.T) {
	t.Run("inline value is unescaped", func(t *testing.T) {
		c := newFlagTestCmd("description")
		_ = c.Flags().Set("description", `para1\n\npara2`)
		got, ok, err := resolveTextFlag(c, "description")
		if err != nil || !ok {
			t.Fatalf("unexpected: ok=%v err=%v", ok, err)
		}
		if got != "para1\n\npara2" {
			t.Errorf("got %q, want decoded paragraphs", got)
		}
	})

	t.Run("stdin body is preserved verbatim", func(t *testing.T) {
		c := newFlagTestCmd("description")
		_ = c.Flags().Set("description-stdin", "true")
		body := "first line\nsecond line with a literal \\n in it\n"
		pipeStdin(t, body, func() {
			got, ok, err := resolveTextFlag(c, "description")
			if err != nil || !ok {
				t.Fatalf("unexpected: ok=%v err=%v", ok, err)
			}
			// strings.TrimSuffix one trailing newline like content-stdin.
			want := "first line\nsecond line with a literal \\n in it"
			if got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	})

	t.Run("inline + stdin is rejected", func(t *testing.T) {
		c := newFlagTestCmd("description")
		_ = c.Flags().Set("description", "inline")
		_ = c.Flags().Set("description-stdin", "true")
		if _, _, err := resolveTextFlag(c, "description"); err == nil {
			t.Fatalf("expected mutually-exclusive error")
		}
	})

	t.Run("missing both returns hasValue=false", func(t *testing.T) {
		c := newFlagTestCmd("description")
		got, ok, err := resolveTextFlag(c, "description")
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if ok || got != "" {
			t.Errorf("expected absent flag to yield (\"\", false), got (%q, %v)", got, ok)
		}
	})

	// --content-file / --description-file exists for Windows agents — piping
	// HEREDOC content through Windows PowerShell silently drops non-ASCII
	// bytes (PS 5.1's $OutputEncoding defaults to ASCIIEncoding when piping
	// to a native command), so Chinese / Cyrillic / etc. arrive as `?`.
	// Reading the body straight off disk skips the shell entirely.
	// See issues #2198, #2236, #2376.
	t.Run("file body is preserved verbatim with non-ASCII content", func(t *testing.T) {
		dir := t.TempDir()
		path := dir + string(os.PathSeparator) + "desc.md"
		body := "标题 / Заголовок\n\n中文段落 with `code` and \"quotes\".\n"
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write tempfile: %v", err)
		}
		c := newFlagTestCmd("description")
		_ = c.Flags().Set("description-file", path)
		got, ok, err := resolveTextFlag(c, "description")
		if err != nil || !ok {
			t.Fatalf("unexpected: ok=%v err=%v", ok, err)
		}
		want := "标题 / Заголовок\n\n中文段落 with `code` and \"quotes\"."
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("file path that doesn't exist surfaces a useful error", func(t *testing.T) {
		c := newFlagTestCmd("content")
		_ = c.Flags().Set("content-file", "/this/path/does/not/exist.txt")
		_, _, err := resolveTextFlag(c, "content")
		if err == nil {
			t.Fatalf("expected error for missing file")
		}
		if !strings.Contains(err.Error(), "content-file") {
			t.Errorf("error should mention --content-file, got %v", err)
		}
	})

	t.Run("empty file is rejected", func(t *testing.T) {
		dir := t.TempDir()
		path := dir + string(os.PathSeparator) + "empty.md"
		if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
			t.Fatalf("write tempfile: %v", err)
		}
		c := newFlagTestCmd("description")
		_ = c.Flags().Set("description-file", path)
		_, _, err := resolveTextFlag(c, "description")
		if err == nil {
			t.Fatalf("expected error for empty file")
		}
	})

	t.Run("file plus inline is rejected", func(t *testing.T) {
		dir := t.TempDir()
		path := dir + string(os.PathSeparator) + "x.md"
		if err := os.WriteFile(path, []byte("body"), 0o644); err != nil {
			t.Fatalf("write tempfile: %v", err)
		}
		c := newFlagTestCmd("description")
		_ = c.Flags().Set("description", "inline")
		_ = c.Flags().Set("description-file", path)
		if _, _, err := resolveTextFlag(c, "description"); err == nil {
			t.Fatalf("expected mutually-exclusive error for inline + file")
		}
	})

	t.Run("file plus stdin is rejected", func(t *testing.T) {
		dir := t.TempDir()
		path := dir + string(os.PathSeparator) + "x.md"
		if err := os.WriteFile(path, []byte("body"), 0o644); err != nil {
			t.Fatalf("write tempfile: %v", err)
		}
		c := newFlagTestCmd("description")
		_ = c.Flags().Set("description-stdin", "true")
		_ = c.Flags().Set("description-file", path)
		if _, _, err := resolveTextFlag(c, "description"); err == nil {
			t.Fatalf("expected mutually-exclusive error for stdin + file")
		}
	})
}

func newIssueCreateTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "create"}
	cmd.Flags().String("title", "", "")
	cmd.Flags().String("description", "", "")
	cmd.Flags().Bool("description-stdin", false, "")
	cmd.Flags().String("description-file", "", "")
	cmd.Flags().String("status", "", "")
	cmd.Flags().String("priority", "", "")
	cmd.Flags().String("assignee", "", "")
	cmd.Flags().String("assignee-id", "", "")
	cmd.Flags().String("parent", "", "")
	cmd.Flags().String("project", "", "")
	cmd.Flags().String("due-date", "", "")
	cmd.Flags().Bool("allow-duplicate", false, "")
	cmd.Flags().String("output", "json", "")
	cmd.Flags().StringSlice("attachment", nil, "")
	return cmd
}

func TestRunIssueCreateSendsAllowDuplicate(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/issues" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"id":         "issue-1",
			"identifier": "MUL-1",
			"title":      "Duplicate allowed",
			"status":     "todo",
			"priority":   "none",
		})
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newIssueCreateTestCmd()
	_ = cmd.Flags().Set("title", "Duplicate allowed")
	_ = cmd.Flags().Set("allow-duplicate", "true")
	if err := runIssueCreate(cmd, nil); err != nil {
		t.Fatalf("runIssueCreate: %v", err)
	}
	if got := body["allow_duplicate"]; got != true {
		t.Fatalf("allow_duplicate = %#v, want true in request body", got)
	}
}

func TestRunIssueCreateShowsDuplicateMessage(t *testing.T) {
	want := "Active duplicate issue exists: YUA-36 SH-PM-SYNTH-01 Synthesize recommendation-to-shortlist planning outputs (status: in_progress). Set allow_duplicate=true or use --allow-duplicate to create another."
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/issues" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]any{
			"code":  "active_duplicate_issue",
			"error": want,
			"issue": map[string]any{
				"id":         "issue-id",
				"identifier": "YUA-36",
				"status":     "in_progress",
			},
		})
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newIssueCreateTestCmd()
	_ = cmd.Flags().Set("title", "SH-PM-SYNTH-01 Synthesize recommendation-to-shortlist planning outputs")
	err := runIssueCreate(cmd, nil)
	if err == nil {
		t.Fatal("runIssueCreate: expected duplicate error")
	}
	if got := err.Error(); got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
}

func TestTruncateID(t *testing.T) {
	tests := []struct {
		name string
		id   string
		want string
	}{
		{"short", "abc", "abc"},
		{"exact 8", "abcdefgh", "abcdefgh"},
		{"longer than 8", "abcdefgh-1234-5678", "abcdefgh"},
		{"empty", "", ""},
		{"unicode", "日本語テスト文字列追加", "日本語テスト文字"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateID(tt.id)
			if got != tt.want {
				t.Errorf("truncateID(%q) = %q, want %q", tt.id, got, tt.want)
			}
		})
	}
}

func TestFormatAssignee(t *testing.T) {
	actors := actorDisplayLookup{
		state: &actorDisplayLookupState{
			members:       map[string]string{"abcdefgh-1234": "Alice"},
			agents:        map[string]string{"xyz": "CodeBot"},
			squads:        map[string]string{"sq-1": "Super Human"},
			membersLoaded: true,
			agentsLoaded:  true,
			squadsLoaded:  true,
		},
	}
	tests := []struct {
		name  string
		issue map[string]any
		want  string
	}{
		{"empty", map[string]any{}, ""},
		{"no type", map[string]any{"assignee_id": "abc"}, ""},
		{"no id", map[string]any{"assignee_type": "member"}, ""},
		{"member", map[string]any{"assignee_type": "member", "assignee_id": "abcdefgh-1234"}, "member:Alice"},
		{"agent", map[string]any{"assignee_type": "agent", "assignee_id": "xyz"}, "agent:CodeBot"},
		{"squad", map[string]any{"assignee_type": "squad", "assignee_id": "sq-1"}, "squad:Super Human"},
		{"unknown fallback", map[string]any{"assignee_type": "agent", "assignee_id": "missing"}, "agent:missing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatAssignee(tt.issue, actors)
			if got != tt.want {
				t.Errorf("formatAssignee() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestActorDisplayLookupLazyLoads(t *testing.T) {
	var memberCalls, agentCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/workspaces/ws-1/members":
			memberCalls++
			json.NewEncoder(w).Encode([]map[string]any{{"user_id": "user-1", "name": "Alice"}})
		case "/api/agents":
			agentCalls++
			json.NewEncoder(w).Encode([]map[string]any{{"id": "agent-1", "name": "CodeBot"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := cli.NewAPIClient(srv.URL, "ws-1", "test-token")
	lookup := loadActorDisplayLookup(context.Background(), client)

	if got := lookup.agent("agent-1"); got != "CodeBot" {
		t.Fatalf("agent() = %q, want CodeBot", got)
	}
	if memberCalls != 0 || agentCalls != 1 {
		t.Fatalf("after agent lookup: memberCalls=%d agentCalls=%d, want 0/1", memberCalls, agentCalls)
	}
	if got := lookup.actor("member", "user-1"); got != "member:Alice" {
		t.Fatalf("actor(member) = %q, want member:Alice", got)
	}
	if memberCalls != 1 || agentCalls != 1 {
		t.Fatalf("after member lookup: memberCalls=%d agentCalls=%d, want 1/1", memberCalls, agentCalls)
	}
	if got := lookup.actor("member", "missing"); got != "member:missing" {
		t.Fatalf("actor(missing member) = %q", got)
	}
	if memberCalls != 1 || agentCalls != 1 {
		t.Fatalf("lookup should cache per type: memberCalls=%d agentCalls=%d", memberCalls, agentCalls)
	}
}

func TestResolveIDByPrefix(t *testing.T) {
	client := cli.NewAPIClient("http://example.invalid", "ws-1", "test-token")
	ctx := context.Background()
	fetch := func(context.Context, *cli.APIClient) ([]idCandidate, error) {
		return []idCandidate{
			{ID: "aaaaaaaa-1111-2222-3333-444444444444", Display: "Alpha"},
			{ID: "bbbbbbbb-1111-2222-3333-444444444444", Display: "Beta"},
			{ID: "aaaabbbb-1111-2222-3333-444444444444", Display: "Alpha Two"},
		}, nil
	}

	t.Run("full UUID passes through", func(t *testing.T) {
		got, err := resolveIDByPrefix(ctx, client, "thing", "bbbbbbbb-1111-2222-3333-444444444444", fetch)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.ID != "bbbbbbbb-1111-2222-3333-444444444444" {
			t.Fatalf("got %q", got.ID)
		}
	})

	t.Run("unique short prefix resolves", func(t *testing.T) {
		got, err := resolveIDByPrefix(ctx, client, "thing", "bbbb", fetch)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.ID != "bbbbbbbb-1111-2222-3333-444444444444" || got.Display != "Beta" {
			t.Fatalf("got %#v", got)
		}
	})

	t.Run("short prefix can include dashes", func(t *testing.T) {
		got, err := resolveIDByPrefix(ctx, client, "thing", "bbbb-b", fetch)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.ID != "bbbbbbbb-1111-2222-3333-444444444444" {
			t.Fatalf("got %#v", got)
		}
	})

	t.Run("ambiguous prefix is rejected", func(t *testing.T) {
		_, err := resolveIDByPrefix(ctx, client, "thing", "aaaa", fetch)
		if err == nil || !strings.Contains(err.Error(), "ambiguous") {
			t.Fatalf("expected ambiguous error, got %v", err)
		}
		if strings.Contains(err.Error(), "Alpha") || strings.Contains(err.Error(), "Beta") {
			t.Fatalf("ambiguous error exposed candidate display/detail: %v", err)
		}
	})

	t.Run("too short prefix is rejected", func(t *testing.T) {
		if _, err := resolveIDByPrefix(ctx, client, "thing", "aaa", fetch); err == nil || !strings.Contains(err.Error(), "at least 4") {
			t.Fatalf("expected short prefix error, got %v", err)
		}
	})
}

func TestResolveIssueRef(t *testing.T) {
	issue := map[string]any{
		"id":         "1881a167-4bb6-4602-944b-f40ce4192fe6",
		"identifier": "MUL-1852",
		"title":      "Short ID bug",
	}

	t.Run("identifier is resolved before prefix lookup", func(t *testing.T) {
		listCalled := false
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/issues/MUL-1852":
				json.NewEncoder(w).Encode(issue)
			case "/api/issues":
				listCalled = true
				http.Error(w, "should not list", http.StatusTeapot)
			default:
				http.NotFound(w, r)
			}
		}))
		defer srv.Close()

		client := cli.NewAPIClient(srv.URL, "ws-1", "test-token")
		got, err := resolveIssueRef(context.Background(), client, "MUL-1852")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if listCalled {
			t.Fatal("identifier path should not call issue list")
		}
		if got.ID != issue["id"] || got.Display != "MUL-1852" {
			t.Fatalf("got %#v", got)
		}
	})

	t.Run("short UUID prefix resolves from workspace issue list", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/issues" {
				http.NotFound(w, r)
				return
			}
			if got := r.URL.Query().Get("workspace_id"); got != "ws-1" {
				t.Errorf("workspace_id = %q, want ws-1", got)
			}
			if got := r.URL.Query().Get("include_closed"); got != "true" {
				t.Errorf("include_closed = %q, want true", got)
			}
			if got := r.URL.Query().Get("limit"); got != strconv.Itoa(resolverListPageLimit) {
				t.Errorf("limit = %q, want %d", got, resolverListPageLimit)
			}
			json.NewEncoder(w).Encode(map[string]any{
				"issues": []map[string]any{issue},
				"total":  1,
			})
		}))
		defer srv.Close()

		client := cli.NewAPIClient(srv.URL, "ws-1", "test-token")
		got, err := resolveIssueRef(context.Background(), client, "1881")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.ID != issue["id"] || got.Display != "MUL-1852" {
			t.Fatalf("got %#v", got)
		}
	})

	t.Run("bare issue number is not resolved as issue number", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/issues" {
				http.NotFound(w, r)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{
				"issues": []map[string]any{{
					"id":         "aaaaaaaa-4bb6-4602-944b-f40ce4192fe6",
					"identifier": "MUL-1852",
					"title":      "Should not resolve by number",
				}},
				"total": 1,
			})
		}))
		defer srv.Close()

		client := cli.NewAPIClient(srv.URL, "ws-1", "test-token")
		_, err := resolveIssueRef(context.Background(), client, "1852")
		if err == nil {
			t.Fatal("expected bare number to be treated only as a UUID prefix")
		}
		if got := err.Error(); !strings.Contains(got, "id prefix") {
			t.Fatalf("expected prefix error, got: %s", got)
		}
	})
}

func TestFetchAutopilotCandidatesPaginates(t *testing.T) {
	page1 := make([]map[string]any, 0, resolverListPageLimit)
	for i := 0; i < resolverListPageLimit; i++ {
		page1 = append(page1, map[string]any{
			"id":     fmt.Sprintf("aaaaaaaa-0000-0000-0000-%012x", i),
			"title":  fmt.Sprintf("autopilot-%d", i),
			"status": "active",
		})
	}
	page2 := []map[string]any{{
		"id":     "bbbbbbbb-0000-0000-0000-000000000000",
		"title":  "final autopilot",
		"status": "paused",
	}}

	var offsets []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/autopilots" {
			http.NotFound(w, r)
			return
		}
		if got := r.URL.Query().Get("workspace_id"); got != "ws-1" {
			t.Errorf("workspace_id = %q, want ws-1", got)
		}
		if got := r.URL.Query().Get("limit"); got != strconv.Itoa(resolverListPageLimit) {
			t.Errorf("limit = %q, want %d", got, resolverListPageLimit)
		}
		offset := r.URL.Query().Get("offset")
		offsets = append(offsets, offset)
		switch offset {
		case "":
			json.NewEncoder(w).Encode(map[string]any{
				"autopilots": page1,
				"total":      resolverListPageLimit + 1,
			})
		case strconv.Itoa(resolverListPageLimit):
			json.NewEncoder(w).Encode(map[string]any{
				"autopilots": page2,
				"total":      resolverListPageLimit + 1,
			})
		default:
			t.Fatalf("unexpected offset %q", offset)
		}
	}))
	defer srv.Close()

	client := cli.NewAPIClient(srv.URL, "ws-1", "test-token")
	got, err := fetchAutopilotCandidates(context.Background(), client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != resolverListPageLimit+1 {
		t.Fatalf("got %d candidates, want %d", len(got), resolverListPageLimit+1)
	}
	if len(offsets) != 2 || offsets[0] != "" || offsets[1] != strconv.Itoa(resolverListPageLimit) {
		t.Fatalf("offsets = %#v, want [\"\", %q]", offsets, strconv.Itoa(resolverListPageLimit))
	}
	if got[len(got)-1].ID != "bbbbbbbb-0000-0000-0000-000000000000" {
		t.Fatalf("last candidate = %#v", got[len(got)-1])
	}
}

func TestResolveTaskRunID(t *testing.T) {
	issueID := "1881a167-4bb6-4602-944b-f40ce4192fe6"
	taskID := "abcd1234-0000-0000-0000-000000000000"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/issues/" + issueID + "/task-runs":
			json.NewEncoder(w).Encode([]map[string]any{{
				"id":       taskID,
				"agent_id": "agent-1",
				"status":   "completed",
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := cli.NewAPIClient(srv.URL, "ws-1", "test-token")
	got, err := resolveTaskRunID(context.Background(), client, issueID, "abcd")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != taskID {
		t.Fatalf("got %#v, want task id %s", got, taskID)
	}

	_, err = resolveTaskRunID(context.Background(), client, "", "abcd")
	if err == nil || !strings.Contains(err.Error(), "--issue") {
		t.Fatalf("expected missing --issue error for short prefix, got %v", err)
	}
}

func TestRunIssueRunMessagesResolvesShortTaskPrefix(t *testing.T) {
	issueID := "1881a167-4bb6-4602-944b-f40ce4192fe6"
	taskID := "abcd1234-0000-0000-0000-000000000000"
	var messagePath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/issues/MUL-1852":
			json.NewEncoder(w).Encode(map[string]any{
				"id":         issueID,
				"identifier": "MUL-1852",
			})
		case "/api/issues/" + issueID + "/task-runs":
			json.NewEncoder(w).Encode([]map[string]any{{"id": taskID}})
		case "/api/tasks/" + taskID + "/messages":
			messagePath = r.URL.Path
			json.NewEncoder(w).Encode([]map[string]any{{
				"seq":     1,
				"type":    "text",
				"content": "done",
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := &cobra.Command{Use: "run-messages"}
	cmd.Flags().String("output", "json", "")
	cmd.Flags().Int("since", 0, "")
	cmd.Flags().String("issue", "", "")
	_ = cmd.Flags().Set("issue", "MUL-1852")
	if err := runIssueRunMessages(cmd, []string{"abcd"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if messagePath != "/api/tasks/"+taskID+"/messages" {
		t.Fatalf("message path = %q, want user-facing task messages path", messagePath)
	}
}

func TestResolveAssignee(t *testing.T) {
	membersResp := []map[string]any{
		{"user_id": "user-1111", "name": "Alice Smith"},
		{"user_id": "user-2222", "name": "Bob Jones"},
	}
	agentsResp := []map[string]any{
		{"id": "agent-3333", "name": "CodeBot"},
	}
	squadsResp := []map[string]any{
		{"id": "squad-4444", "name": "Super Human"},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/workspaces/ws-1/members":
			json.NewEncoder(w).Encode(membersResp)
		case "/api/agents":
			json.NewEncoder(w).Encode(agentsResp)
		case "/api/squads":
			json.NewEncoder(w).Encode(squadsResp)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := cli.NewAPIClient(srv.URL, "ws-1", "test-token")
	ctx := context.Background()

	t.Run("exact match member", func(t *testing.T) {
		aType, aID, err := resolveAssignee(ctx, client, "Alice Smith", issueAssigneeKinds)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if aType != "member" || aID != "user-1111" {
			t.Errorf("got (%q, %q), want (member, user-1111)", aType, aID)
		}
	})

	t.Run("case-insensitive substring", func(t *testing.T) {
		aType, aID, err := resolveAssignee(ctx, client, "bob", issueAssigneeKinds)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if aType != "member" || aID != "user-2222" {
			t.Errorf("got (%q, %q), want (member, user-2222)", aType, aID)
		}
	})

	t.Run("match agent", func(t *testing.T) {
		aType, aID, err := resolveAssignee(ctx, client, "codebot", issueAssigneeKinds)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if aType != "agent" || aID != "agent-3333" {
			t.Errorf("got (%q, %q), want (agent, agent-3333)", aType, aID)
		}
	})

	// MUL-2165: squad names must resolve to (squad, <id>) so the autopilot
	// quick-create prompt can route work to a squad (e.g. "Super Human")
	// instead of falling through to "Unrecognized assignee".
	t.Run("match squad by exact name", func(t *testing.T) {
		aType, aID, err := resolveAssignee(ctx, client, "Super Human", issueAssigneeKinds)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if aType != "squad" || aID != "squad-4444" {
			t.Errorf("got (%q, %q), want (squad, squad-4444)", aType, aID)
		}
	})

	t.Run("match squad by case-insensitive substring", func(t *testing.T) {
		aType, aID, err := resolveAssignee(ctx, client, "super", issueAssigneeKinds)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if aType != "squad" || aID != "squad-4444" {
			t.Errorf("got (%q, %q), want (squad, squad-4444)", aType, aID)
		}
	})

	t.Run("match squad by bare @ display name", func(t *testing.T) {
		aType, aID, err := resolveAssignee(ctx, client, "@Super Human", issueAssigneeKinds)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if aType != "squad" || aID != "squad-4444" {
			t.Errorf("got (%q, %q), want (squad, squad-4444)", aType, aID)
		}
	})

	t.Run("no match", func(t *testing.T) {
		_, _, err := resolveAssignee(ctx, client, "nobody", issueAssigneeKinds)
		if err == nil {
			t.Fatal("expected error for no match")
		}
	})

	t.Run("ambiguous", func(t *testing.T) {
		// Both "Alice Smith" and "Bob Jones" contain a space — but let's use a broader query
		// "e" matches "Alice Smith" and "Bob Jones" and "CodeBot"
		_, _, err := resolveAssignee(ctx, client, "o", issueAssigneeKinds)
		if err == nil {
			t.Fatal("expected error for ambiguous match")
		}
		if got := err.Error(); !strings.Contains(got, "ambiguous") {
			t.Errorf("expected ambiguous error, got: %s", got)
		}
	})

	t.Run("missing workspace ID", func(t *testing.T) {
		noWSClient := cli.NewAPIClient(srv.URL, "", "test-token")
		_, _, err := resolveAssignee(ctx, noWSClient, "alice", issueAssigneeKinds)
		if err == nil {
			t.Fatal("expected error for missing workspace ID")
		}
	})
}

func TestNormalizeAssigneeLookupInput(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain name", "Super Human", "Super Human"},
		{"bare at name", "@Super Human", "Super Human"},
		{"fullwidth at name", "＠独立团", "独立团"},
		{"spaced at name", "  @  Super Human  ", "Super Human"},
		{
			name: "mention link",
			in:   "[@Super Human](mention://squad/ccccccc1-2222-3333-4444-555555555555)",
			want: "ccccccc1-2222-3333-4444-555555555555",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeAssigneeLookupInput(tt.in); got != tt.want {
				t.Errorf("normalizeAssigneeLookupInput(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestResolveAssigneeRespectsKinds covers the MUL-2165 follow-up: callers
// whose target schema is member-or-agent-only (project.lead_type DB CHECK
// at server/migrations/034_projects.up.sql:10, and the subscriber handler's
// isWorkspaceEntity switch at server/internal/handler/handler.go:414) must
// be able to opt out of squad resolution. Without this, "--lead <SquadName>"
// would return (squad, ...) and the request would 500/403 server-side
// instead of failing with a clean CLI-side resolution error.
func TestResolveAssigneeRespectsKinds(t *testing.T) {
	membersResp := []map[string]any{
		{"user_id": "user-1111", "name": "Alice"},
	}
	agentsResp := []map[string]any{
		{"id": "agent-3333", "name": "CodeBot"},
	}
	squadsResp := []map[string]any{
		{"id": "ccccccc1-2222-3333-4444-555555555555", "name": "Super Human"},
	}

	var squadsHits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/workspaces/ws-1/members":
			json.NewEncoder(w).Encode(membersResp)
		case "/api/agents":
			json.NewEncoder(w).Encode(agentsResp)
		case "/api/squads":
			squadsHits++
			json.NewEncoder(w).Encode(squadsResp)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := cli.NewAPIClient(srv.URL, "ws-1", "test-token")
	ctx := context.Background()

	t.Run("memberOrAgentKinds skips the /api/squads fetch entirely", func(t *testing.T) {
		before := squadsHits
		_, _, _ = resolveAssignee(ctx, client, "Alice", memberOrAgentKinds)
		if squadsHits != before {
			t.Errorf("expected memberOrAgentKinds to skip /api/squads, but it was called %d time(s)", squadsHits-before)
		}
	})

	t.Run("memberOrAgentKinds rejects a squad name with a member-or-agent-only error", func(t *testing.T) {
		_, _, err := resolveAssignee(ctx, client, "Super Human", memberOrAgentKinds)
		if err == nil {
			t.Fatal("expected resolution error for squad name under memberOrAgentKinds")
		}
		if !strings.Contains(err.Error(), "no member or agent") {
			t.Errorf("expected member-or-agent error wording, got: %v", err)
		}
		if strings.Contains(err.Error(), "squad") {
			t.Errorf("error must not mention squad when squads are not allowed, got: %v", err)
		}
	})

	t.Run("memberOrAgentKinds rejects a squad UUID via the strict resolver", func(t *testing.T) {
		_, _, err := resolveAssigneeByID(ctx, client, "ccccccc1-2222-3333-4444-555555555555", memberOrAgentKinds)
		if err == nil {
			t.Fatal("expected not-found error for squad UUID under memberOrAgentKinds")
		}
		if !strings.Contains(err.Error(), "no member or agent") {
			t.Errorf("expected member-or-agent error wording, got: %v", err)
		}
	})

	t.Run("issueAssigneeKinds still resolves the same squad name (control)", func(t *testing.T) {
		aType, aID, err := resolveAssignee(ctx, client, "Super Human", issueAssigneeKinds)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if aType != "squad" || aID != "ccccccc1-2222-3333-4444-555555555555" {
			t.Errorf("got (%q, %q), want (squad, ccccccc1-...)", aType, aID)
		}
	})
}

// TestResolveAssigneeExactMatchWins covers the substring-collision scenario from
// multica-ai/multica#1620: when one name is a substring of another (e.g.
// "reviewer" vs "peer-reviewer"), an exact match on the shorter name must
// short-circuit substring matching instead of erroring out as ambiguous.
func TestResolveAssigneeExactMatchWins(t *testing.T) {
	agentsResp := []map[string]any{
		{"id": "f656eab8-1111-1111-1111-111111111111", "name": "reviewer"},
		{"id": "9b0ff9a2-2222-2222-2222-222222222222", "name": "peer-reviewer"},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/workspaces/ws-1/members":
			json.NewEncoder(w).Encode([]map[string]any{})
		case "/api/agents":
			json.NewEncoder(w).Encode(agentsResp)
		case "/api/squads":
			json.NewEncoder(w).Encode([]map[string]any{})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := cli.NewAPIClient(srv.URL, "ws-1", "test-token")
	ctx := context.Background()

	t.Run("exact shorter name resolves to shorter agent", func(t *testing.T) {
		aType, aID, err := resolveAssignee(ctx, client, "reviewer", issueAssigneeKinds)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if aType != "agent" || aID != "f656eab8-1111-1111-1111-111111111111" {
			t.Errorf("got (%q, %q), want (agent, f656eab8-...)", aType, aID)
		}
	})

	t.Run("exact longer name still resolves unambiguously", func(t *testing.T) {
		aType, aID, err := resolveAssignee(ctx, client, "peer-reviewer", issueAssigneeKinds)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if aType != "agent" || aID != "9b0ff9a2-2222-2222-2222-222222222222" {
			t.Errorf("got (%q, %q), want (agent, 9b0ff9a2-...)", aType, aID)
		}
	})

	t.Run("exact match is case-insensitive and tolerates whitespace", func(t *testing.T) {
		aType, aID, err := resolveAssignee(ctx, client, "  Reviewer  ", issueAssigneeKinds)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if aType != "agent" || aID != "f656eab8-1111-1111-1111-111111111111" {
			t.Errorf("got (%q, %q), want exact reviewer agent", aType, aID)
		}
	})

	t.Run("substring-only input falls back and stays ambiguous", func(t *testing.T) {
		// "review" matches both agents via substring and neither via exact name,
		// so the existing ambiguity error is preserved.
		_, _, err := resolveAssignee(ctx, client, "review", issueAssigneeKinds)
		if err == nil {
			t.Fatal("expected error for ambiguous substring match")
		}
		if got := err.Error(); !strings.Contains(got, "ambiguous") {
			t.Errorf("expected ambiguous error, got: %s", got)
		}
	})
}

// TestResolveAssigneeByID covers the ID/ShortID escape hatch from
// multica-ai/multica#1620: passing a full UUID or its 8-char prefix must
// resolve directly without going through name matching.
func TestResolveAssigneeByID(t *testing.T) {
	membersResp := []map[string]any{
		{"user_id": "aaaaaaaa-1111-1111-1111-111111111111", "name": "Alice"},
	}
	agentsResp := []map[string]any{
		{"id": "f656eab8-1111-1111-1111-111111111111", "name": "reviewer"},
		{"id": "9b0ff9a2-2222-2222-2222-222222222222", "name": "peer-reviewer"},
	}
	squadsResp := []map[string]any{
		{"id": "ccccccc1-2222-3333-4444-555555555555", "name": "Super Human"},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/workspaces/ws-1/members":
			json.NewEncoder(w).Encode(membersResp)
		case "/api/agents":
			json.NewEncoder(w).Encode(agentsResp)
		case "/api/squads":
			json.NewEncoder(w).Encode(squadsResp)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := cli.NewAPIClient(srv.URL, "ws-1", "test-token")
	ctx := context.Background()

	t.Run("full UUID resolves agent", func(t *testing.T) {
		aType, aID, err := resolveAssignee(ctx, client, "f656eab8-1111-1111-1111-111111111111", issueAssigneeKinds)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if aType != "agent" || aID != "f656eab8-1111-1111-1111-111111111111" {
			t.Errorf("got (%q, %q), want reviewer agent", aType, aID)
		}
	})

	t.Run("8-char ShortID resolves agent", func(t *testing.T) {
		aType, aID, err := resolveAssignee(ctx, client, "f656eab8", issueAssigneeKinds)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if aType != "agent" || aID != "f656eab8-1111-1111-1111-111111111111" {
			t.Errorf("got (%q, %q), want reviewer agent", aType, aID)
		}
	})

	t.Run("uppercase ShortID still resolves", func(t *testing.T) {
		aType, aID, err := resolveAssignee(ctx, client, "F656EAB8", issueAssigneeKinds)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if aType != "agent" || aID != "f656eab8-1111-1111-1111-111111111111" {
			t.Errorf("got (%q, %q), want reviewer agent", aType, aID)
		}
	})

	t.Run("ShortID resolves a member", func(t *testing.T) {
		aType, aID, err := resolveAssignee(ctx, client, "aaaaaaaa", issueAssigneeKinds)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if aType != "member" || aID != "aaaaaaaa-1111-1111-1111-111111111111" {
			t.Errorf("got (%q, %q), want Alice", aType, aID)
		}
	})
}

// TestResolveAssigneeByIDStrict covers the strict UUID resolver that backs
// --assignee-id / --to-id / --user-id. Unlike resolveAssignee it must reject
// non-UUID inputs (no name fallback) and surface a clear error when the UUID
// is well-formed but not present in the workspace.
func TestResolveAssigneeByIDStrict(t *testing.T) {
	membersResp := []map[string]any{
		{"user_id": "aaaaaaaa-1111-1111-1111-111111111111", "name": "Alice"},
	}
	agentsResp := []map[string]any{
		{"id": "5fb87ac7-23b5-4a7a-81fa-ed295a54545d", "name": "J"},
		{"id": "192b9cca-2222-2222-2222-222222222222", "name": "Open Claw - J"},
	}
	squadsResp := []map[string]any{
		{"id": "ccccccc1-2222-3333-4444-555555555555", "name": "Super Human"},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/workspaces/ws-1/members":
			json.NewEncoder(w).Encode(membersResp)
		case "/api/agents":
			json.NewEncoder(w).Encode(agentsResp)
		case "/api/squads":
			json.NewEncoder(w).Encode(squadsResp)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := cli.NewAPIClient(srv.URL, "ws-1", "test-token")
	ctx := context.Background()

	t.Run("full UUID resolves the right agent in a substring-collision workspace", func(t *testing.T) {
		// This is the MUL-1254 scenario: agent "J" is unreachable by name
		// because every other agent has "J" in it. UUID lookup must
		// deterministically pick the right one.
		aType, aID, err := resolveAssigneeByID(ctx, client, "5fb87ac7-23b5-4a7a-81fa-ed295a54545d", issueAssigneeKinds)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if aType != "agent" || aID != "5fb87ac7-23b5-4a7a-81fa-ed295a54545d" {
			t.Errorf("got (%q, %q), want agent J", aType, aID)
		}
	})

	t.Run("uppercase UUID is normalized", func(t *testing.T) {
		aType, aID, err := resolveAssigneeByID(ctx, client, "5FB87AC7-23B5-4A7A-81FA-ED295A54545D", issueAssigneeKinds)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if aType != "agent" || aID != "5fb87ac7-23b5-4a7a-81fa-ed295a54545d" {
			t.Errorf("got (%q, %q), want agent J", aType, aID)
		}
	})

	t.Run("UUID resolves a member", func(t *testing.T) {
		aType, aID, err := resolveAssigneeByID(ctx, client, "aaaaaaaa-1111-1111-1111-111111111111", issueAssigneeKinds)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if aType != "member" || aID != "aaaaaaaa-1111-1111-1111-111111111111" {
			t.Errorf("got (%q, %q), want Alice", aType, aID)
		}
	})

	// MUL-2165: --assignee-id <squad-uuid> must resolve to (squad, <id>) so
	// scripts that read the squad list and pin its UUID can assign work to a
	// squad in a single deterministic call.
	t.Run("UUID resolves a squad", func(t *testing.T) {
		aType, aID, err := resolveAssigneeByID(ctx, client, "ccccccc1-2222-3333-4444-555555555555", issueAssigneeKinds)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if aType != "squad" || aID != "ccccccc1-2222-3333-4444-555555555555" {
			t.Errorf("got (%q, %q), want squad Super Human", aType, aID)
		}
	})

	t.Run("non-UUID input is rejected without name fallback", func(t *testing.T) {
		_, _, err := resolveAssigneeByID(ctx, client, "Alice", issueAssigneeKinds)
		if err == nil {
			t.Fatal("expected error for non-UUID input")
		}
		if !strings.Contains(err.Error(), "UUID") {
			t.Errorf("expected UUID error, got: %v", err)
		}
	})

	t.Run("UUID prefix (ShortID) is rejected — strict mode requires canonical form", func(t *testing.T) {
		_, _, err := resolveAssigneeByID(ctx, client, "5fb87ac7", issueAssigneeKinds)
		if err == nil {
			t.Fatal("expected error for ShortID")
		}
	})

	t.Run("well-formed UUID with no matching entity errors", func(t *testing.T) {
		_, _, err := resolveAssigneeByID(ctx, client, "deadbeef-1111-1111-1111-111111111111", issueAssigneeKinds)
		if err == nil {
			t.Fatal("expected error for missing entity")
		}
		if !strings.Contains(err.Error(), "no member, agent, or squad") {
			t.Errorf("expected not-found error, got: %v", err)
		}
	})

	t.Run("missing workspace ID", func(t *testing.T) {
		noWSClient := cli.NewAPIClient(srv.URL, "", "test-token")
		_, _, err := resolveAssigneeByID(ctx, noWSClient, "5fb87ac7-23b5-4a7a-81fa-ed295a54545d", issueAssigneeKinds)
		if err == nil {
			t.Fatal("expected error for missing workspace ID")
		}
	})
}

// TestPickAssigneeFromFlags covers the flag-pair picker that backs every
// assignee-taking command. The mutual-exclusion guard is the load-bearing
// piece — silently preferring one side would let a buggy script set both
// flags and assign the wrong entity.
func TestPickAssigneeFromFlags(t *testing.T) {
	membersResp := []map[string]any{
		{"user_id": "aaaaaaaa-1111-1111-1111-111111111111", "name": "Alice"},
	}
	agentsResp := []map[string]any{
		{"id": "5fb87ac7-23b5-4a7a-81fa-ed295a54545d", "name": "J"},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/workspaces/ws-1/members":
			json.NewEncoder(w).Encode(membersResp)
		case "/api/agents":
			json.NewEncoder(w).Encode(agentsResp)
		case "/api/squads":
			json.NewEncoder(w).Encode([]map[string]any{})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := cli.NewAPIClient(srv.URL, "ws-1", "test-token")
	ctx := context.Background()

	newCmd := func() *cobra.Command {
		c := &cobra.Command{Use: "test"}
		c.Flags().String("assignee", "", "")
		c.Flags().String("assignee-id", "", "")
		return c
	}

	t.Run("neither flag set returns hasValue=false", func(t *testing.T) {
		_, _, has, err := pickAssigneeFromFlags(ctx, client, newCmd(), "assignee", "assignee-id", issueAssigneeKinds)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if has {
			t.Errorf("expected hasValue=false")
		}
	})

	t.Run("name flag uses fuzzy resolver", func(t *testing.T) {
		c := newCmd()
		_ = c.Flags().Set("assignee", "Alice")
		typ, id, has, err := pickAssigneeFromFlags(ctx, client, c, "assignee", "assignee-id", issueAssigneeKinds)
		if err != nil || !has || typ != "member" || id != "aaaaaaaa-1111-1111-1111-111111111111" {
			t.Errorf("got (%q, %q, %v, %v), want Alice", typ, id, has, err)
		}
	})

	t.Run("id flag uses strict resolver", func(t *testing.T) {
		c := newCmd()
		_ = c.Flags().Set("assignee-id", "5fb87ac7-23b5-4a7a-81fa-ed295a54545d")
		typ, id, has, err := pickAssigneeFromFlags(ctx, client, c, "assignee", "assignee-id", issueAssigneeKinds)
		if err != nil || !has || typ != "agent" || id != "5fb87ac7-23b5-4a7a-81fa-ed295a54545d" {
			t.Errorf("got (%q, %q, %v, %v), want agent J", typ, id, has, err)
		}
	})

	t.Run("both flags set is rejected", func(t *testing.T) {
		c := newCmd()
		_ = c.Flags().Set("assignee", "Alice")
		_ = c.Flags().Set("assignee-id", "5fb87ac7-23b5-4a7a-81fa-ed295a54545d")
		_, _, _, err := pickAssigneeFromFlags(ctx, client, c, "assignee", "assignee-id", issueAssigneeKinds)
		if err == nil {
			t.Fatal("expected mutually-exclusive error")
		}
		if !strings.Contains(err.Error(), "mutually exclusive") {
			t.Errorf("expected mutually-exclusive error, got: %v", err)
		}
	})

	// Explicit-empty regression: a script that interpolates an empty env var
	// into `--assignee-id "$MAYBE_UUID"` must NOT silently route through the
	// "no flag set" branch — that would defeat the whole point of the strict
	// UUID flag (issue list returning everything, create leaving the issue
	// unassigned, subscriber add subscribing the caller). Detection is via
	// Flags().Changed, so an explicit empty string surfaces as a UUID error.
	t.Run("explicit empty --assignee-id surfaces as UUID error, not silent skip", func(t *testing.T) {
		c := newCmd()
		_ = c.Flags().Set("assignee-id", "")
		_, _, has, err := pickAssigneeFromFlags(ctx, client, c, "assignee", "assignee-id", issueAssigneeKinds)
		if err == nil {
			t.Fatal("expected UUID error for explicit empty assignee-id")
		}
		if !has {
			t.Errorf("expected hasValue=true so caller treats this as a real attempt, not a no-op")
		}
		if !strings.Contains(err.Error(), "UUID") {
			t.Errorf("expected UUID-shaped error, got: %v", err)
		}
	})

	t.Run("explicit empty --assignee surfaces as not-found, not silent skip", func(t *testing.T) {
		c := newCmd()
		_ = c.Flags().Set("assignee", "")
		_, _, has, err := pickAssigneeFromFlags(ctx, client, c, "assignee", "assignee-id", issueAssigneeKinds)
		if err == nil {
			t.Fatal("expected resolver error for explicit empty assignee")
		}
		if !has {
			t.Errorf("expected hasValue=true so caller treats this as a real attempt, not a no-op")
		}
	})

	t.Run("explicit empty on both flags is mutually exclusive (set wins over value)", func(t *testing.T) {
		c := newCmd()
		_ = c.Flags().Set("assignee", "")
		_ = c.Flags().Set("assignee-id", "")
		_, _, _, err := pickAssigneeFromFlags(ctx, client, c, "assignee", "assignee-id", issueAssigneeKinds)
		if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
			t.Errorf("expected mutually-exclusive error, got: %v", err)
		}
	})
}

// TestPickAssigneeFromFlagsMemberOrAgentKinds is the call-site regression
// for the MUL-2165 follow-up. Subscriber add/remove and project lead pass
// memberOrAgentKinds because their target schema rejects squads
// (subscriber: server/internal/handler/handler.go:414;
// project: server/migrations/034_projects.up.sql:10). Without this gating,
// `multica issue subscriber add --user "<SquadName>"` or
// `multica project create --lead "<SquadName>"` would resolve to
// (squad, ...) and surface as a 500/403 server-side instead of a clean
// CLI-side resolution error.
func TestPickAssigneeFromFlagsMemberOrAgentKinds(t *testing.T) {
	membersResp := []map[string]any{
		{"user_id": "aaaaaaaa-1111-1111-1111-111111111111", "name": "Alice"},
	}
	agentsResp := []map[string]any{
		{"id": "5fb87ac7-23b5-4a7a-81fa-ed295a54545d", "name": "J"},
	}
	squadsResp := []map[string]any{
		{"id": "ccccccc1-2222-3333-4444-555555555555", "name": "Super Human"},
	}

	var squadsHits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/workspaces/ws-1/members":
			json.NewEncoder(w).Encode(membersResp)
		case "/api/agents":
			json.NewEncoder(w).Encode(agentsResp)
		case "/api/squads":
			squadsHits++
			json.NewEncoder(w).Encode(squadsResp)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := cli.NewAPIClient(srv.URL, "ws-1", "test-token")
	ctx := context.Background()

	newCmd := func(nameFlag, idFlag string) *cobra.Command {
		c := &cobra.Command{Use: "test"}
		c.Flags().String(nameFlag, "", "")
		c.Flags().String(idFlag, "", "")
		return c
	}

	t.Run("subscriber --user with a squad name is rejected without hitting /api/squads", func(t *testing.T) {
		before := squadsHits
		c := newCmd("user", "user-id")
		_ = c.Flags().Set("user", "Super Human")
		_, _, _, err := pickAssigneeFromFlags(ctx, client, c, "user", "user-id", memberOrAgentKinds)
		if err == nil {
			t.Fatal("expected resolution error for squad name under memberOrAgentKinds")
		}
		if !strings.Contains(err.Error(), "no member or agent") {
			t.Errorf("expected member-or-agent error wording, got: %v", err)
		}
		if squadsHits != before {
			t.Errorf("memberOrAgentKinds must NOT fetch /api/squads, but it was called %d time(s)", squadsHits-before)
		}
	})

	t.Run("subscriber --user-id with a squad UUID is rejected", func(t *testing.T) {
		c := newCmd("user", "user-id")
		_ = c.Flags().Set("user-id", "ccccccc1-2222-3333-4444-555555555555")
		_, _, _, err := pickAssigneeFromFlags(ctx, client, c, "user", "user-id", memberOrAgentKinds)
		if err == nil {
			t.Fatal("expected not-found error for squad UUID under memberOrAgentKinds")
		}
		if !strings.Contains(err.Error(), "no member or agent") {
			t.Errorf("expected member-or-agent error wording, got: %v", err)
		}
	})

	t.Run("project --lead with a member name still resolves cleanly", func(t *testing.T) {
		c := newCmd("lead", "lead-id")
		_ = c.Flags().Set("lead", "Alice")
		typ, id, has, err := pickAssigneeFromFlags(ctx, client, c, "lead", "lead-id", memberOrAgentKinds)
		if err != nil || !has || typ != "member" || id != "aaaaaaaa-1111-1111-1111-111111111111" {
			t.Errorf("got (%q, %q, %v, %v), want member Alice", typ, id, has, err)
		}
	})

	t.Run("project --lead with an agent name still resolves cleanly", func(t *testing.T) {
		c := newCmd("lead", "lead-id")
		_ = c.Flags().Set("lead", "J")
		typ, id, has, err := pickAssigneeFromFlags(ctx, client, c, "lead", "lead-id", memberOrAgentKinds)
		if err != nil || !has || typ != "agent" || id != "5fb87ac7-23b5-4a7a-81fa-ed295a54545d" {
			t.Errorf("got (%q, %q, %v, %v), want agent J", typ, id, has, err)
		}
	})
}

func TestIssueSubscriberList(t *testing.T) {
	subscribersResp := []map[string]any{
		{
			"issue_id":   "issue-1",
			"user_type":  "member",
			"user_id":    "user-1111",
			"reason":     "creator",
			"created_at": "2026-04-01T10:00:00Z",
		},
		{
			"issue_id":   "issue-1",
			"user_type":  "agent",
			"user_id":    "agent-3333",
			"reason":     "manual",
			"created_at": "2026-04-01T11:00:00Z",
		},
	}

	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		json.NewEncoder(w).Encode(subscribersResp)
	}))
	defer srv.Close()

	client := cli.NewAPIClient(srv.URL, "ws-1", "test-token")
	ctx := context.Background()

	var got []map[string]any
	if err := client.GetJSON(ctx, "/api/issues/issue-1/subscribers", &got); err != nil {
		t.Fatalf("GetJSON: %v", err)
	}
	if gotPath != "/api/issues/issue-1/subscribers" {
		t.Errorf("unexpected path: %s", gotPath)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 subscribers, got %d", len(got))
	}
	if got[0]["user_type"] != "member" || got[1]["user_type"] != "agent" {
		t.Errorf("unexpected subscriber ordering: %+v", got)
	}
}

func TestIssueSubscriberMutationBody(t *testing.T) {
	tests := []struct {
		name     string
		action   string
		user     string
		members  []map[string]any
		agents   []map[string]any
		wantPath string
		wantBody map[string]any
	}{
		{
			name:     "subscribe caller (no user flag)",
			action:   "subscribe",
			user:     "",
			wantPath: "/api/issues/issue-1/subscribe",
			wantBody: map[string]any{},
		},
		{
			name:     "unsubscribe caller",
			action:   "unsubscribe",
			user:     "",
			wantPath: "/api/issues/issue-1/unsubscribe",
			wantBody: map[string]any{},
		},
		{
			name:     "subscribe a member by name",
			action:   "subscribe",
			user:     "alice",
			members:  []map[string]any{{"user_id": "user-1111", "name": "Alice Smith"}},
			wantPath: "/api/issues/issue-1/subscribe",
			wantBody: map[string]any{"user_type": "member", "user_id": "user-1111"},
		},
		{
			name:     "subscribe an agent by name",
			action:   "subscribe",
			user:     "codebot",
			agents:   []map[string]any{{"id": "agent-3333", "name": "CodeBot"}},
			wantPath: "/api/issues/issue-1/subscribe",
			wantBody: map[string]any{"user_type": "agent", "user_id": "agent-3333"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotPath string
			var gotBody map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/workspaces/ws-1/members":
					json.NewEncoder(w).Encode(tt.members)
					return
				case "/api/agents":
					json.NewEncoder(w).Encode(tt.agents)
					return
				case "/api/squads":
					json.NewEncoder(w).Encode([]map[string]any{})
					return
				}
				gotPath = r.URL.Path
				if r.Method != http.MethodPost {
					t.Errorf("expected POST, got %s", r.Method)
				}
				json.NewDecoder(r.Body).Decode(&gotBody)
				json.NewEncoder(w).Encode(map[string]bool{"subscribed": tt.action == "subscribe"})
			}))
			defer srv.Close()

			client := cli.NewAPIClient(srv.URL, "ws-1", "test-token")
			ctx := context.Background()

			body := map[string]any{}
			if tt.user != "" {
				uType, uID, err := resolveAssignee(ctx, client, tt.user, issueAssigneeKinds)
				if err != nil {
					t.Fatalf("resolveAssignee: %v", err)
				}
				body["user_type"] = uType
				body["user_id"] = uID
			}

			var result map[string]any
			path := "/api/issues/issue-1/" + tt.action
			if err := client.PostJSON(ctx, path, body, &result); err != nil {
				t.Fatalf("PostJSON: %v", err)
			}

			if gotPath != tt.wantPath {
				t.Errorf("path = %q, want %q", gotPath, tt.wantPath)
			}
			for k, want := range tt.wantBody {
				if gotBody[k] != want {
					t.Errorf("body[%q] = %v, want %v", k, gotBody[k], want)
				}
			}
			if len(tt.wantBody) == 0 && len(gotBody) != 0 {
				t.Errorf("expected empty body, got %+v", gotBody)
			}
		})
	}
}

func TestValidIssueStatuses(t *testing.T) {
	expected := map[string]bool{
		"backlog":     true,
		"todo":        true,
		"in_progress": true,
		"in_review":   true,
		"done":        true,
		"blocked":     true,
		"cancelled":   true,
	}
	for _, s := range validIssueStatuses {
		if !expected[s] {
			t.Errorf("unexpected status in validIssueStatuses: %q", s)
		}
	}
	if len(validIssueStatuses) != len(expected) {
		t.Errorf("validIssueStatuses has %d entries, expected %d", len(validIssueStatuses), len(expected))
	}
}

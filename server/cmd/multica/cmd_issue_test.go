package main

import (
	"context"
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

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// stderrCapture redirects os.Stderr through a pipe so a test can assert on
// the human-facing strings runIssueCommentList prints alongside its JSON
// output. Read() drains the pipe and is safe to call multiple times.
type stderrCapture struct {
	t       *testing.T
	orig    *os.File
	r, w    *os.File
	out     strings.Builder
	doneCh  chan struct{}
	stopped bool
}

func captureStderr(t *testing.T) *stderrCapture {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	c := &stderrCapture{t: t, orig: os.Stderr, r: r, w: w, doneCh: make(chan struct{})}
	os.Stderr = w
	go func() {
		buf, _ := io.ReadAll(r)
		c.out.Write(buf)
		close(c.doneCh)
	}()
	return c
}

func (c *stderrCapture) restore() {
	if c.stopped {
		return
	}
	c.stopped = true
	os.Stderr = c.orig
	_ = c.w.Close()
	<-c.doneCh
	_ = c.r.Close()
}

func (c *stderrCapture) read() string {
	c.restore()
	return c.out.String()
}

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
	cmd.Flags().StringSlice("attachment-id", nil, "")
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

func TestRunIssueCreateSendsExistingAttachmentIDs(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/issues" {
			http.NotFound(w, r)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"id":         "issue-1",
			"identifier": "MUL-1",
			"title":      "With attachments",
			"status":     "todo",
			"priority":   "none",
		})
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")
	t.Setenv("MULTICA_QUICK_CREATE_ATTACHMENT_IDS", `["att-env","att-shared"]`)

	cmd := newIssueCreateTestCmd()
	_ = cmd.Flags().Set("title", "With attachments")
	_ = cmd.Flags().Set("attachment-id", "att-flag")
	_ = cmd.Flags().Set("attachment-id", "att-shared")
	if err := runIssueCreate(cmd, nil); err != nil {
		t.Fatalf("runIssueCreate: %v", err)
	}

	got, ok := body["attachment_ids"].([]any)
	if !ok {
		t.Fatalf("attachment_ids = %#v, want JSON array", body["attachment_ids"])
	}
	want := []string{"att-flag", "att-shared", "att-env"}
	if len(got) != len(want) {
		t.Fatalf("attachment_ids length = %d, want %d (%#v)", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Fatalf("attachment_ids[%d] = %#v, want %q (all=%#v)", i, got[i], w, got)
		}
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

func newIssuePullRequestsTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "pull-requests"}
	cmd.Flags().String("output", "table", "")
	return cmd
}

func TestRunIssuePullRequestsListsLinkedPRsAsJSON(t *testing.T) {
	var gotPaths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPaths = append(gotPaths, r.URL.Path)
		switch r.URL.Path {
		case "/api/issues/MUL-2818":
			json.NewEncoder(w).Encode(map[string]any{
				"id":         "issue-uuid",
				"identifier": "MUL-2818",
				"title":      "CLI PR lookup",
			})
		case "/api/issues/issue-uuid/pull-requests":
			json.NewEncoder(w).Encode(map[string]any{
				"pull_requests": []map[string]any{
					{
						"url":    "https://github.com/multica-ai/multica/pull/42",
						"number": float64(42),
						"state":  "open",
						"title":  "MUL-2818 add issue PR CLI",
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newIssuePullRequestsTestCmd()
	_ = cmd.Flags().Set("output", "json")
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	err := runIssuePullRequests(cmd, []string{"MUL-2818"})
	_ = w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)
	if err != nil {
		t.Fatalf("runIssuePullRequests: %v", err)
	}

	if want := []string{"/api/issues/MUL-2818", "/api/issues/issue-uuid/pull-requests"}; fmt.Sprint(gotPaths) != fmt.Sprint(want) {
		t.Fatalf("paths = %v, want %v", gotPaths, want)
	}
	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("decode JSON output: %v\n%s", err, string(out))
	}
	prs, _ := payload["pull_requests"].([]any)
	if len(prs) != 1 {
		t.Fatalf("pull_requests length = %d, want 1", len(prs))
	}
	pr, _ := prs[0].(map[string]any)
	if pr["url"] != "https://github.com/multica-ai/multica/pull/42" || pr["number"] != float64(42) || pr["state"] != "open" || pr["title"] != "MUL-2818 add issue PR CLI" {
		t.Fatalf("unexpected PR payload: %#v", pr)
	}
}

func newIssueUsageTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "usage"}
	cmd.Flags().String("output", "table", "")
	return cmd
}

func TestRunIssueUsageReturnsTokenSummaryAsJSON(t *testing.T) {
	var gotPaths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPaths = append(gotPaths, r.URL.Path)
		switch r.URL.Path {
		case "/api/issues/MUL-2818":
			json.NewEncoder(w).Encode(map[string]any{
				"id":         "issue-uuid",
				"identifier": "MUL-2818",
				"title":      "CLI usage lookup",
			})
		case "/api/issues/issue-uuid/usage":
			json.NewEncoder(w).Encode(map[string]any{
				"total_input_tokens":       float64(3800),
				"total_output_tokens":      float64(11700),
				"total_cache_read_tokens":  float64(537800),
				"total_cache_write_tokens": float64(42400),
				"task_count":               float64(1),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newIssueUsageTestCmd()
	_ = cmd.Flags().Set("output", "json")
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	err := runIssueUsage(cmd, []string{"MUL-2818"})
	_ = w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)
	if err != nil {
		t.Fatalf("runIssueUsage: %v", err)
	}

	if want := []string{"/api/issues/MUL-2818", "/api/issues/issue-uuid/usage"}; fmt.Sprint(gotPaths) != fmt.Sprint(want) {
		t.Fatalf("paths = %v, want %v", gotPaths, want)
	}
	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("decode JSON output: %v\n%s", err, string(out))
	}
	if payload["total_input_tokens"] != float64(3800) || payload["total_output_tokens"] != float64(11700) ||
		payload["total_cache_read_tokens"] != float64(537800) || payload["total_cache_write_tokens"] != float64(42400) ||
		payload["task_count"] != float64(1) {
		t.Fatalf("unexpected usage payload: %#v", payload)
	}
}

func TestRunIssuePullRequestsTableIncludesCoreFields(t *testing.T) {
	prs := []map[string]any{{
		"url":    "https://github.com/multica-ai/multica/pull/42",
		"number": float64(42),
		"state":  "open",
		"title":  "MUL-2818 add issue PR CLI",
	}}

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	printIssuePullRequestsTable(prs)
	_ = w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)
	text := string(out)
	for _, want := range []string{"NUMBER", "STATE", "TITLE", "URL", "42", "open", "MUL-2818 add issue PR CLI", "https://github.com/multica-ai/multica/pull/42"} {
		if !strings.Contains(text, want) {
			t.Fatalf("table output missing %q:\n%s", want, text)
		}
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

	t.Run("short UUID prefix is rejected without any HTTP call", func(t *testing.T) {
		// Until commit 9a3a99c this called fetchIssueCandidates and paged
		// /api/issues client-side, which timed out on large workspaces
		// (GH #4701). The resolver now refuses short prefixes outright
		// and the test pins that contract: any HTTP call here means the
		// removal regressed.
		var hits []string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits = append(hits, r.Method+" "+r.URL.Path)
			http.NotFound(w, r)
		}))
		defer srv.Close()

		client := cli.NewAPIClient(srv.URL, "ws-1", "test-token")
		_, err := resolveIssueRef(context.Background(), client, "1881")
		if err == nil {
			t.Fatal("expected short UUID prefix to be rejected")
		}
		if msg := err.Error(); !strings.Contains(msg, "short UUID prefix") {
			t.Fatalf("expected error to flag the short-prefix case, got: %s", msg)
		}
		if msg := err.Error(); !strings.Contains(msg, "MUL-") {
			t.Fatalf("expected error to suggest the issue key form, got: %s", msg)
		}
		if len(hits) != 0 {
			t.Fatalf("short prefix must not perform any HTTP call; got %#v", hits)
		}
	})

	t.Run("short UUID prefix with dashes is rejected", func(t *testing.T) {
		// "1881-a167" used to be accepted as a dashed short prefix; make
		// sure dashes do not slip past the new rejection.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Errorf("unexpected HTTP call: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}))
		defer srv.Close()

		client := cli.NewAPIClient(srv.URL, "ws-1", "test-token")
		_, err := resolveIssueRef(context.Background(), client, "1881-a167")
		if err == nil {
			t.Fatal("expected dashed short prefix to be rejected")
		}
		if msg := err.Error(); !strings.Contains(msg, "short UUID prefix") {
			t.Fatalf("expected short-prefix error, got: %s", msg)
		}
	})

	t.Run("bare numeric input is rejected as a short prefix", func(t *testing.T) {
		// A 4+ digit string is still pure hex, so the resolver classifies
		// it as a short UUID prefix and returns the tailored hint. This
		// pins that we never accidentally treat "1852" as the bare issue
		// number 1852.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Errorf("unexpected HTTP call: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}))
		defer srv.Close()

		client := cli.NewAPIClient(srv.URL, "ws-1", "test-token")
		_, err := resolveIssueRef(context.Background(), client, "1852")
		if err == nil {
			t.Fatal("expected bare number to be rejected")
		}
		if msg := err.Error(); !strings.Contains(msg, "short UUID prefix") {
			t.Fatalf("expected short-prefix error, got: %s", msg)
		}
	})

	t.Run("non-hex gibberish is rejected with key/UUID guidance", func(t *testing.T) {
		// Inputs that are neither MUL-key, full UUID, nor a hex prefix
		// fall through to the generic guidance path and must not hit the
		// network either.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Errorf("unexpected HTTP call: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}))
		defer srv.Close()

		client := cli.NewAPIClient(srv.URL, "ws-1", "test-token")
		_, err := resolveIssueRef(context.Background(), client, "not-an-id")
		if err == nil {
			t.Fatal("expected gibberish input to be rejected")
		}
		if msg := err.Error(); strings.Contains(msg, "short UUID prefix") {
			t.Fatalf("non-hex input should not be classified as a short prefix; got: %s", msg)
		}
		if msg := err.Error(); !strings.Contains(msg, "not a recognized issue reference") {
			t.Fatalf("expected generic guidance error, got: %s", msg)
		}
	})

	t.Run("full UUID still resolves via single GET", func(t *testing.T) {
		// Sanity check: the canonical reference forms must still work.
		var hits []string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits = append(hits, r.Method+" "+r.URL.Path)
			if r.URL.Path == "/api/issues/"+issue["id"].(string) {
				json.NewEncoder(w).Encode(issue)
				return
			}
			http.NotFound(w, r)
		}))
		defer srv.Close()

		client := cli.NewAPIClient(srv.URL, "ws-1", "test-token")
		got, err := resolveIssueRef(context.Background(), client, issue["id"].(string))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.ID != issue["id"] || got.Display != "MUL-1852" {
			t.Fatalf("got %#v", got)
		}
		if len(hits) != 1 {
			t.Fatalf("full UUID must resolve via a single request; got %#v", hits)
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

// newIssueCommentListTestCmd mirrors the flag set wired in main.init() for
// the comment list command. We replicate it here so the runIssueCommentList
// guards can be exercised in isolation — the real command tree pulls in the
// daemon init path.
func newIssueCommentListTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "list"}
	cmd.Flags().String("output", "json", "")
	cmd.Flags().String("since", "", "")
	cmd.Flags().Bool("roots-only", false, "")
	cmd.Flags().Bool("summary", false, "")
	cmd.Flags().Bool("full", false, "")
	cmd.Flags().String("thread", "", "")
	cmd.Flags().Int("recent", 0, "")
	cmd.Flags().Int("tail", 0, "")
	cmd.Flags().String("before", "", "")
	cmd.Flags().String("before-id", "", "")
	return cmd
}

func newIssueCommentResolutionTestCmd(use string) *cobra.Command {
	cmd := &cobra.Command{Use: use}
	cmd.Flags().String("output", "json", "")
	return cmd
}

func TestRunIssueCommentResolution(t *testing.T) {
	commentID := "comment-123"
	tests := []struct {
		name       string
		run        func(*cobra.Command, []string) error
		cmdUse     string
		wantMethod string
	}{
		{
			name:       "resolve posts to resolve endpoint",
			run:        runIssueCommentResolve,
			cmdUse:     "resolve",
			wantMethod: http.MethodPost,
		},
		{
			name:       "unresolve deletes resolve endpoint",
			run:        runIssueCommentUnresolve,
			cmdUse:     "unresolve",
			wantMethod: http.MethodDelete,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotMethod, gotPath string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod = r.Method
				gotPath = r.URL.Path
				if gotMethod != tt.wantMethod {
					t.Errorf("method = %s, want %s", gotMethod, tt.wantMethod)
				}
				if gotPath != "/api/comments/"+commentID+"/resolve" {
					t.Errorf("path = %q, want /api/comments/%s/resolve", gotPath, commentID)
				}
				if ws := r.Header.Get("X-Workspace-ID"); ws != "ws-1" {
					t.Errorf("X-Workspace-ID = %q, want ws-1", ws)
				}
				json.NewEncoder(w).Encode(map[string]any{
					"id":          commentID,
					"content":     "done",
					"resolved_at": "2026-06-22T08:00:00Z",
				})
			}))
			defer srv.Close()

			t.Setenv("MULTICA_SERVER_URL", srv.URL)
			t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
			t.Setenv("MULTICA_TOKEN", "test-token")

			cmd := newIssueCommentResolutionTestCmd(tt.cmdUse)
			out, err := captureStdout(t, func() error {
				return tt.run(cmd, []string{commentID})
			})
			if err != nil {
				t.Fatalf("run command: %v", err)
			}
			if gotMethod == "" || gotPath == "" {
				t.Fatal("server did not receive request")
			}

			var got map[string]any
			if err := json.Unmarshal([]byte(out), &got); err != nil {
				t.Fatalf("decode stdout JSON: %v\nstdout: %s", err, out)
			}
			if got["id"] != commentID {
				t.Fatalf("stdout id = %v, want %s", got["id"], commentID)
			}
		})
	}
}

// TestRunIssueCommentListFlagGuards locks the CLI-side flag combination
// matrix. Three behaviours matter here:
//
//   - --recent 0 / --recent -3 must error rather than silently fall back to
//     the default list path. Previously `recent > 0` collapsed "not passed"
//     and "passed an invalid value" into the same branch; using
//     Flags().Changed("recent") distinguishes them so an explicit non-
//     positive value is rejected.
//   - --before / --before-id without --recent must error. Before this fix
//     the cursor would be sent to the server but ignored because RecentN=0,
//     so callers asking for "comments before X" got the full timeline.
//   - --roots-only is mutually exclusive with the thread/recent/pagination
//     modes; it may only combine with --since.
//
// These cases must fail before any HTTP round-trip — verified by an
// httptest server that fatals if /api/issues/<key>/comments is hit.
func TestRunIssueCommentListFlagGuards(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// resolveIssueRef hits GET /api/issues/<ref>; everything else means
		// the guard let an invalid combination through to the wire.
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/issues/") && !strings.Contains(r.URL.Path, "/comments") {
			json.NewEncoder(w).Encode(map[string]any{
				"id":         "issue-1",
				"identifier": "MUL-1",
			})
			return
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.String())
		http.Error(w, "unexpected", http.StatusInternalServerError)
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cases := []struct {
		name    string
		setup   func(c *cobra.Command)
		wantMsg string
	}{
		{
			name: "explicit zero recent rejected",
			setup: func(c *cobra.Command) {
				_ = c.Flags().Set("recent", "0")
			},
			wantMsg: "--recent must be a positive integer",
		},
		{
			name: "negative recent rejected",
			setup: func(c *cobra.Command) {
				_ = c.Flags().Set("recent", "-3")
			},
			wantMsg: "--recent must be a positive integer",
		},
		{
			name: "before + before-id without recent or thread+tail rejected",
			setup: func(c *cobra.Command) {
				_ = c.Flags().Set("before", "2026-01-01T00:00:00Z")
				_ = c.Flags().Set("before-id", "00000000-0000-0000-0000-000000000001")
			},
			wantMsg: "require --recent",
		},
		{
			name: "thread + recent still rejected when --recent explicit zero", // also covers the Changed() path
			setup: func(c *cobra.Command) {
				_ = c.Flags().Set("thread", "00000000-0000-0000-0000-000000000001")
				_ = c.Flags().Set("recent", "5")
			},
			wantMsg: "--thread and --recent are mutually exclusive",
		},
		{
			// --tail is a thread-scoped limit. Outside of --thread it
			// would be silently dropped at the server, so the CLI rejects
			// it locally with a clear hint.
			name: "tail without thread rejected",
			setup: func(c *cobra.Command) {
				_ = c.Flags().Set("tail", "5")
			},
			wantMsg: "--tail requires --thread",
		},
		{
			// tail=0 is allowed (root-only) but a negative value would
			// round-trip to LIMIT -N on the server. Catch at the CLI so
			// the user sees a useful message instead of a 400.
			name: "negative tail rejected",
			setup: func(c *cobra.Command) {
				_ = c.Flags().Set("thread", "00000000-0000-0000-0000-000000000001")
				_ = c.Flags().Set("tail", "-2")
			},
			wantMsg: "--tail must be a non-negative integer",
		},
		{
			// --thread + --before without --tail used to be rejected
			// outright. Now it requires --tail so the cursor's "scroll
			// older replies" semantics has somewhere to land.
			name: "thread + before without tail rejected",
			setup: func(c *cobra.Command) {
				_ = c.Flags().Set("thread", "00000000-0000-0000-0000-000000000001")
				_ = c.Flags().Set("before", "2026-01-01T00:00:00Z")
				_ = c.Flags().Set("before-id", "00000000-0000-0000-0000-000000000002")
			},
			wantMsg: "require --recent",
		},
		{
			name: "roots-only + thread rejected",
			setup: func(c *cobra.Command) {
				_ = c.Flags().Set("roots-only", "true")
				_ = c.Flags().Set("thread", "00000000-0000-0000-0000-000000000001")
			},
			wantMsg: "--roots-only and --thread are mutually exclusive",
		},
		{
			name: "roots-only + recent rejected",
			setup: func(c *cobra.Command) {
				_ = c.Flags().Set("roots-only", "true")
				_ = c.Flags().Set("recent", "3")
			},
			wantMsg: "--roots-only and --recent are mutually exclusive",
		},
		{
			name: "roots-only + tail rejected",
			setup: func(c *cobra.Command) {
				_ = c.Flags().Set("roots-only", "true")
				_ = c.Flags().Set("tail", "3")
			},
			wantMsg: "--roots-only and --tail are mutually exclusive",
		},
		{
			name: "roots-only + before rejected",
			setup: func(c *cobra.Command) {
				_ = c.Flags().Set("roots-only", "true")
				_ = c.Flags().Set("before", "2026-01-01T00:00:00Z")
				_ = c.Flags().Set("before-id", "00000000-0000-0000-0000-000000000001")
			},
			wantMsg: "--roots-only does not support --before / --before-id",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newIssueCommentListTestCmd()
			tc.setup(cmd)
			err := runIssueCommentList(cmd, []string{"MUL-1"})
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantMsg)
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tc.wantMsg)
			}
		})
	}
}

// TestRunIssueCommentList_RootsOnlyPassesThroughWithSince pins the CLI side
// of #3164: the flag must be forwarded as the server's roots_only query param,
// and it must still allow the existing --since incremental polling filter.
func TestRunIssueCommentList_RootsOnlyPassesThroughWithSince(t *testing.T) {
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/issues/") && !strings.Contains(r.URL.Path, "/comments") {
			json.NewEncoder(w).Encode(map[string]any{
				"id":         "issue-1",
				"identifier": "MUL-1",
			})
			return
		}
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/comments") {
			gotQuery = r.URL.Query()
			w.Write([]byte("[]"))
			return
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.String())
		http.Error(w, "unexpected", http.StatusInternalServerError)
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newIssueCommentListTestCmd()
	if err := cmd.Flags().Set("roots-only", "true"); err != nil {
		t.Fatalf("set roots-only: %v", err)
	}
	if err := cmd.Flags().Set("since", "2026-01-01T00:00:00Z"); err != nil {
		t.Fatalf("set since: %v", err)
	}
	if err := runIssueCommentList(cmd, []string{"MUL-1"}); err != nil {
		t.Fatalf("runIssueCommentList: %v", err)
	}

	if got := gotQuery.Get("roots_only"); got != "true" {
		t.Errorf("roots_only query = %q, want true", got)
	}
	if got := gotQuery.Get("since"); got != "2026-01-01T00:00:00Z" {
		t.Errorf("since query = %q, want timestamp", got)
	}
}

// TestRunIssueCommentList_SummaryPassesThrough pins the CLI side of the summary
// projection: --summary must forward summary=true, and it must compose with
// --roots-only (the orientation read it pairs with most often) rather than
// being rejected as an incompatible combination.
func TestRunIssueCommentList_SummaryPassesThrough(t *testing.T) {
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/issues/") && !strings.Contains(r.URL.Path, "/comments") {
			json.NewEncoder(w).Encode(map[string]any{
				"id":         "issue-1",
				"identifier": "MUL-1",
			})
			return
		}
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/comments") {
			gotQuery = r.URL.Query()
			w.Write([]byte("[]"))
			return
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.String())
		http.Error(w, "unexpected", http.StatusInternalServerError)
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newIssueCommentListTestCmd()
	if err := cmd.Flags().Set("summary", "true"); err != nil {
		t.Fatalf("set summary: %v", err)
	}
	if err := cmd.Flags().Set("roots-only", "true"); err != nil {
		t.Fatalf("set roots-only: %v", err)
	}
	if err := runIssueCommentList(cmd, []string{"MUL-1"}); err != nil {
		t.Fatalf("runIssueCommentList: %v", err)
	}

	if got := gotQuery.Get("summary"); got != "true" {
		t.Errorf("summary query = %q, want true", got)
	}
	if got := gotQuery.Get("roots_only"); got != "true" {
		t.Errorf("roots_only query = %q, want true", got)
	}
}

// TestRunIssueCommentList_FoldDefaultAndFullEscape pins the CLI's resolve-aware
// fold default and its --full escape hatch (MUL-3555):
//
//   - the complete-thread reads (default list, --recent, untailed --thread) send
//     fold=true by default, so an agent skips settled discussion automatically;
//   - --full suppresses fold so a caller can pull the whole discussion back;
//   - the partial-thread reads (--since, --thread + --tail) and --roots-only
//     never send fold — folding them would be unsafe, and the server rejects it,
//     so the CLI must not add the param there in the first place.
func TestRunIssueCommentList_FoldDefaultAndFullEscape(t *testing.T) {
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/issues/") && !strings.Contains(r.URL.Path, "/comments") {
			json.NewEncoder(w).Encode(map[string]any{
				"id":         "issue-1",
				"identifier": "MUL-1",
			})
			return
		}
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/comments") {
			gotQuery = r.URL.Query()
			w.Write([]byte("[]"))
			return
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.String())
		http.Error(w, "unexpected", http.StatusInternalServerError)
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cases := []struct {
		name     string
		setup    func(c *cobra.Command)
		wantFold bool
	}{
		{name: "default list folds", setup: func(c *cobra.Command) {}, wantFold: true},
		{name: "recent folds", setup: func(c *cobra.Command) {
			_ = c.Flags().Set("recent", "10")
		}, wantFold: true},
		{name: "untailed thread folds", setup: func(c *cobra.Command) {
			_ = c.Flags().Set("thread", "00000000-0000-0000-0000-000000000001")
		}, wantFold: true},
		{name: "full opts out", setup: func(c *cobra.Command) {
			_ = c.Flags().Set("full", "true")
		}, wantFold: false},
		{name: "since never folds", setup: func(c *cobra.Command) {
			_ = c.Flags().Set("since", "2026-01-01T00:00:00Z")
		}, wantFold: false},
		{name: "thread+tail never folds", setup: func(c *cobra.Command) {
			_ = c.Flags().Set("thread", "00000000-0000-0000-0000-000000000001")
			_ = c.Flags().Set("tail", "5")
		}, wantFold: false},
		{name: "roots-only never folds", setup: func(c *cobra.Command) {
			_ = c.Flags().Set("roots-only", "true")
		}, wantFold: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotQuery = nil
			cmd := newIssueCommentListTestCmd()
			tc.setup(cmd)
			if err := runIssueCommentList(cmd, []string{"MUL-1"}); err != nil {
				t.Fatalf("runIssueCommentList: %v", err)
			}
			gotFold := gotQuery.Get("fold") == "true"
			if gotFold != tc.wantFold {
				t.Errorf("fold query = %q, want fold=%v", gotQuery.Get("fold"), tc.wantFold)
			}
		})
	}
}

// TestRunIssueCommentList_ThreadTailPassesThroughAndPrintsReplyCursor pins
// the positive path for --thread + --tail: the CLI forwards `thread` +
// `tail` query params and, on response, prints "Next reply cursor" (not
// "Next thread cursor") so an operator can scroll older replies inside
// the same thread without guessing which cursor model the server emitted.
func TestRunIssueCommentList_ThreadTailPassesThroughAndPrintsReplyCursor(t *testing.T) {
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/issues/") && !strings.Contains(r.URL.Path, "/comments") {
			json.NewEncoder(w).Encode(map[string]any{
				"id":         "issue-1",
				"identifier": "MUL-1",
			})
			return
		}
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/comments") {
			gotQuery = r.URL.Query()
			// Emit a cursor so we can prove the CLI labels it "reply"
			// when the call was a --thread + --tail combo.
			w.Header().Set("X-Multica-Next-Before", "2026-01-01T00:00:00.000000001Z")
			w.Header().Set("X-Multica-Next-Before-Id", "00000000-0000-0000-0000-000000000999")
			w.Write([]byte("[]"))
			return
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.String())
		http.Error(w, "unexpected", http.StatusInternalServerError)
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	// Redirect stderr so we can assert on the "Next reply cursor" line —
	// that's the user-visible signal that the CLI knew it was paging
	// within a thread, not across threads.
	stderr := captureStderr(t)
	defer stderr.restore()

	cmd := newIssueCommentListTestCmd()
	if err := cmd.Flags().Set("thread", "00000000-0000-0000-0000-000000000001"); err != nil {
		t.Fatalf("set thread: %v", err)
	}
	if err := cmd.Flags().Set("tail", "5"); err != nil {
		t.Fatalf("set tail: %v", err)
	}
	if err := runIssueCommentList(cmd, []string{"MUL-1"}); err != nil {
		t.Fatalf("runIssueCommentList: %v", err)
	}

	if got := gotQuery.Get("thread"); got != "00000000-0000-0000-0000-000000000001" {
		t.Errorf("thread query = %q, want the passed anchor", got)
	}
	if got := gotQuery.Get("tail"); got != "5" {
		t.Errorf("tail query = %q, want %q", got, "5")
	}
	out := stderr.read()
	if !strings.Contains(out, "Next reply cursor: --before 2026-01-01T00:00:00.000000001Z --before-id 00000000-0000-0000-0000-000000000999") {
		t.Errorf("stderr missing reply-cursor line, got: %q", out)
	}
}

// TestRunIssueCommentList_RecentStillLabelsCursorAsThread is the negative
// counterpart: under --recent the CLI must keep printing "Next thread
// cursor". A regression that printed "reply" here would mis-signal the
// cursor semantics to anyone copy-pasting it.
func TestRunIssueCommentList_RecentStillLabelsCursorAsThread(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/issues/") && !strings.Contains(r.URL.Path, "/comments") {
			json.NewEncoder(w).Encode(map[string]any{
				"id":         "issue-1",
				"identifier": "MUL-1",
			})
			return
		}
		w.Header().Set("X-Multica-Next-Before", "2026-01-01T00:00:00.000000001Z")
		w.Header().Set("X-Multica-Next-Before-Id", "00000000-0000-0000-0000-000000000777")
		w.Write([]byte("[]"))
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	stderr := captureStderr(t)
	defer stderr.restore()

	cmd := newIssueCommentListTestCmd()
	if err := cmd.Flags().Set("recent", "3"); err != nil {
		t.Fatalf("set recent: %v", err)
	}
	if err := runIssueCommentList(cmd, []string{"MUL-1"}); err != nil {
		t.Fatalf("runIssueCommentList: %v", err)
	}

	out := stderr.read()
	if !strings.Contains(out, "Next thread cursor:") {
		t.Errorf("stderr missing thread-cursor line, got: %q", out)
	}
}

// TestRunIssueCommentList_DoesNotPrintShowingPreamble locks in the removal of
// the "Showing N comments." stderr preamble. The line was the only
// `list --output json` subcommand that emitted a human-readable count, which
// polluted stdout/stderr-merged consumers (agent harnesses, CI `2>&1`).
// Tracks GitHub issue #3303.
func TestRunIssueCommentList_DoesNotPrintShowingPreamble(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/issues/") && !strings.Contains(r.URL.Path, "/comments") {
			json.NewEncoder(w).Encode(map[string]any{
				"id":         "issue-1",
				"identifier": "MUL-1",
			})
			return
		}
		w.Write([]byte(`[{"id":"c1"},{"id":"c2"}]`))
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	stderr := captureStderr(t)
	defer stderr.restore()

	cmd := newIssueCommentListTestCmd()
	if err := cmd.Flags().Set("output", "json"); err != nil {
		t.Fatalf("set output: %v", err)
	}
	if err := runIssueCommentList(cmd, []string{"MUL-1"}); err != nil {
		t.Fatalf("runIssueCommentList: %v", err)
	}

	if got := stderr.read(); strings.Contains(got, "Showing") {
		t.Errorf("stderr must not contain a 'Showing ...' preamble, got: %q", got)
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

func TestValidateIssueStatus(t *testing.T) {
	for _, s := range validIssueStatuses {
		if err := validateIssueStatus(s); err != nil {
			t.Errorf("status %q should be valid, got: %v", s, err)
		}
	}
	err := validateIssueStatus("active")
	if err == nil {
		t.Fatal("status \"active\" should be rejected")
	}
	if !strings.Contains(err.Error(), "backlog") {
		t.Errorf("error should list valid statuses, got: %v", err)
	}
}

func TestValidateIssuePriority(t *testing.T) {
	expected := map[string]bool{
		"urgent": true,
		"high":   true,
		"medium": true,
		"low":    true,
		"none":   true,
	}
	for _, p := range validIssuePriorities {
		if !expected[p] {
			t.Errorf("unexpected priority in validIssuePriorities: %q", p)
		}
		if err := validateIssuePriority(p); err != nil {
			t.Errorf("priority %q should be valid, got: %v", p, err)
		}
	}
	if len(validIssuePriorities) != len(expected) {
		t.Errorf("validIssuePriorities has %d entries, expected %d", len(validIssuePriorities), len(expected))
	}
	err := validateIssuePriority("P1")
	if err == nil {
		t.Fatal("priority \"P1\" should be rejected")
	}
	if !strings.Contains(err.Error(), "urgent") {
		t.Errorf("error should list valid priorities, got: %v", err)
	}
}

func TestRunIssueCreateRejectsInvalidStatusBeforeRequest(t *testing.T) {
	cmd := newIssueCreateTestCmd()
	_ = cmd.Flags().Set("title", "Invalid status")
	_ = cmd.Flags().Set("status", "active")
	err := runIssueCreate(cmd, nil)
	if err == nil {
		t.Fatal("runIssueCreate should reject invalid status")
	}
	if !strings.Contains(err.Error(), "valid values") {
		t.Fatalf("expected valid values error, got: %v", err)
	}
}

func TestRunIssueCreateRejectsInvalidPriorityBeforeRequest(t *testing.T) {
	cmd := newIssueCreateTestCmd()
	_ = cmd.Flags().Set("title", "Invalid priority")
	_ = cmd.Flags().Set("priority", "P1")
	err := runIssueCreate(cmd, nil)
	if err == nil {
		t.Fatal("runIssueCreate should reject invalid priority")
	}
	if !strings.Contains(err.Error(), "valid values") {
		t.Fatalf("expected valid values error, got: %v", err)
	}
}

func TestRunIssueUpdateRejectsInvalidStatusBeforeRequest(t *testing.T) {
	cmd := &cobra.Command{Use: "update"}
	cmd.Flags().String("status", "", "")
	cmd.Flags().String("priority", "", "")
	_ = cmd.Flags().Set("status", "active")
	err := runIssueUpdate(cmd, []string{"MUL-1"})
	if err == nil {
		t.Fatal("runIssueUpdate should reject invalid status")
	}
	if !strings.Contains(err.Error(), "valid values") {
		t.Fatalf("expected valid values error, got: %v", err)
	}
}

func TestRunIssueUpdateRejectsInvalidPriorityBeforeRequest(t *testing.T) {
	cmd := &cobra.Command{Use: "update"}
	cmd.Flags().String("status", "", "")
	cmd.Flags().String("priority", "", "")
	_ = cmd.Flags().Set("priority", "P1")
	err := runIssueUpdate(cmd, []string{"MUL-1"})
	if err == nil {
		t.Fatal("runIssueUpdate should reject invalid priority")
	}
	if !strings.Contains(err.Error(), "valid values") {
		t.Fatalf("expected valid values error, got: %v", err)
	}
}

func newIssueUpdateTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "update"}
	cmd.Flags().String("title", "", "")
	cmd.Flags().String("description", "", "")
	cmd.Flags().Bool("description-stdin", false, "")
	cmd.Flags().String("description-file", "", "")
	cmd.Flags().String("status", "", "")
	cmd.Flags().String("priority", "", "")
	cmd.Flags().String("assignee", "", "")
	cmd.Flags().String("assignee-id", "", "")
	cmd.Flags().String("project", "", "")
	cmd.Flags().String("start-date", "", "")
	cmd.Flags().String("due-date", "", "")
	cmd.Flags().String("parent", "", "")
	cmd.Flags().Float64("position", 0, "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

func newIssueListTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "list"}
	cmd.Flags().String("output", "table", "")
	cmd.Flags().Bool("full-id", false, "")
	cmd.Flags().String("status", "", "")
	cmd.Flags().String("priority", "", "")
	cmd.Flags().String("assignee", "", "")
	cmd.Flags().String("assignee-id", "", "")
	cmd.Flags().String("project", "", "")
	cmd.Flags().StringSlice("metadata", nil, "")
	cmd.Flags().Int("limit", 50, "")
	cmd.Flags().Int("offset", 0, "")
	cmd.Flags().String("sort", "", "")
	cmd.Flags().String("direction", "", "")
	return cmd
}

func newIssueReorderTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "reorder"}
	registerIssueReorderFlags(cmd)
	return cmd
}

func TestRunIssueUpdateSendsPosition(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/MUL-1":
			json.NewEncoder(w).Encode(map[string]any{
				"id": "issue-1", "identifier": "MUL-1", "status": "todo",
			})
		case r.Method == http.MethodPut && r.URL.Path == "/api/issues/issue-1":
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode body: %v", err)
			}
			json.NewEncoder(w).Encode(map[string]any{
				"id": "issue-1", "identifier": "MUL-1", "status": "todo", "priority": "none",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newIssueUpdateTestCmd()
	_ = cmd.Flags().Set("position", "7.5")
	if err := runIssueUpdate(cmd, []string{"MUL-1"}); err != nil {
		t.Fatalf("runIssueUpdate: %v", err)
	}
	if got := body["position"]; got != float64(7.5) {
		t.Fatalf("position = %#v, want 7.5 in request body", got)
	}
}

func TestRunIssueListSendsSortAndDirection(t *testing.T) {
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/issues" {
			http.NotFound(w, r)
			return
		}
		gotQuery = r.URL.Query()
		json.NewEncoder(w).Encode(map[string]any{"issues": []any{}, "total": 0})
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newIssueListTestCmd()
	_ = cmd.Flags().Set("output", "json")
	_ = cmd.Flags().Set("sort", "created_at")
	_ = cmd.Flags().Set("direction", "DESC")
	if err := runIssueList(cmd, nil); err != nil {
		t.Fatalf("runIssueList: %v", err)
	}
	if got := gotQuery.Get("sort"); got != "created_at" {
		t.Fatalf("sort query = %q, want created_at", got)
	}
	if got := gotQuery.Get("direction"); got != "desc" {
		t.Fatalf("direction query = %q, want desc (lower-cased)", got)
	}
}

func TestRunIssueListRejectsInvalidSortAndDirection(t *testing.T) {
	t.Setenv("MULTICA_SERVER_URL", "http://127.0.0.1:0")
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newIssueListTestCmd()
	_ = cmd.Flags().Set("sort", "nonsense")
	if err := runIssueList(cmd, nil); err == nil {
		t.Fatal("runIssueList: expected error for invalid --sort")
	} else if !strings.Contains(err.Error(), "invalid --sort") {
		t.Fatalf("error = %q, want it to mention invalid --sort", err)
	}

	cmd = newIssueListTestCmd()
	_ = cmd.Flags().Set("direction", "sideways")
	if err := runIssueList(cmd, nil); err == nil {
		t.Fatal("runIssueList: expected error for invalid --direction")
	} else if !strings.Contains(err.Error(), "invalid --direction") {
		t.Fatalf("error = %q, want it to mention invalid --direction", err)
	}
}

// TestRunIssueListRejectsDirectionWithoutDirectionalSort guards that passing a
// valid --direction while the sort is the default/position (which the server
// always sorts ascending) is rejected up front, rather than silently dropping
// the flag — a passed-but-ignored flag is a footgun, especially in scripts.
func TestRunIssueListRejectsDirectionWithoutDirectionalSort(t *testing.T) {
	t.Setenv("MULTICA_SERVER_URL", "http://127.0.0.1:0")
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cases := []struct {
		name string
		sort string
	}{
		{"direction without sort", ""},
		{"direction with explicit position sort", "position"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newIssueListTestCmd()
			if tc.sort != "" {
				_ = cmd.Flags().Set("sort", tc.sort)
			}
			_ = cmd.Flags().Set("direction", "desc")
			err := runIssueList(cmd, nil)
			if err == nil {
				t.Fatal("runIssueList: expected error for --direction on position/default sort")
			}
			if !strings.Contains(err.Error(), "--direction requires --sort") {
				t.Fatalf("error = %q, want it to explain --direction requires a directional --sort", err)
			}
			// The message should name a concrete directional column to guide the fix.
			if !strings.Contains(err.Error(), "created_at") {
				t.Fatalf("error = %q, want it to list the valid directional sort columns", err)
			}
		})
	}
}

func TestComputeReorderPosition(t *testing.T) {
	positions := map[string]float64{"a": 10, "b": 20, "x": 99}
	tests := []struct {
		name     string
		ids      []string
		active   string
		fallback float64
		want     float64
	}{
		{"single item keeps position", []string{"x"}, "x", 42, 42},
		{"absent active keeps position", []string{"a", "b"}, "x", 7, 7},
		{"top is one below the next", []string{"x", "a", "b"}, "x", 99, 9},
		{"bottom is one above the prev", []string{"a", "b", "x"}, "x", 99, 21},
		{"interior is the midpoint", []string{"a", "x", "b"}, "x", 99, 15},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := computeReorderPosition(tc.ids, tc.active, positions, tc.fallback)
			if got != tc.want {
				t.Fatalf("computeReorderPosition = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestIssueReorderTargetFlagGroup drives the command through Execute so the real
// cobra flag group (registerIssueReorderFlags) validates the "exactly one
// target" rule — zero targets, two bool targets, and --before+--after are all
// rejected before RunE. A stub RunE that errors proves validation happens first.
// The assertions match on cobra's canonical flag-group error strings, so they
// are coupled to cobra's wording (pinned at v1.10.2 in server/go.mod).
func TestIssueReorderTargetFlagGroup(t *testing.T) {
	newCmd := func() *cobra.Command {
		cmd := newIssueReorderTestCmd()
		cmd.Args = exactArgs(1)
		cmd.RunE = func(*cobra.Command, []string) error {
			return fmt.Errorf("RunE ran despite an invalid target flag group")
		}
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true
		return cmd
	}

	t.Run("no target flag", func(t *testing.T) {
		cmd := newCmd()
		cmd.SetArgs([]string{"MUL-1"})
		err := cmd.Execute()
		if err == nil {
			t.Fatal("expected an error when no target flag is set")
		}
		if !strings.Contains(err.Error(), "at least one of the flags in the group") {
			t.Fatalf("want cobra one-required error, got: %v", err)
		}
	})

	t.Run("two bool targets", func(t *testing.T) {
		cmd := newCmd()
		cmd.SetArgs([]string{"MUL-1", "--top", "--bottom"})
		err := cmd.Execute()
		if err == nil {
			t.Fatal("expected an error when --top and --bottom are combined")
		}
		if !strings.Contains(err.Error(), "none of the others can be") {
			t.Fatalf("want cobra mutually-exclusive error, got: %v", err)
		}
	})

	t.Run("before and after together", func(t *testing.T) {
		cmd := newCmd()
		cmd.SetArgs([]string{"MUL-1", "--before", "MUL-2", "--after", "MUL-3"})
		err := cmd.Execute()
		if err == nil {
			t.Fatal("expected an error when --before and --after are combined")
		}
		if !strings.Contains(err.Error(), "none of the others can be") {
			t.Fatalf("want cobra mutually-exclusive error, got: %v", err)
		}
	})
}

// TestRunIssueReorderRejectsNoOpTargetValues covers the cases the flag group
// cannot: cobra keys off flag presence (Changed), not value, so an
// explicitly-passed but empty --before/--after or an explicit --top=false /
// --bottom=false satisfies the group yet selects no real target. runIssueReorder
// rejects each up front rather than falling through to a confusing downstream
// error.
func TestRunIssueReorderRejectsNoOpTargetValues(t *testing.T) {
	cases := []struct{ flag, value string }{
		{"before", ""},
		{"after", ""},
		{"top", "false"},
		{"bottom", "false"},
	}
	for _, tc := range cases {
		t.Run(tc.flag, func(t *testing.T) {
			cmd := newIssueReorderTestCmd()
			if err := cmd.Flags().Set(tc.flag, tc.value); err != nil {
				t.Fatalf("set --%s=%q: %v", tc.flag, tc.value, err)
			}
			err := runIssueReorder(cmd, []string{"MUL-1"})
			if err == nil {
				t.Fatalf("expected an error when --%s is passed %q", tc.flag, tc.value)
			}
			if !strings.Contains(err.Error(), "--"+tc.flag) {
				t.Fatalf("want error naming --%s, got: %v", tc.flag, err)
			}
		})
	}
}

// reorderTestServer serves a fixed three-issue "todo" column (positions
// 1/9/20) plus single-issue lookups by key and id, capturing the position of
// any PUT so a test can assert where the issue landed.
func reorderTestServer(t *testing.T, gotPosition *float64) *httptest.Server {
	t.Helper()
	issue := func(id, key, status string, pos float64) map[string]any {
		return map[string]any{"id": id, "identifier": key, "status": status, "position": pos}
	}
	a := issue("a-id", "MUL-1", "todo", 1)
	b := issue("b-id", "MUL-2", "todo", 9)
	target := issue("t-id", "MUL-9", "todo", 20)
	c := issue("c-id", "MUL-3", "in_progress", 4)
	byRef := map[string]map[string]any{
		"MUL-1": a, "a-id": a,
		"MUL-2": b, "b-id": b,
		"MUL-9": target, "t-id": target,
		"MUL-3": c, "c-id": c,
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/issues" && r.Method == http.MethodGet {
			// The column query only ever asks for "todo" in these tests.
			json.NewEncoder(w).Encode(map[string]any{
				"issues": []any{a, b, target},
				"total":  3,
			})
			return
		}
		ref, _ := url.PathUnescape(strings.TrimPrefix(r.URL.Path, "/api/issues/"))
		found, ok := byRef[ref]
		if !ok {
			http.NotFound(w, r)
			return
		}
		switch r.Method {
		case http.MethodGet:
			json.NewEncoder(w).Encode(found)
		case http.MethodPut:
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode body: %v", err)
			}
			if p, ok := body["position"].(float64); ok && gotPosition != nil {
				*gotPosition = p
			}
			merged := map[string]any{}
			for k, v := range found {
				merged[k] = v
			}
			if p, ok := body["position"]; ok {
				merged["position"] = p
			}
			json.NewEncoder(w).Encode(merged)
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestRunIssueReorderComputesPosition(t *testing.T) {
	tests := []struct {
		name string
		flag string
		val  string
		want float64
	}{
		{"top of column", "top", "true", 0},            // pos(a) - 1 = 0
		{"bottom of column", "bottom", "true", 10},     // pos(b) + 1 = 10
		{"before another issue", "before", "MUL-2", 5}, // midpoint(a=1, b=9)
		{"after another issue", "after", "MUL-2", 10},  // moves below b (the bottom)
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var gotPosition float64 = -1
			srv := reorderTestServer(t, &gotPosition)
			defer srv.Close()

			t.Setenv("MULTICA_SERVER_URL", srv.URL)
			t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
			t.Setenv("MULTICA_TOKEN", "test-token")

			cmd := newIssueReorderTestCmd()
			_ = cmd.Flags().Set(tc.flag, tc.val)
			if err := runIssueReorder(cmd, []string{"MUL-9"}); err != nil {
				t.Fatalf("runIssueReorder: %v", err)
			}
			if gotPosition != tc.want {
				t.Fatalf("PUT position = %v, want %v", gotPosition, tc.want)
			}
		})
	}
}

func TestRunIssueReorderRejectsCrossColumnTarget(t *testing.T) {
	srv := reorderTestServer(t, nil)
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newIssueReorderTestCmd()
	_ = cmd.Flags().Set("before", "MUL-3") // MUL-3 lives in the in_progress column
	err := runIssueReorder(cmd, []string{"MUL-9"})
	if err == nil {
		t.Fatal("expected error when the --before target is in another column")
	}
	if !strings.Contains(err.Error(), "in_progress") || !strings.Contains(err.Error(), "todo") {
		t.Fatalf("error = %q, want it to name both columns", err)
	}
}

func mkIssue(id, key, status string, pos float64) map[string]any {
	return map[string]any{"id": id, "identifier": key, "status": status, "position": pos}
}

func TestFetchIssueColumnPaginates(t *testing.T) {
	all := []map[string]any{
		mkIssue("i1", "MUL-1", "todo", 1),
		mkIssue("i2", "MUL-2", "todo", 2),
		mkIssue("i3", "MUL-3", "todo", 3),
	}
	var pageRequests int
	var sawProject string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/issues" {
			http.NotFound(w, r)
			return
		}
		pageRequests++
		sawProject = r.URL.Query().Get("project_id")
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		// Force pagination by serving two issues at a time regardless of the
		// requested limit, so the loop's offset/total termination is exercised.
		page := []any{}
		for i := offset; i < offset+2 && i < len(all); i++ {
			page = append(page, all[i])
		}
		json.NewEncoder(w).Encode(map[string]any{"issues": page, "total": len(all)})
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	client, err := newAPIClient(&cobra.Command{Use: "x"})
	if err != nil {
		t.Fatalf("newAPIClient: %v", err)
	}
	got, err := fetchIssueColumn(context.Background(), client, "ws-1", "proj-1", "todo")
	if err != nil {
		t.Fatalf("fetchIssueColumn: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d issues, want 3 (pagination should accumulate all pages)", len(got))
	}
	if pageRequests < 2 {
		t.Fatalf("page requests = %d, want >= 2 (loop should paginate)", pageRequests)
	}
	if sawProject != "proj-1" {
		t.Fatalf("project_id query = %q, want proj-1 (column should be project-scoped)", sawProject)
	}
	for i, want := range []string{"i1", "i2", "i3"} {
		if got := strVal(got[i], "id"); got != want {
			t.Fatalf("ordered[%d] = %q, want %q (ascending position order preserved)", i, got, want)
		}
	}
}

func TestRunIssueReorderNoOpSkipsPut(t *testing.T) {
	// Column order by position: a(0), target(5), b(10). Asking for the target
	// to go --before b computes (0+10)/2 = 5, which it already holds, so the
	// command should short-circuit without issuing a PUT.
	a := mkIssue("a-id", "MUL-1", "todo", 0)
	b := mkIssue("b-id", "MUL-2", "todo", 10)
	target := mkIssue("t-id", "MUL-9", "todo", 5)
	byRef := map[string]map[string]any{
		"MUL-1": a, "a-id": a,
		"MUL-2": b, "b-id": b,
		"MUL-9": target, "t-id": target,
	}
	putCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/issues" && r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(map[string]any{"issues": []any{a, target, b}, "total": 3})
			return
		}
		if r.Method == http.MethodPut {
			putCalled = true
			http.NotFound(w, r)
			return
		}
		ref, _ := url.PathUnescape(strings.TrimPrefix(r.URL.Path, "/api/issues/"))
		if found, ok := byRef[ref]; ok {
			json.NewEncoder(w).Encode(found)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newIssueReorderTestCmd()
	_ = cmd.Flags().Set("before", "MUL-2")
	if err := runIssueReorder(cmd, []string{"MUL-9"}); err != nil {
		t.Fatalf("runIssueReorder: %v", err)
	}
	if putCalled {
		t.Fatal("expected no PUT when the issue is already in the requested position")
	}
}

func TestRunIssueReorderSingleItemColumnValidatesTarget(t *testing.T) {
	// The "todo" column holds only the issue being moved; MUL-3 lives in a
	// different column. A relative move must still validate its target rather
	// than reporting a successful no-op (regression guard for the single-item
	// fast path running before target resolution).
	target := mkIssue("t-id", "MUL-9", "todo", 5)
	other := mkIssue("c-id", "MUL-3", "in_progress", 2)
	byRef := map[string]map[string]any{
		"MUL-9": target, "t-id": target,
		"MUL-3": other, "c-id": other,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/issues" && r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(map[string]any{"issues": []any{target}, "total": 1})
			return
		}
		ref, _ := url.PathUnescape(strings.TrimPrefix(r.URL.Path, "/api/issues/"))
		if found, ok := byRef[ref]; ok {
			json.NewEncoder(w).Encode(found)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	t.Run("cross-column target errors", func(t *testing.T) {
		cmd := newIssueReorderTestCmd()
		_ = cmd.Flags().Set("after", "MUL-3")
		err := runIssueReorder(cmd, []string{"MUL-9"})
		if err == nil {
			t.Fatal("expected an error, not a silent no-op")
		}
		if !strings.Contains(err.Error(), "in_progress") {
			t.Fatalf("error = %q, want it to name the target's column", err)
		}
	})

	t.Run("self target errors", func(t *testing.T) {
		cmd := newIssueReorderTestCmd()
		_ = cmd.Flags().Set("before", "MUL-9")
		err := runIssueReorder(cmd, []string{"MUL-9"})
		if err == nil {
			t.Fatal("expected an error when targeting itself")
		}
		if !strings.Contains(err.Error(), "itself") {
			t.Fatalf("error = %q, want it to mention self-reference", err)
		}
	})
}

func TestRunIssueReorderOnlyIssueInColumnIsNoOp(t *testing.T) {
	// A --top/--bottom move on a single-issue column has nowhere to go, so it
	// short-circuits without a PUT instead of rewriting the position.
	target := mkIssue("t-id", "MUL-9", "todo", 5)
	putCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/issues" && r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(map[string]any{"issues": []any{target}, "total": 1})
			return
		}
		if r.Method == http.MethodPut {
			putCalled = true
			http.NotFound(w, r)
			return
		}
		ref, _ := url.PathUnescape(strings.TrimPrefix(r.URL.Path, "/api/issues/"))
		if ref == "MUL-9" || ref == "t-id" {
			json.NewEncoder(w).Encode(target)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newIssueReorderTestCmd()
	_ = cmd.Flags().Set("top", "true")
	if err := runIssueReorder(cmd, []string{"MUL-9"}); err != nil {
		t.Fatalf("runIssueReorder: %v", err)
	}
	if putCalled {
		t.Fatal("expected no PUT when the issue is alone in its column")
	}
}

func TestRunIssueUpdateOmitsPositionWhenUnset(t *testing.T) {
	// position 0 is a real, meaningful value, so an update that does not pass
	// --position must not send "position" at all (Flags().Changed guard).
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/MUL-1":
			json.NewEncoder(w).Encode(map[string]any{"id": "issue-1", "identifier": "MUL-1", "status": "todo"})
		case r.Method == http.MethodPut && r.URL.Path == "/api/issues/issue-1":
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode body: %v", err)
			}
			json.NewEncoder(w).Encode(map[string]any{"id": "issue-1", "identifier": "MUL-1", "title": "Renamed"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newIssueUpdateTestCmd()
	_ = cmd.Flags().Set("title", "Renamed")
	if err := runIssueUpdate(cmd, []string{"MUL-1"}); err != nil {
		t.Fatalf("runIssueUpdate: %v", err)
	}
	if _, present := body["position"]; present {
		t.Fatalf("position must be absent from the body when --position is not passed, got %#v", body["position"])
	}
}

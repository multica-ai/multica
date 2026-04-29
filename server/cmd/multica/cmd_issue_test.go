package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
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
// stdin flag pair that resolveTextFlag expects.
func newFlagTestCmd(name string) *cobra.Command {
	c := &cobra.Command{Use: "test"}
	c.Flags().String(name, "", "")
	c.Flags().Bool(name+"-stdin", false, "")
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
	tests := []struct {
		name  string
		issue map[string]any
		want  string
	}{
		{"empty", map[string]any{}, ""},
		{"no type", map[string]any{"assignee_id": "abc"}, ""},
		{"no id", map[string]any{"assignee_type": "member"}, ""},
		{"member", map[string]any{"assignee_type": "member", "assignee_id": "abcdefgh-1234"}, "member:abcdefgh"},
		{"agent", map[string]any{"assignee_type": "agent", "assignee_id": "xyz"}, "agent:xyz"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatAssignee(tt.issue)
			if got != tt.want {
				t.Errorf("formatAssignee() = %q, want %q", got, tt.want)
			}
		})
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

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/workspaces/ws-1/members":
			json.NewEncoder(w).Encode(membersResp)
		case "/api/agents":
			json.NewEncoder(w).Encode(agentsResp)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := cli.NewAPIClient(srv.URL, "ws-1", "test-token")
	ctx := context.Background()

	t.Run("exact match member", func(t *testing.T) {
		aType, aID, err := resolveAssignee(ctx, client, "Alice Smith")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if aType != "member" || aID != "user-1111" {
			t.Errorf("got (%q, %q), want (member, user-1111)", aType, aID)
		}
	})

	t.Run("case-insensitive substring", func(t *testing.T) {
		aType, aID, err := resolveAssignee(ctx, client, "bob")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if aType != "member" || aID != "user-2222" {
			t.Errorf("got (%q, %q), want (member, user-2222)", aType, aID)
		}
	})

	t.Run("match agent", func(t *testing.T) {
		aType, aID, err := resolveAssignee(ctx, client, "codebot")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if aType != "agent" || aID != "agent-3333" {
			t.Errorf("got (%q, %q), want (agent, agent-3333)", aType, aID)
		}
	})

	t.Run("no match", func(t *testing.T) {
		_, _, err := resolveAssignee(ctx, client, "nobody")
		if err == nil {
			t.Fatal("expected error for no match")
		}
	})

	t.Run("ambiguous", func(t *testing.T) {
		// Both "Alice Smith" and "Bob Jones" contain a space — but let's use a broader query
		// "e" matches "Alice Smith" and "Bob Jones" and "CodeBot"
		_, _, err := resolveAssignee(ctx, client, "o")
		if err == nil {
			t.Fatal("expected error for ambiguous match")
		}
		if got := err.Error(); !strings.Contains(got, "ambiguous") {
			t.Errorf("expected ambiguous error, got: %s", got)
		}
	})

	t.Run("missing workspace ID", func(t *testing.T) {
		noWSClient := cli.NewAPIClient(srv.URL, "", "test-token")
		_, _, err := resolveAssignee(ctx, noWSClient, "alice")
		if err == nil {
			t.Fatal("expected error for missing workspace ID")
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
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := cli.NewAPIClient(srv.URL, "ws-1", "test-token")
	ctx := context.Background()

	t.Run("exact shorter name resolves to shorter agent", func(t *testing.T) {
		aType, aID, err := resolveAssignee(ctx, client, "reviewer")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if aType != "agent" || aID != "f656eab8-1111-1111-1111-111111111111" {
			t.Errorf("got (%q, %q), want (agent, f656eab8-...)", aType, aID)
		}
	})

	t.Run("exact longer name still resolves unambiguously", func(t *testing.T) {
		aType, aID, err := resolveAssignee(ctx, client, "peer-reviewer")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if aType != "agent" || aID != "9b0ff9a2-2222-2222-2222-222222222222" {
			t.Errorf("got (%q, %q), want (agent, 9b0ff9a2-...)", aType, aID)
		}
	})

	t.Run("exact match is case-insensitive and tolerates whitespace", func(t *testing.T) {
		aType, aID, err := resolveAssignee(ctx, client, "  Reviewer  ")
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
		_, _, err := resolveAssignee(ctx, client, "review")
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/workspaces/ws-1/members":
			json.NewEncoder(w).Encode(membersResp)
		case "/api/agents":
			json.NewEncoder(w).Encode(agentsResp)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := cli.NewAPIClient(srv.URL, "ws-1", "test-token")
	ctx := context.Background()

	t.Run("full UUID resolves agent", func(t *testing.T) {
		aType, aID, err := resolveAssignee(ctx, client, "f656eab8-1111-1111-1111-111111111111")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if aType != "agent" || aID != "f656eab8-1111-1111-1111-111111111111" {
			t.Errorf("got (%q, %q), want reviewer agent", aType, aID)
		}
	})

	t.Run("8-char ShortID resolves agent", func(t *testing.T) {
		aType, aID, err := resolveAssignee(ctx, client, "f656eab8")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if aType != "agent" || aID != "f656eab8-1111-1111-1111-111111111111" {
			t.Errorf("got (%q, %q), want reviewer agent", aType, aID)
		}
	})

	t.Run("uppercase ShortID still resolves", func(t *testing.T) {
		aType, aID, err := resolveAssignee(ctx, client, "F656EAB8")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if aType != "agent" || aID != "f656eab8-1111-1111-1111-111111111111" {
			t.Errorf("got (%q, %q), want reviewer agent", aType, aID)
		}
	})

	t.Run("ShortID resolves a member", func(t *testing.T) {
		aType, aID, err := resolveAssignee(ctx, client, "aaaaaaaa")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if aType != "member" || aID != "aaaaaaaa-1111-1111-1111-111111111111" {
			t.Errorf("got (%q, %q), want Alice", aType, aID)
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
				uType, uID, err := resolveAssignee(ctx, client, tt.user)
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

// runIssueTakeCmd executes `multica issue take` via the real rootCmd,
// against a stub server. takeFlags are inserted as `--<k>=<v>` on the
// command line so cobra parses + marks them Changed. Returns captured
// stdout and the cobra error.
//
// Sequential execution is required: rootCmd is package-global state.
// We reset the flags this command touches between calls because cobra
// keeps the previous Set value otherwise.
func runIssueTakeCmd(t *testing.T, serverURL string, posArgs []string, takeFlags map[string]string) (string, error) {
	t.Helper()

	for _, name := range []string{"provider", "print", "copy"} {
		if f := issueTakeCmd.Flags().Lookup(name); f != nil {
			_ = f.Value.Set(f.DefValue)
			f.Changed = false
		}
	}

	args := []string{
		"--server-url=" + serverURL,
		"--workspace-id=ws-1",
		"issue", "take",
	}
	args = append(args, posArgs...)
	for k, v := range takeFlags {
		if v == "true" {
			args = append(args, "--"+k)
		} else {
			args = append(args, "--"+k+"="+v)
		}
	}

	origStdout := os.Stdout
	r, wPipe, _ := os.Pipe()
	os.Stdout = wPipe

	rootCmd.SetArgs(args)
	err := rootCmd.Execute()

	wPipe.Close()
	os.Stdout = origStdout

	out, _ := io.ReadAll(r)
	return string(out), err
}

// TestResumeCommandForProvider locks the provider→CLI mapping for `issue take`.
// Drift here breaks the contract documented on the command — these are the
// commands users would otherwise have to assemble by hand.
func TestResumeCommandForProvider(t *testing.T) {
	const sid = "sess-123"
	cases := []struct {
		provider string
		wantBin  string
		wantArgs []string
	}{
		{"claude", "claude", []string{"--resume", sid}},
		{"codex", "codex", []string{"resume", sid}},
		{"cursor", "cursor-agent", []string{"--resume", sid}},
		{"gemini", "gemini", []string{"-r", sid}},
		{"opencode", "opencode", []string{"--session", sid}},
		{"copilot", "copilot", []string{"--resume", sid}},
		{"  Claude  ", "claude", []string{"--resume", sid}}, // case + whitespace tolerant
	}
	for _, tc := range cases {
		t.Run(tc.provider, func(t *testing.T) {
			r, err := resumeCommandForProvider(tc.provider, sid)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if r.Bin != tc.wantBin {
				t.Errorf("bin = %q, want %q", r.Bin, tc.wantBin)
			}
			if !equalStringSlice(r.Args, tc.wantArgs) {
				t.Errorf("args = %#v, want %#v", r.Args, tc.wantArgs)
			}
		})
	}

	t.Run("unknown provider returns error", func(t *testing.T) {
		if _, err := resumeCommandForProvider("openai-codex-2099", "sid"); err == nil {
			t.Fatal("expected error for unknown provider")
		}
	})
}

func TestShellSingleQuote(t *testing.T) {
	cases := map[string]string{
		"plain":               "'plain'",
		"":                    "''",
		"with spaces":         "'with spaces'",
		"it's-fine":           `'it'\''s-fine'`,
		"/tmp/multica/wd-1":   "'/tmp/multica/wd-1'",
		"$HOME/path":          "'$HOME/path'", // single quotes block expansion
		"a'b'c":               `'a'\''b'\''c'`,
	}
	for in, want := range cases {
		if got := shellSingleQuote(in); got != want {
			t.Errorf("shellSingleQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildShellResumeCommand(t *testing.T) {
	resume := providerResume{Bin: "claude", Args: []string{"--resume", "sess-1"}}

	t.Run("with workdir", func(t *testing.T) {
		got := buildShellResumeCommand("/tmp/wd", resume)
		want := `cd '/tmp/wd' && claude '--resume' 'sess-1'`
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("workdir with quote", func(t *testing.T) {
		got := buildShellResumeCommand("/tmp/it's/wd", resume)
		want := `cd '/tmp/it'\''s/wd' && claude '--resume' 'sess-1'`
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("empty workdir omits cd prefix", func(t *testing.T) {
		got := buildShellResumeCommand("", resume)
		want := `claude '--resume' 'sess-1'`
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}

func TestPickResumableRun(t *testing.T) {
	t.Run("picks first completed with session_id", func(t *testing.T) {
		runs := []map[string]any{
			{"status": "in_progress", "runtime_id": "rt-skip", "result": map[string]any{"session_id": "skip"}},
			{"status": "completed", "runtime_id": "rt-1", "result": map[string]any{"session_id": "sid-1", "work_dir": "/tmp/wd-1"}},
			{"status": "completed", "runtime_id": "rt-2", "result": map[string]any{"session_id": "sid-2"}},
		}
		sid, wd, rid, ok := pickResumableRun(runs)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if sid != "sid-1" || wd != "/tmp/wd-1" || rid != "rt-1" {
			t.Errorf("got (%q, %q, %q), want (sid-1, /tmp/wd-1, rt-1)", sid, wd, rid)
		}
	})

	t.Run("skips completed runs without session_id", func(t *testing.T) {
		runs := []map[string]any{
			{"status": "completed", "result": map[string]any{}},
			{"status": "completed", "runtime_id": "rt-2", "result": map[string]any{"session_id": "sid-2", "work_dir": "/tmp/wd-2"}},
		}
		sid, wd, rid, ok := pickResumableRun(runs)
		if !ok || sid != "sid-2" || wd != "/tmp/wd-2" || rid != "rt-2" {
			t.Errorf("got (%q, %q, %q, %v), want sid-2 run", sid, wd, rid, ok)
		}
	})

	t.Run("no completed runs returns ok=false", func(t *testing.T) {
		runs := []map[string]any{
			{"status": "in_progress", "result": map[string]any{"session_id": "s"}},
			{"status": "failed", "result": map[string]any{"session_id": "s"}},
		}
		_, _, _, ok := pickResumableRun(runs)
		if ok {
			t.Fatal("expected ok=false")
		}
	})

	t.Run("empty list returns ok=false", func(t *testing.T) {
		_, _, _, ok := pickResumableRun(nil)
		if ok {
			t.Fatal("expected ok=false on empty list")
		}
	})

	t.Run("missing work_dir is fine", func(t *testing.T) {
		runs := []map[string]any{
			{"status": "completed", "runtime_id": "rt-x", "result": map[string]any{"session_id": "sid-x"}},
		}
		sid, wd, rid, ok := pickResumableRun(runs)
		if !ok || sid != "sid-x" || wd != "" || rid != "rt-x" {
			t.Errorf("got (%q, %q, %q, %v)", sid, wd, rid, ok)
		}
	})
}

// TestIssueTakeProviderAutoDetect exercises the full take flow against a stub
// server: list runs → list runtimes → render command. --print is used so the
// test never spawns an actual agent CLI. The work_dir points at a real temp
// dir so the workdir-existence check does not strip the `cd` prefix.
func TestIssueTakeProviderAutoDetect(t *testing.T) {
	wd := t.TempDir()
	runs := []map[string]any{
		{
			"id":         "task-1",
			"status":     "completed",
			"runtime_id": "runtime-1",
			"result":     map[string]any{"session_id": "sid-abc", "work_dir": wd},
		},
	}
	runtimes := []map[string]any{
		{"id": "runtime-1", "provider": "claude"},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/issues/issue-1/task-runs":
			json.NewEncoder(w).Encode(runs)
		case "/api/runtimes":
			json.NewEncoder(w).Encode(runtimes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	out, err := runIssueTakeCmd(t, srv.URL, []string{"issue-1"}, map[string]string{"print": "true"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	got := strings.TrimSpace(out)
	want := "cd " + shellSingleQuote(wd) + " && claude '--resume' 'sid-abc'"
	if got != want {
		t.Errorf("printed command = %q, want %q", got, want)
	}
}

// TestIssueTakeNoCompletedRun verifies the spec error when no completed
// run is available.
func TestIssueTakeNoCompletedRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/issues/issue-1/task-runs":
			json.NewEncoder(w).Encode([]map[string]any{
				{"status": "in_progress", "result": map[string]any{"session_id": "x"}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	_, err := runIssueTakeCmd(t, srv.URL, []string{"issue-1"}, map[string]string{"print": "true"})
	if err == nil {
		t.Fatal("expected error when no completed run is available")
	}
	if !strings.Contains(err.Error(), "no completed run found") {
		t.Errorf("error = %v, want 'no completed run found'", err)
	}
}

// TestIssueTakePrintAndCopyMutex covers --print and --copy being mutually exclusive.
func TestIssueTakePrintAndCopyMutex(t *testing.T) {
	_, err := runIssueTakeCmd(t, "http://unused", []string{"issue-1"}, map[string]string{
		"print": "true",
		"copy":  "true",
	})
	if err == nil {
		t.Fatal("expected error when both --print and --copy are set")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("error = %v, want 'mutually exclusive'", err)
	}
}

// TestIssueTakeUnsupportedProvider ensures we exit non-zero (with a helpful
// message) when the runtime's provider is not in the take map.
func TestIssueTakeUnsupportedProvider(t *testing.T) {
	runs := []map[string]any{
		{
			"id":         "task-1",
			"status":     "completed",
			"runtime_id": "runtime-1",
			"result":     map[string]any{"session_id": "sid-abc", "work_dir": "/tmp/wd"},
		},
	}
	runtimes := []map[string]any{
		{"id": "runtime-1", "provider": "kimi"},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/issues/issue-1/task-runs":
			json.NewEncoder(w).Encode(runs)
		case "/api/runtimes":
			json.NewEncoder(w).Encode(runtimes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	_, err := runIssueTakeCmd(t, srv.URL, []string{"issue-1"}, map[string]string{"print": "true"})
	if err == nil {
		t.Fatal("expected error for unsupported provider")
	}
	if !strings.Contains(err.Error(), "unsupported provider") {
		t.Errorf("error = %v, want 'unsupported provider'", err)
	}
}

// TestIssueTakeRuntimeNotFound covers the case where the task references a
// runtime that no longer appears in /api/runtimes.
func TestIssueTakeRuntimeNotFound(t *testing.T) {
	runs := []map[string]any{
		{
			"id":         "task-1",
			"status":     "completed",
			"runtime_id": "runtime-missing",
			"result":     map[string]any{"session_id": "sid-abc", "work_dir": "/tmp/wd"},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/issues/issue-1/task-runs":
			json.NewEncoder(w).Encode(runs)
		case "/api/runtimes":
			json.NewEncoder(w).Encode([]map[string]any{})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	_, err := runIssueTakeCmd(t, srv.URL, []string{"issue-1"}, map[string]string{"print": "true"})
	if err == nil {
		t.Fatal("expected error when runtime is missing")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %v, want 'not found'", err)
	}
}

// TestIssueTakeProviderOverride checks that --provider skips the runtime
// lookup entirely (and therefore works even when /api/runtimes is broken).
func TestIssueTakeProviderOverride(t *testing.T) {
	wd := t.TempDir()
	runs := []map[string]any{
		{
			"id":         "task-1",
			"status":     "completed",
			"runtime_id": "runtime-1",
			"result":     map[string]any{"session_id": "sid-abc", "work_dir": wd},
		},
	}
	hitRuntimes := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/issues/issue-1/task-runs":
			json.NewEncoder(w).Encode(runs)
		case "/api/runtimes":
			hitRuntimes = true
			http.Error(w, "boom", 500)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	out, err := runIssueTakeCmd(t, srv.URL, []string{"issue-1"}, map[string]string{
		"provider": "codex",
		"print":    "true",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if hitRuntimes {
		t.Error("--provider should bypass /api/runtimes lookup")
	}
	got := strings.TrimSpace(out)
	want := "cd " + shellSingleQuote(wd) + " && codex 'resume' 'sid-abc'"
	if got != want {
		t.Errorf("printed command = %q, want %q", got, want)
	}
}

func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
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

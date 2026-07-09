package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// newWorkspaceSwitchTestCmd builds a standalone cobra command with the flags
// runWorkspaceSwitch reads. We can't reuse the real workspaceSwitchCmd because
// it has no parent root carrying --workspace-id / --profile / --server-url.
func newWorkspaceSwitchTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "switch"}
	cmd.Flags().String("workspace-id", "", "")
	cmd.Flags().String("profile", "", "")
	cmd.Flags().String("server-url", "", "")
	return cmd
}

func TestRunWorkspaceSwitch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/workspaces" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode([]map[string]any{
			{"id": "11111111-1111-1111-1111-111111111111", "name": "Alpha", "slug": "alpha"},
			{"id": "22222222-2222-2222-2222-222222222222", "name": "Beta", "slug": "beta"},
		})
	}))
	defer srv.Close()

	// Isolate HOME so the test never touches the developer's ~/.multica.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_TOKEN", "test-token")
	t.Setenv("MULTICA_WORKSPACE_ID", "")

	t.Run("switches by slug and persists workspace_id", func(t *testing.T) {
		cmd := newWorkspaceSwitchTestCmd()
		if err := runWorkspaceSwitch(cmd, []string{"beta"}); err != nil {
			t.Fatalf("runWorkspaceSwitch: %v", err)
		}
		cfg, err := cli.LoadCLIConfig()
		if err != nil {
			t.Fatalf("LoadCLIConfig: %v", err)
		}
		if cfg.WorkspaceID != "22222222-2222-2222-2222-222222222222" {
			t.Errorf("workspace_id = %q, want Beta's id", cfg.WorkspaceID)
		}
	})

	t.Run("rejects unknown workspace and leaves config untouched", func(t *testing.T) {
		// Seed a known workspace_id so we can verify it is NOT clobbered on
		// failure — the issue's acceptance criteria explicitly call this out.
		if err := cli.SaveCLIConfig(cli.CLIConfig{WorkspaceID: "11111111-1111-1111-1111-111111111111"}); err != nil {
			t.Fatalf("seed config: %v", err)
		}

		cmd := newWorkspaceSwitchTestCmd()
		err := runWorkspaceSwitch(cmd, []string{"does-not-exist"})
		if err == nil {
			t.Fatal("expected error for unknown workspace")
		}

		cfg, _ := cli.LoadCLIConfig()
		if cfg.WorkspaceID != "11111111-1111-1111-1111-111111111111" {
			t.Errorf("workspace_id = %q, expected it to stay on Alpha's id when switch fails", cfg.WorkspaceID)
		}
	})

	t.Run("isolates by profile", func(t *testing.T) {
		cmd := newWorkspaceSwitchTestCmd()
		_ = cmd.Flags().Set("profile", "staging")
		if err := runWorkspaceSwitch(cmd, []string{"alpha"}); err != nil {
			t.Fatalf("runWorkspaceSwitch: %v", err)
		}

		// The staging profile picked up Alpha; the default profile (touched
		// earlier in this test) must remain unaffected.
		stagingCfg, err := cli.LoadCLIConfigForProfile("staging")
		if err != nil {
			t.Fatalf("load staging config: %v", err)
		}
		if stagingCfg.WorkspaceID != "11111111-1111-1111-1111-111111111111" {
			t.Errorf("staging workspace_id = %q, want Alpha's id", stagingCfg.WorkspaceID)
		}

		// Verify the staging profile config landed in the expected path.
		path, _ := cli.CLIConfigPathForProfile("staging")
		wantSuffix := filepath.Join(".multica", "profiles", "staging", "config.json")
		if !strings.HasSuffix(path, wantSuffix) {
			t.Errorf("staging config path = %q, want suffix %q", path, wantSuffix)
		}
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected staging config file at %s, got %v", path, err)
		}
	})
}

func TestResolveWorkspaceByIDOrSlug(t *testing.T) {
	workspaces := []workspaceSummary{
		{ID: "11111111-1111-1111-1111-111111111111", Name: "Alpha", Slug: "alpha"},
		{ID: "22222222-2222-2222-2222-222222222222", Name: "Beta", Slug: "beta"},
	}

	t.Run("matches by exact UUID", func(t *testing.T) {
		ws, err := resolveWorkspaceByIDOrSlug(workspaces, "22222222-2222-2222-2222-222222222222")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ws.Name != "Beta" {
			t.Errorf("got %q, want Beta", ws.Name)
		}
	})

	t.Run("matches by slug", func(t *testing.T) {
		ws, err := resolveWorkspaceByIDOrSlug(workspaces, "alpha")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ws.ID != "11111111-1111-1111-1111-111111111111" {
			t.Errorf("got id %q, want alpha's id", ws.ID)
		}
	})

	t.Run("slug match is case-insensitive", func(t *testing.T) {
		ws, err := resolveWorkspaceByIDOrSlug(workspaces, "ALPHA")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ws.Slug != "alpha" {
			t.Errorf("got %q, want alpha", ws.Slug)
		}
	})

	t.Run("unknown target returns access-style error", func(t *testing.T) {
		_, err := resolveWorkspaceByIDOrSlug(workspaces, "gamma")
		if err == nil {
			t.Fatal("expected error for unknown workspace")
		}
		// The error should hint at running 'workspace list' so the user has an
		// actionable next step. We treat it as a soft contract because it is
		// the message users see when they typo a slug.
		if !strings.Contains(err.Error(), "workspace list") {
			t.Errorf("error %q should reference 'workspace list'", err)
		}
	})

	t.Run("empty target is rejected", func(t *testing.T) {
		_, err := resolveWorkspaceByIDOrSlug(workspaces, "   ")
		if err == nil {
			t.Fatal("expected error for empty target")
		}
	})

	t.Run("whitespace-padded target is trimmed", func(t *testing.T) {
		ws, err := resolveWorkspaceByIDOrSlug(workspaces, "  beta  ")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ws.Name != "Beta" {
			t.Errorf("got %q, want Beta", ws.Name)
		}
	})

	t.Run("matches unique short UUID prefix", func(t *testing.T) {
		ws, err := resolveWorkspaceByIDOrSlug(workspaces, "2222")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ws.Name != "Beta" {
			t.Errorf("got %q, want Beta", ws.Name)
		}
	})

	t.Run("short UUID prefix with dashes is accepted", func(t *testing.T) {
		ws, err := resolveWorkspaceByIDOrSlug(workspaces, "1111-11")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ws.Name != "Alpha" {
			t.Errorf("got %q, want Alpha", ws.Name)
		}
	})

	t.Run("ambiguous prefix lists all matches", func(t *testing.T) {
		ambiguous := []workspaceSummary{
			{ID: "ab123456-0000-0000-0000-000000000001", Name: "First", Slug: "first"},
			{ID: "ab129999-0000-0000-0000-000000000002", Name: "Second", Slug: "second"},
		}
		_, err := resolveWorkspaceByIDOrSlug(ambiguous, "ab12")
		if err == nil {
			t.Fatal("expected ambiguous prefix error")
		}
		if !strings.Contains(err.Error(), "ambiguous") {
			t.Errorf("error = %q, want it to mention 'ambiguous'", err)
		}
		// Both candidate IDs must surface so the user can disambiguate without
		// re-running `workspace list`.
		if !strings.Contains(err.Error(), "ab123456") || !strings.Contains(err.Error(), "ab129999") {
			t.Errorf("error = %q, want both candidate IDs", err)
		}
	})

	t.Run("slug wins over colliding UUID prefix", func(t *testing.T) {
		// If a workspace's slug equals another workspace's UUID prefix, the
		// slug must take priority — that's the value users actually see in
		// `workspace list`.
		collision := []workspaceSummary{
			{ID: "deadbeef-0000-0000-0000-000000000001", Name: "Hex", Slug: "hex"},
			{ID: "feedface-0000-0000-0000-000000000002", Name: "Decoy", Slug: "deadbeef"},
		}
		ws, err := resolveWorkspaceByIDOrSlug(collision, "deadbeef")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ws.Name != "Decoy" {
			t.Errorf("got %q, want Decoy (slug match should beat UUID prefix)", ws.Name)
		}
	})

	t.Run("non-hex unknown target falls through to not-found", func(t *testing.T) {
		// 'gamma' has letters outside the hex range, so it cannot reach the
		// prefix branch — must surface the not-found error pointing the user
		// at `workspace list`.
		_, err := resolveWorkspaceByIDOrSlug(workspaces, "gamma")
		if err == nil {
			t.Fatal("expected error for unknown workspace")
		}
		if !strings.Contains(err.Error(), "workspace list") {
			t.Errorf("error = %q, want it to reference 'workspace list'", err)
		}
	})

	t.Run("prefix shorter than 4 hex chars is rejected", func(t *testing.T) {
		// Too-short prefixes would collide with random hex substrings; the
		// resolver must surface the not-found error rather than silently
		// returning a wrong workspace.
		_, err := resolveWorkspaceByIDOrSlug(workspaces, "11")
		if err == nil {
			t.Fatal("expected error for 2-char prefix")
		}
		if !strings.Contains(err.Error(), "workspace list") {
			t.Errorf("error = %q, want it to reference 'workspace list'", err)
		}
	})
}

// resetWorkspaceUpdateFlags clears every flag on workspaceUpdateCmd and marks
// each as not-Changed. The cobra.Command instance is a process-wide singleton,
// so previous subtests leak state into the next one without this guard.
func resetWorkspaceUpdateFlags(t *testing.T) {
	t.Helper()
	flags := workspaceUpdateCmd.Flags()
	for _, name := range []string{"name", "description", "context", "issue-prefix"} {
		_ = flags.Set(name, "")
		if f := flags.Lookup(name); f != nil {
			f.Changed = false
		}
	}
	for _, name := range []string{"description-stdin", "context-stdin"} {
		_ = flags.Set(name, "false")
		if f := flags.Lookup(name); f != nil {
			f.Changed = false
		}
	}
}

func setStringFlag(t *testing.T, name, value string) {
	t.Helper()
	if err := workspaceUpdateCmd.Flags().Set(name, value); err != nil {
		t.Fatalf("set --%s: %v", name, err)
	}
}

func setBoolFlag(t *testing.T, name string, value bool) {
	t.Helper()
	v := "false"
	if value {
		v = "true"
	}
	if err := workspaceUpdateCmd.Flags().Set(name, v); err != nil {
		t.Fatalf("set --%s: %v", name, err)
	}
}

func TestBuildWorkspaceUpdateBody(t *testing.T) {
	t.Run("only changed flags appear in body", func(t *testing.T) {
		resetWorkspaceUpdateFlags(t)
		setStringFlag(t, "name", "Acme Eng")

		body, err := buildWorkspaceUpdateBody(workspaceUpdateCmd)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if got, _ := body["name"].(string); got != "Acme Eng" {
			t.Errorf("name = %v, want Acme Eng", body["name"])
		}
		for _, key := range []string{"description", "context", "issue_prefix"} {
			if _, present := body[key]; present {
				t.Errorf("%s should not appear when its flag was not set, got %v", key, body)
			}
		}
	})

	t.Run("multiple fields combine into one PATCH body", func(t *testing.T) {
		resetWorkspaceUpdateFlags(t)
		setStringFlag(t, "name", "Acme")
		setStringFlag(t, "description", `line1\nline2`)
		setStringFlag(t, "issue-prefix", "ENG")

		body, err := buildWorkspaceUpdateBody(workspaceUpdateCmd)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if body["name"] != "Acme" {
			t.Errorf("name = %v, want Acme", body["name"])
		}
		// resolveTextFlag decodes \n in inline values.
		if body["description"] != "line1\nline2" {
			t.Errorf("description = %q, want decoded newline", body["description"])
		}
		if body["issue_prefix"] != "ENG" {
			t.Errorf("issue_prefix = %v, want ENG", body["issue_prefix"])
		}
	})

	t.Run("inline + stdin is rejected for description", func(t *testing.T) {
		resetWorkspaceUpdateFlags(t)
		setStringFlag(t, "description", "inline")
		setBoolFlag(t, "description-stdin", true)

		if _, err := buildWorkspaceUpdateBody(workspaceUpdateCmd); err == nil {
			t.Fatalf("expected mutually-exclusive error for --description and --description-stdin")
		}
	})

	t.Run("context-stdin reads from stdin", func(t *testing.T) {
		resetWorkspaceUpdateFlags(t)
		setBoolFlag(t, "context-stdin", true)

		stdinBody := "first\nsecond line with literal \\n\n"
		var got map[string]any
		pipeStdin(t, stdinBody, func() {
			b, err := buildWorkspaceUpdateBody(workspaceUpdateCmd)
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			got = b
		})
		want := "first\nsecond line with literal \\n"
		if got["context"] != want {
			t.Errorf("context = %q, want %q", got["context"], want)
		}
	})

	t.Run("empty issue-prefix is rejected", func(t *testing.T) {
		resetWorkspaceUpdateFlags(t)
		setStringFlag(t, "issue-prefix", "")
		// Force Changed=true so the flag is treated as "explicitly passed".
		if f := workspaceUpdateCmd.Flags().Lookup("issue-prefix"); f != nil {
			f.Changed = true
		}

		_, err := buildWorkspaceUpdateBody(workspaceUpdateCmd)
		if err == nil {
			t.Fatalf("expected error when --issue-prefix is empty")
		}
		if !strings.Contains(err.Error(), "cannot be empty") {
			t.Errorf("error = %q, want it to mention 'cannot be empty'", err)
		}
	})

	t.Run("whitespace-only issue-prefix is rejected", func(t *testing.T) {
		resetWorkspaceUpdateFlags(t)
		setStringFlag(t, "issue-prefix", "   ")
		if f := workspaceUpdateCmd.Flags().Lookup("issue-prefix"); f != nil {
			f.Changed = true
		}
		if _, err := buildWorkspaceUpdateBody(workspaceUpdateCmd); err == nil {
			t.Fatalf("expected error when --issue-prefix is whitespace-only")
		}
	})

	t.Run("no flags set produces empty body", func(t *testing.T) {
		resetWorkspaceUpdateFlags(t)
		body, err := buildWorkspaceUpdateBody(workspaceUpdateCmd)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if len(body) != 0 {
			t.Errorf("body = %v, want empty", body)
		}
	})
}

func newWorkspaceMemberInviteTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "invite"}
	cmd.Flags().String("server-url", "", "")
	cmd.Flags().String("workspace-id", "", "")
	cmd.Flags().String("profile", "", "")
	cmd.Flags().String("role", "member", "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

func TestWorkspaceMemberInviteCommandIsRegistered(t *testing.T) {
	cmd, _, err := workspaceMemberCmd.Find([]string{"invite", "alice@example.com"})
	if err != nil {
		t.Fatalf("find invite command: %v", err)
	}
	if cmd == nil || cmd.Name() != "invite" {
		t.Fatalf("invite command not registered; got %#v", cmd)
	}
	for _, flag := range []string{"role", "output"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Fatalf("invite command missing --%s flag", flag)
		}
	}
}

func TestRunWorkspaceMemberInvitePostsInvitation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_TOKEN", "test-token")
	t.Setenv("MULTICA_WORKSPACE_ID", "workspace-123")

	var gotMethod, gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if r.Header.Get("X-Workspace-ID") != "workspace-123" {
			t.Fatalf("X-Workspace-ID = %q, want workspace-123", r.Header.Get("X-Workspace-ID"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"invitee_email": "alice@example.com",
			"role":          "member",
			"status":        "pending",
		})
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)

	cmd := newWorkspaceMemberInviteTestCmd()
	// A mixed-case email should be lowercased before it is sent.
	if err := runWorkspaceMemberInvite(cmd, []string{"Alice@Example.com"}); err != nil {
		t.Fatalf("runWorkspaceMemberInvite: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("method = %s, want POST", gotMethod)
	}
	if gotPath != "/api/workspaces/workspace-123/members" {
		t.Fatalf("path = %q, want /api/workspaces/workspace-123/members", gotPath)
	}
	if gotBody["email"] != "alice@example.com" {
		t.Fatalf("body email = %v, want alice@example.com", gotBody["email"])
	}
	if gotBody["role"] != "member" {
		t.Fatalf("body role = %v, want member (default)", gotBody["role"])
	}
}

func TestRunWorkspaceMemberInviteUsesWorkspaceArgAndRoleFlag(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_TOKEN", "test-token")

	const wsUUID = "11111111-1111-1111-1111-111111111111"
	var gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"invitee_email": "bob@example.com", "role": "admin", "status": "pending"})
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)

	cmd := newWorkspaceMemberInviteTestCmd()
	_ = cmd.Flags().Set("role", "admin")
	// A full UUID positional is forwarded as-is, without a /api/workspaces lookup.
	if err := runWorkspaceMemberInvite(cmd, []string{"bob@example.com", wsUUID}); err != nil {
		t.Fatalf("runWorkspaceMemberInvite: %v", err)
	}
	if gotPath != "/api/workspaces/"+wsUUID+"/members" {
		t.Fatalf("path = %q, want /api/workspaces/%s/members", gotPath, wsUUID)
	}
	if gotBody["role"] != "admin" {
		t.Fatalf("body role = %v, want admin", gotBody["role"])
	}
}

func TestRunWorkspaceMemberInviteRejectsOwnerRole(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_TOKEN", "test-token")
	t.Setenv("MULTICA_WORKSPACE_ID", "workspace-123")

	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)

	cmd := newWorkspaceMemberInviteTestCmd()
	_ = cmd.Flags().Set("role", "owner")
	if err := runWorkspaceMemberInvite(cmd, []string{"alice@example.com"}); err == nil {
		t.Fatal("expected error for --role owner, got nil")
	}
	if called {
		t.Fatal("owner role should be rejected client-side without an HTTP call")
	}
}

func TestRunWorkspaceMemberInviteRejectsUnknownRole(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_TOKEN", "test-token")
	t.Setenv("MULTICA_WORKSPACE_ID", "workspace-123")

	cmd := newWorkspaceMemberInviteTestCmd()
	_ = cmd.Flags().Set("role", "superuser")
	if err := runWorkspaceMemberInvite(cmd, []string{"alice@example.com"}); err == nil {
		t.Fatal("expected error for unknown --role, got nil")
	}
}

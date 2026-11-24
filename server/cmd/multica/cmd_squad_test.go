package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func newSquadMemberSetRoleTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "set-role"}
	cmd.Flags().String("server-url", "", "")
	cmd.Flags().String("workspace-id", "", "")
	cmd.Flags().String("profile", "", "")
	cmd.Flags().String("member-id", "", "")
	cmd.Flags().String("member-type", "agent", "")
	cmd.Flags().String("role", "", "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

func TestRunSquadMemberSetRolePatchesRole(t *testing.T) {
	redirectTestHome(t, t.TempDir())
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
			"squad_id":    "squad-123",
			"member_id":   "member-456",
			"member_type": "agent",
			"role":        "reviewer",
		})
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)

	cmd := newSquadMemberSetRoleTestCmd()
	_ = cmd.Flags().Set("member-id", "member-456")
	_ = cmd.Flags().Set("member-type", "agent")
	_ = cmd.Flags().Set("role", "reviewer")
	_ = cmd.Flags().Set("output", "json")

	if err := runSquadMemberSetRole(cmd, []string{"squad-123"}); err != nil {
		t.Fatalf("runSquadMemberSetRole: %v", err)
	}
	if gotMethod != http.MethodPatch {
		t.Fatalf("method = %s, want PATCH", gotMethod)
	}
	if gotPath != "/api/squads/squad-123/members/role" {
		t.Fatalf("path = %q, want /api/squads/squad-123/members/role", gotPath)
	}
	wantBody := map[string]any{"member_id": "member-456", "member_type": "agent", "role": "reviewer"}
	for k, want := range wantBody {
		if gotBody[k] != want {
			t.Fatalf("body[%s] = %v, want %v (full body: %#v)", k, gotBody[k], want, gotBody)
		}
	}
}

func newSquadTestCmd(use string, flags map[string]string) *cobra.Command {
	cmd := &cobra.Command{Use: use}
	for _, name := range []string{"server-url", "workspace-id", "profile"} {
		cmd.Flags().String(name, "", "")
	}
	for name, def := range flags {
		cmd.Flags().String(name, def, "")
	}
	return cmd
}

func squadTestServer(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	redirectTestHome(t, t.TempDir())
	t.Setenv("MULTICA_TOKEN", "test-token")
	t.Setenv("MULTICA_WORKSPACE_ID", "workspace-123")
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
}

func newSquadUpdateTestCmd() *cobra.Command {
	return newSquadTestCmd("update", map[string]string{
		"name": "", "description": "", "instructions": "", "leader": "", "avatar-url": "",
		"handoff-mode": "", "output": "json",
	})
}

func TestRunSquadUpdateSendsHandoffMode(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	squadTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "squad-123", "name": "Delivery", "handoff_mode": "direct"})
	})

	cmd := newSquadUpdateTestCmd()
	_ = cmd.Flags().Set("handoff-mode", "direct")
	if _, err := captureStdout(t, func() error { return runSquadUpdate(cmd, []string{"squad-123"}) }); err != nil {
		t.Fatalf("runSquadUpdate: %v", err)
	}
	if gotMethod != http.MethodPut || gotPath != "/api/squads/squad-123" {
		t.Fatalf("request = %s %s, want PUT /api/squads/squad-123", gotMethod, gotPath)
	}
	if len(gotBody) != 1 || gotBody["handoff_mode"] != "direct" {
		t.Fatalf("body = %#v, want only handoff_mode=direct", gotBody)
	}
}

func TestRunSquadUpdateRejectsUnknownHandoffMode(t *testing.T) {
	squadTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("an invalid mode must not reach the server: %s %s", r.Method, r.URL.Path)
	})
	cmd := newSquadUpdateTestCmd()
	_ = cmd.Flags().Set("handoff-mode", "round_robin")
	if err := runSquadUpdate(cmd, []string{"squad-123"}); err == nil || !strings.Contains(err.Error(), "--handoff-mode") {
		t.Fatalf("err = %v, want a --handoff-mode validation error", err)
	}
}

func TestRunSquadBriefs(t *testing.T) {
	const memberID = "11111111-1111-4111-8111-111111111111"
	squadTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/squads/squad-123/briefs" {
			t.Fatalf("request = %s %s, want GET /api/squads/squad-123/briefs", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"handoff_mode": "direct",
			"briefs": []map[string]any{
				{"member_type": "agent", "member_id": "22222222-2222-4222-8222-222222222222", "is_leader": true, "brief": "## Squad Operating Protocol"},
				{"member_type": "agent", "member_id": memberID, "is_leader": false, "brief": "## Squad Member Brief"},
			},
		})
	})
	newCmd := func() *cobra.Command { return newSquadTestCmd("briefs", map[string]string{"agent": "", "output": "table"}) }

	t.Run("table prints every agent", func(t *testing.T) {
		out, err := captureStdout(t, func() error { return runSquadBriefs(newCmd(), []string{"squad-123"}) })
		if err != nil {
			t.Fatalf("runSquadBriefs: %v", err)
		}
		if !strings.Contains(out, "(leader)\n## Squad Operating Protocol") || !strings.Contains(out, memberID+" (member)\n## Squad Member Brief") {
			t.Fatalf("output = %q", out)
		}
	})

	t.Run("agent filter and json", func(t *testing.T) {
		cmd := newCmd()
		_ = cmd.Flags().Set("agent", memberID)
		_ = cmd.Flags().Set("output", "json")
		out, err := captureStdout(t, func() error { return runSquadBriefs(cmd, []string{"squad-123"}) })
		if err != nil {
			t.Fatalf("runSquadBriefs: %v", err)
		}
		var got struct {
			HandoffMode string           `json:"handoff_mode"`
			Briefs      []map[string]any `json:"briefs"`
		}
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("decode %q: %v", out, err)
		}
		if got.HandoffMode != "direct" || len(got.Briefs) != 1 || got.Briefs[0]["member_id"] != memberID {
			t.Fatalf("json = %+v", got)
		}
	})

	t.Run("agent outside the squad", func(t *testing.T) {
		cmd := newCmd()
		_ = cmd.Flags().Set("agent", "33333333-3333-4333-8333-333333333333")
		if err := runSquadBriefs(cmd, []string{"squad-123"}); err == nil {
			t.Fatal("expected an error for an agent that is not a squad member")
		}
	})
}

func TestSquadHandoffModeDisplayDefaultsToLeader(t *testing.T) {
	if got := squadHandoffModeDisplay(map[string]any{"id": "squad-123"}); got != "leader" {
		t.Fatalf("missing handoff_mode = %q, want leader", got)
	}
	if got := squadHandoffModeDisplay(map[string]any{"handoff_mode": "direct"}); got != "direct" {
		t.Fatalf("handoff_mode = %q, want direct", got)
	}
}

func TestRunSquadMemberSetRoleValidatesRequiredFlags(t *testing.T) {
	cmd := newSquadMemberSetRoleTestCmd()
	if err := runSquadMemberSetRole(cmd, []string{"squad-123"}); err == nil {
		t.Fatal("expected missing --member-id error")
	}

	cmd = newSquadMemberSetRoleTestCmd()
	_ = cmd.Flags().Set("member-id", "member-456")
	_ = cmd.Flags().Set("member-type", "invalid")
	if err := runSquadMemberSetRole(cmd, []string{"squad-123"}); err == nil {
		t.Fatal("expected invalid --member-type error")
	}

	cmd = newSquadMemberSetRoleTestCmd()
	_ = cmd.Flags().Set("member-id", "member-456")
	if err := runSquadMemberSetRole(cmd, []string{"squad-123"}); err == nil {
		t.Fatal("expected missing --role error")
	}
}

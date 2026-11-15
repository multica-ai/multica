package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func newProjectCreateVisibilityTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "create"}
	for _, name := range []string{"title", "description", "status", "icon", "lead", "start-date", "due-date", "visibility"} {
		cmd.Flags().String(name, "", "")
	}
	cmd.Flags().StringArray("repo", nil, "")
	cmd.Flags().StringArray("member", nil, "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

func TestRunProjectCreateSendsVisibilityAndMembers(t *testing.T) {
	const memberID = "33333333-3333-3333-3333-333333333333"
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/projects" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		json.NewEncoder(w).Encode(map[string]any{"id": "p-1", "title": body["title"], "visibility": body["visibility"]})
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newProjectCreateVisibilityTestCmd()
	_ = cmd.Flags().Set("title", "Client A")
	_ = cmd.Flags().Set("visibility", "private")
	_ = cmd.Flags().Set("member", memberID)
	if err := runProjectCreate(cmd, nil); err != nil {
		t.Fatalf("runProjectCreate: %v", err)
	}
	if body["visibility"] != "private" {
		t.Fatalf("visibility = %#v", body["visibility"])
	}
	ids, _ := body["member_ids"].([]any)
	if len(ids) != 1 || ids[0] != memberID {
		t.Fatalf("member_ids = %#v", body["member_ids"])
	}
}

func TestProjectVisibilityFlagValidation(t *testing.T) {
	for _, tc := range []struct {
		visibility string
		member     string
		wantErr    string
	}{
		{"secret", "", "invalid visibility"},
		{"", "33333333-3333-3333-3333-333333333333", "--member requires --visibility private"},
		{"workspace", "33333333-3333-3333-3333-333333333333", "--member requires --visibility private"},
	} {
		cmd := newProjectCreateVisibilityTestCmd()
		if tc.visibility != "" {
			_ = cmd.Flags().Set("visibility", tc.visibility)
		}
		if tc.member != "" {
			_ = cmd.Flags().Set("member", tc.member)
		}
		err := applyProjectVisibilityFlags(t.Context(), cmd, nil, map[string]any{})
		if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("visibility=%q member=%q: err = %v, want %q", tc.visibility, tc.member, err, tc.wantErr)
		}
	}
}

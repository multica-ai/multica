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

type projectUpdateMemberCalls struct {
	putBody     map[string]any
	addedIDs    []any
	putCalled   bool
	postCalled  bool
	addRejected bool
}

func newProjectUpdateMemberServer(t *testing.T, projectID, currentVisibility string, calls *projectUpdateMemberCalls) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/projects/"+projectID && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"id": projectID, "title": "Client A", "visibility": currentVisibility})
		case r.URL.Path == "/api/projects/"+projectID && r.Method == http.MethodPut:
			calls.putCalled = true
			if err := json.NewDecoder(r.Body).Decode(&calls.putBody); err != nil {
				t.Errorf("decode put body: %v", err)
			}
			json.NewEncoder(w).Encode(map[string]any{"id": projectID, "title": "Client A", "visibility": "private"})
		case r.URL.Path == "/api/projects/"+projectID+"/members" && r.Method == http.MethodPost:
			calls.postCalled = true
			if calls.addRejected {
				w.WriteHeader(http.StatusForbidden)
				json.NewEncoder(w).Encode(map[string]any{"error": "only workspace admins and the member who manages this project's access can add members"})
				return
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode post body: %v", err)
			}
			calls.addedIDs, _ = body["user_ids"].([]any)
			json.NewEncoder(w).Encode(map[string]any{"members": []any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")
	return srv
}

func newProjectUpdateMemberCmd(memberID string) *cobra.Command {
	cmd := newProjectCreateVisibilityTestCmd()
	_ = cmd.Flags().Set("visibility", "private")
	_ = cmd.Flags().Set("member", memberID)
	return cmd
}

func TestRunProjectUpdateAddsMembersToAlreadyPrivateProject(t *testing.T) {
	const projectID = "11111111-1111-1111-1111-111111111111"
	const memberID = "33333333-3333-3333-3333-333333333333"
	calls := &projectUpdateMemberCalls{}
	newProjectUpdateMemberServer(t, projectID, "private", calls)

	if err := runProjectUpdate(newProjectUpdateMemberCmd(memberID), []string{projectID}); err != nil {
		t.Fatalf("runProjectUpdate: %v", err)
	}
	if len(calls.addedIDs) != 1 || calls.addedIDs[0] != memberID {
		t.Fatalf("members added = %#v, want [%s]", calls.addedIDs, memberID)
	}
	if _, ok := calls.putBody["member_ids"]; ok {
		t.Fatalf("PUT body still carries member_ids: %#v", calls.putBody)
	}
}

func TestRunProjectUpdateSendsMembersWhenSwitchingToPrivate(t *testing.T) {
	const projectID = "11111111-1111-1111-1111-111111111111"
	const memberID = "33333333-3333-3333-3333-333333333333"
	calls := &projectUpdateMemberCalls{}
	newProjectUpdateMemberServer(t, projectID, "workspace", calls)

	if err := runProjectUpdate(newProjectUpdateMemberCmd(memberID), []string{projectID}); err != nil {
		t.Fatalf("runProjectUpdate: %v", err)
	}
	if calls.postCalled {
		t.Fatalf("member add endpoint called while switching visibility")
	}
	ids, _ := calls.putBody["member_ids"].([]any)
	if len(ids) != 1 || ids[0] != memberID {
		t.Fatalf("member_ids = %#v", calls.putBody["member_ids"])
	}
}

func TestRunProjectUpdateFailsWhenMemberAddIsRejected(t *testing.T) {
	const projectID = "11111111-1111-1111-1111-111111111111"
	calls := &projectUpdateMemberCalls{addRejected: true}
	newProjectUpdateMemberServer(t, projectID, "private", calls)

	err := runProjectUpdate(newProjectUpdateMemberCmd("33333333-3333-3333-3333-333333333333"), []string{projectID})
	if err == nil || !strings.Contains(err.Error(), "add project members") {
		t.Fatalf("err = %v, want an add project members error", err)
	}
	if calls.putCalled {
		t.Fatalf("project updated after the member add was rejected")
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

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIssueCreateRegistersAssigneePickerFlag(t *testing.T) {
	flag := issueCreateCmd.Flags().Lookup("assignee-picker")
	if flag == nil || flag.Value.Type() != "bool" {
		t.Fatal("issue create must expose the boolean --assignee-picker flag")
	}
}

func TestRunIssueCreatePickerAssignee(t *testing.T) {
	t.Chdir(t.TempDir())
	const (
		taskID     = "cccc1111-2222-3333-4444-555566667777"
		agentID    = "282bdf4e-b502-4ca9-8fae-0c842f973e63"
		squadID    = "aaaa1111-2222-3333-4444-555555555555"
		explicitID = "bbbb1111-2222-3333-4444-666666666666"
	)
	for _, tc := range []struct {
		name, taskID, pickerType, pickerID string
		args                               []string
		wantType, wantID, wantErr          string
		serverStatus                       int
	}{
		{name: "agent picker", taskID: taskID, pickerType: "agent", pickerID: agentID, args: []string{"--assignee-picker"}, wantType: "agent", wantID: agentID},
		{name: "squad picker", taskID: taskID, pickerType: "squad", pickerID: squadID, args: []string{"--assignee-picker"}, wantType: "squad", wantID: squadID},
		{name: "unrecognized assignee omits ownership", taskID: taskID, pickerType: "agent", pickerID: agentID},
		{name: "ordinary create ignores picker environment", pickerType: "agent", pickerID: agentID},
		{name: "false picker does not assign", taskID: taskID, pickerType: "agent", pickerID: agentID, args: []string{"--assignee-picker=false"}},
		{name: "explicit assignee keeps precedence", taskID: taskID, pickerType: "squad", pickerID: squadID, args: []string{"--assignee-id", explicitID}, wantType: "agent", wantID: explicitID},
		{name: "explicit empty ID still fails", taskID: taskID, pickerType: "agent", pickerID: agentID, args: []string{"--assignee-id="}, wantErr: "canonical UUID"},
		{name: "explicit invalid ID still fails", taskID: taskID, pickerType: "agent", pickerID: agentID, args: []string{"--assignee-id", "282df4e-b502-4ca9-8fae-0c842f973e63"}, wantErr: "canonical UUID"},
		{name: "picker conflicts with name", args: []string{"--assignee-picker", "--assignee", "someone"}, wantErr: "mutually exclusive"},
		{name: "picker conflicts with ID", args: []string{"--assignee-picker", "--assignee-id", explicitID}, wantErr: "mutually exclusive"},
		{name: "picker conflicts with empty ID", args: []string{"--assignee-picker", "--assignee-id="}, wantErr: "mutually exclusive"},
		{name: "false picker still conflicts with explicit flag", args: []string{"--assignee-picker=false", "--assignee-id", explicitID}, wantErr: "mutually exclusive"},
		{name: "picker requires quick create context", pickerType: "agent", pickerID: agentID, args: []string{"--assignee-picker"}, wantErr: "quick-create"},
		{name: "missing type", taskID: taskID, pickerID: agentID, args: []string{"--assignee-picker"}, wantErr: "assignee type"},
		{name: "missing ID", taskID: taskID, pickerType: "agent", args: []string{"--assignee-picker"}, wantErr: "canonical UUID"},
		{name: "member cannot be a picker", taskID: taskID, pickerType: "member", pickerID: agentID, args: []string{"--assignee-picker"}, wantErr: "assignee type"},
		{name: "malformed picker ID", taskID: taskID, pickerType: "agent", pickerID: "282df4e-b502-4ca9-8fae-0c842f973e63", args: []string{"--assignee-picker"}, wantErr: "canonical UUID"},
		{name: "server permission rejection is preserved", taskID: taskID, pickerType: "agent", pickerID: agentID, args: []string{"--assignee-picker"}, wantType: "agent", wantID: agentID, serverStatus: http.StatusForbidden, wantErr: "permission denied"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var body map[string]any
			posts := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/issues":
					if r.Method != http.MethodPost {
						t.Errorf("issue request method = %s, want POST", r.Method)
					}
					posts++
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Errorf("decode create body: %v", err)
					}
					if tc.serverStatus != 0 {
						w.WriteHeader(tc.serverStatus)
						json.NewEncoder(w).Encode(map[string]any{"error": "permission denied"})
						return
					}
					w.WriteHeader(http.StatusCreated)
					json.NewEncoder(w).Encode(map[string]any{"id": "created-issue", "title": "Picker regression"})
				case "/api/agents":
					json.NewEncoder(w).Encode([]map[string]any{{"id": explicitID, "name": "explicit-agent"}})
				case "/api/workspaces/ws-1/members", "/api/squads":
					json.NewEncoder(w).Encode([]map[string]any{})
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
			t.Setenv("MULTICA_SERVER_URL", srv.URL)
			t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
			t.Setenv("MULTICA_TOKEN", "mat_test-token")
			t.Setenv("MULTICA_QUICK_CREATE_TASK_ID", tc.taskID)
			t.Setenv("MULTICA_QUICK_CREATE_DEFAULT_ASSIGNEE_TYPE", tc.pickerType)
			t.Setenv("MULTICA_QUICK_CREATE_DEFAULT_ASSIGNEE_ID", tc.pickerID)
			cmd := newIssueCreateTestCmd()
			if err := cmd.ParseFlags(append([]string{"--title", "Picker regression", "--description", "Unrecognized assignee: unknown"}, tc.args...)); err != nil {
				t.Fatal(err)
			}
			err := runIssueCreate(cmd, nil)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want %q", err, tc.wantErr)
				}
				if tc.serverStatus == 0 {
					if posts != 0 {
						t.Fatalf("invalid assignee caused %d create POST(s), want zero", posts)
					}
					return
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if posts != 1 {
				t.Fatalf("create POST count = %d, want one", posts)
			}
			if tc.wantType == "" {
				if _, ok := body["assignee_type"]; ok {
					t.Errorf("unexpected assignee_type: %v", body["assignee_type"])
				}
				if _, ok := body["assignee_id"]; ok {
					t.Errorf("unexpected assignee_id: %v", body["assignee_id"])
				}
			} else if body["assignee_type"] != tc.wantType || body["assignee_id"] != tc.wantID {
				t.Errorf("assignee = (%v, %v), want (%s, %s)", body["assignee_type"], body["assignee_id"], tc.wantType, tc.wantID)
			}
			if tc.taskID != "" && (body["origin_type"] != "quick_create" || body["origin_id"] != tc.taskID) {
				t.Errorf("quick-create origin was lost: %v", body)
			}
			if body["description"] != "Unrecognized assignee: unknown" {
				t.Errorf("description changed: %v", body["description"])
			}
		})
	}
}

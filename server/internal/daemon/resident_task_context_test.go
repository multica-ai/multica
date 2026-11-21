package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

func TestResidentSessionHomeReachable(t *testing.T) {
	for _, mode := range []string{"1", "", "true"} {
		t.Run("mode="+mode, func(t *testing.T) {
			t.Setenv("MULTICA_HERMES_RESIDENT_GATEWAY", mode)
			empty := &execenv.Environment{HermesSessionStore: t.TempDir()}
			if got := sessionHomeReachable("hermes", empty, false); got != (mode == "1") {
				t.Errorf("empty Hermes store reachable = %v", got)
			}
			if !sessionHomeReachable("codex", empty, false) {
				t.Error("other provider resume changed")
			}
		})
	}
}

func TestResidentTaskContext(t *testing.T) {
	for _, tc := range []struct {
		mode     string
		provider string
		chat     string
		want     bool
	}{
		{"1", "hermes", "chat-1", true},
		{"1", "hermes", "", true},
		{"", "hermes", "chat-1", false},
		{"1", "codex", "chat-1", false},
	} {
		t.Run(tc.mode+"/"+tc.provider+"/"+tc.chat, func(t *testing.T) {
			t.Setenv("MULTICA_HERMES_RESIDENT_GATEWAY", tc.mode)
			root := t.TempDir()
			task := Task{ID: "task-1", AgentID: "agent-1", WorkspaceID: "workspace-1", ChatSessionID: tc.chat}
			cleanup, err := writeResidentTaskContext(root, tc.provider, task, "mat_task")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(cleanup)
			path := filepath.Join(root, ".multica_resident_task.json")
			data, err := os.ReadFile(path)
			if !tc.want {
				if !os.IsNotExist(err) {
					t.Fatalf("unexpected context file: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != 0o600 {
				t.Fatalf("context mode is not 0600: %v", err)
			}
			var got map[string]string
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatal(err)
			}
			want := map[string]string{"task_id": "task-1", "agent_id": "agent-1", "workspace_id": "workspace-1", "token": "mat_task"}
			if tc.chat != "" {
				want["chat_session_id"] = tc.chat
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("context = %v, want %v", got, want)
			}
			cleanup()
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Errorf("cleanup left context: %v", err)
			}
			entries, err := os.ReadDir(root)
			if err != nil || len(entries) != 0 {
				t.Errorf("temporary files left: %v, %v", entries, err)
			}
		})
	}
}

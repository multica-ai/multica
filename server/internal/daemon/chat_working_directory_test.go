package daemon

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestChatWorkingDirectoryAssignment(t *testing.T) {
	selected := filepath.Join(t.TempDir(), "project with spaces 中文 ")
	project := t.TempDir()
	resource, err := json.Marshal(localDirectoryRef{DaemonID: "machine", LocalPath: project})
	if err != nil {
		t.Fatal(err)
	}
	task := Task{
		ChatSessionID:    "chat",
		WorkingDirectory: &protocol.ChatWorkingDirectory{DaemonID: "machine", Path: selected},
		ProjectResources: []ProjectResourceData{{ResourceType: localDirectoryResourceType, ResourceRef: resource}},
	}
	t.Run("explicit directory overrides project directory", func(t *testing.T) {
		assignment, err := localDirectoryAssignmentForTask(task, "machine")
		if err != nil || assignment == nil || assignment.AbsPath != selected {
			t.Fatalf("assignment = %+v, error = %v; want selected directory %q", assignment, err, selected)
		}
		if assignment.UsesWorktree() {
			t.Fatal("chat selection must execute in place")
		}
	})
	t.Run("machine mismatch cannot fall back to project", func(t *testing.T) {
		if assignment, err := localDirectoryAssignmentForTask(task, "other-machine"); err == nil || assignment != nil {
			t.Fatalf("assignment = %+v, error = %v; want a machine mismatch error", assignment, err)
		}
	})
	t.Run("reject missing machine and relative path", func(t *testing.T) {
		for _, selection := range []protocol.ChatWorkingDirectory{{Path: selected}, {DaemonID: "machine", Path: "relative"}} {
			invalid := task
			invalid.WorkingDirectory = &selection
			if _, err := localDirectoryAssignmentForTask(invalid, "machine"); err == nil {
				t.Fatalf("accepted invalid selection %+v", selection)
			}
		}
	})
	t.Run("selection is chat scoped", func(t *testing.T) {
		invalid := task
		invalid.ChatSessionID = ""
		if _, err := localDirectoryAssignmentForTask(invalid, "machine"); err == nil {
			t.Fatal("non-chat task accepted a chat working directory")
		}
	})
	t.Run("unselected chats retain project assignment", func(t *testing.T) {
		original := task
		original.WorkingDirectory = nil
		assignment, err := localDirectoryAssignmentForTask(original, "machine")
		if err != nil || assignment == nil || assignment.AbsPath != project {
			t.Fatalf("assignment = %+v, error = %v; want project directory %q", assignment, err, project)
		}
	})
}

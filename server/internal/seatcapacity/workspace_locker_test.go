package seatcapacity

import (
	"testing"

	"github.com/google/uuid"
)

func TestWorkspaceAdvisoryLockKeyIsWorkspaceScoped(t *testing.T) {
	workspaceA := uuid.MustParse("0193f876-4f3a-7d9c-8d8e-cb944847a001")
	workspaceB := uuid.MustParse("0193f876-4f3a-7d9c-8d8e-cb944847a002")
	if workspaceAdvisoryLockKey(workspaceA) == workspaceAdvisoryLockKey(workspaceB) {
		t.Fatal("different workspaces share an advisory lock key")
	}
}

package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
)

func TestDocWriteErrorExplainsRevisionConflict(t *testing.T) {
	err := docWriteError("update doc", 3, &cli.HTTPError{
		Method:     "PATCH",
		Path:       "/api/docs/x",
		StatusCode: 409,
		Body:       `{"code":"revision_conflict","actual_revision":5}`,
	})
	if !strings.Contains(err.Error(), "changed since revision 3") || !strings.Contains(err.Error(), "multica doc get") {
		t.Fatalf("conflict error should tell the agent how to recover, got: %v", err)
	}

	other := docWriteError("update doc", 3, errors.New("boom"))
	if strings.Contains(other.Error(), "changed since") {
		t.Fatalf("non-conflict errors must pass through, got: %v", other)
	}
}

func TestDocFolderPaths(t *testing.T) {
	paths := docFolderPaths([]map[string]any{
		{"id": "c", "title": "Runbooks", "parent_id": "b"},
		{"id": "a", "title": "Eng"},
		{"id": "b", "title": "Backend", "parent_id": "a"},
	})
	if paths["c"] != "Eng/Backend/Runbooks" || paths["a"] != "Eng" {
		t.Fatalf("unexpected paths: %v", paths)
	}
}

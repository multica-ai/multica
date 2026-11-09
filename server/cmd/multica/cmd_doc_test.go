package main

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
)

func TestDocCommandsExposeOnlyAgentReadWrite(t *testing.T) {
	expected := map[string]bool{"list": true, "get": true, "create": true, "update": true}
	for _, command := range docCmd.Commands() {
		if !expected[command.Name()] {
			t.Fatalf("unexpected doc command: %s", command.Name())
		}
		delete(expected, command.Name())
	}
	if len(expected) != 0 {
		t.Fatalf("missing commands: %v", expected)
	}
}

func TestDocConflictErrorExplainsRecovery(t *testing.T) {
	err := docWriteError("update doc", 3, &cli.HTTPError{StatusCode: http.StatusConflict, Body: `{"code":"revision_conflict","actual_revision":4}`})
	if !strings.Contains(err.Error(), "nothing was written") || !strings.Contains(err.Error(), "--base-revision") {
		t.Fatalf("missing recovery: %v", err)
	}
	cause := errors.New("connection closed")
	if !errors.Is(docWriteError("update doc", 3, cause), cause) {
		t.Fatal("lost underlying error")
	}
}

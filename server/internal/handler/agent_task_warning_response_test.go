package handler

import (
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestTaskToResponseSurfacesStoredWarnings(t *testing.T) {
	response := taskToResponse(db.AgentTaskQueue{
		Result: []byte(`{"output":"done","warnings":["cleanup pending","manual cleanup required"]}`),
	}, "")

	if len(response.Warnings) != 2 || response.Warnings[0] != "cleanup pending" || response.Warnings[1] != "manual cleanup required" {
		t.Fatalf("warnings = %#v, want stored task warnings", response.Warnings)
	}
}

func TestTaskToResponseDropsMalformedStoredWarnings(t *testing.T) {
	response := taskToResponse(db.AgentTaskQueue{
		Result: []byte(`{"output":"done","warnings":["cleanup pending",42]}`),
	}, "")

	if response.Warnings != nil {
		t.Fatalf("warnings = %#v, want malformed optional warnings omitted", response.Warnings)
	}
	if response.Result == nil {
		t.Fatal("malformed optional warnings should not drop the task result")
	}
}

package handler

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestShouldScheduleRuntimeUpdate(t *testing.T) {
	old := db.AgentRuntime{CliVersion: pgtype.Text{String: "1.0.0", Valid: true}, Metadata: []byte(`{}`)}
	if !shouldScheduleRuntimeUpdate(old, "v1.1.0") {
		t.Fatal("old standalone runtime should receive an update")
	}
	current := old
	current.CliVersion.String = "1.1.0"
	if shouldScheduleRuntimeUpdate(current, "v1.1.0") {
		t.Fatal("current runtime must not receive an update")
	}
	desktop := old
	desktop.Metadata = []byte(`{"launched_by":"desktop"}`)
	if shouldScheduleRuntimeUpdate(desktop, "v1.1.0") {
		t.Fatal("desktop-managed runtime must not self-update")
	}
}

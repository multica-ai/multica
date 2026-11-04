package service

import (
	"context"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestClosingAnInitiativeLeavesADurableRetroRequest checks spec 4.7: the close
// writes a retro request, and the retrospective run turns it into the trigger's
// issue for the owning squad's leader, once. The request is then processed, and a
// second run fires nothing more.
func TestClosingAnInitiativeLeavesADurableRetroRequest(t *testing.T) {
	f := newGovFixture(t)
	ctx := context.Background()
	pipeline := f.Insert(t, "pipeline", map[string]any{
		"workspace_id": f.wsText, "name": "Retro pipeline", "workflow_id": newUUIDText(),
		"project_id": newUUIDText(), "stages": string(pipelineStagesJSON(t)), "is_default": true,
	})
	iniID := f.Insert(t, "initiative", map[string]any{
		"workspace_id": f.wsText, "pipeline_id": pipeline, "seq": 7, "title": "Closed venture",
		"thesis": "fixture", "stage_key": "define", "owner_squad_id": f.squad, "status": "closed",
		"issue_id": newUUIDText(), "stage_entered_at": f.now.AddDate(0, 0, -3),
		"closed_at": f.now.Add(-time.Hour), "closed_reason": "The market moved.",
	})
	ini, err := f.q.GetInitiativeInWorkspace(ctx, db.GetInitiativeInWorkspaceParams{ID: util.MustParseUUID(iniID), WorkspaceID: f.ws})
	if err != nil {
		t.Fatalf("load initiative: %v", err)
	}

	// The close transaction's hook writes the request.
	if err := (RetroRequestHook{}).OnInitiativeClosed(ctx, f.q, ini, "The market moved."); err != nil {
		t.Fatalf("hook: %v", err)
	}
	if n := f.Count(t, `SELECT count(*) FROM retro_request WHERE workspace_id = $1 AND processed_at IS NULL`, f.wsText); n != 1 {
		t.Fatalf("pending requests after the close = %d, want 1", n)
	}

	tasks := NewTaskService(f.q, f.pool, nil, events.New())
	retro := &RetroService{Queries: f.q, Issues: NewIssueService(f.q, f.pool, nil, nil, tasks), Now: func() time.Time { return f.now }}
	fired, err := retro.EvaluateWorkspace(ctx, f.ws)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if fired < 1 {
		t.Fatalf("the run fired %d triggers, want the closed initiative's", fired)
	}
	if n := f.Count(t, `SELECT count(*) FROM retro_trigger WHERE workspace_id = $1 AND trigger_kind = 'initiative_closed' AND subject_id = $2`, f.wsText, iniID); n != 1 {
		t.Fatalf("retro triggers for the initiative = %d, want 1", n)
	}
	if n := f.Count(t, `SELECT count(*) FROM retro_request WHERE workspace_id = $1 AND processed_at IS NULL`, f.wsText); n != 0 {
		t.Fatalf("the request was not marked processed: %d pending", n)
	}

	if _, err := retro.EvaluateWorkspace(ctx, f.ws); err != nil {
		t.Fatalf("second evaluate: %v", err)
	}
	if n := f.Count(t, `SELECT count(*) FROM retro_trigger WHERE workspace_id = $1 AND trigger_kind = 'initiative_closed' AND subject_id = $2`, f.wsText, iniID); n != 1 {
		t.Fatalf("a second run fired the trigger again: %d rows", n)
	}
}

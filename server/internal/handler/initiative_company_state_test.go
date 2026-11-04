package handler

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/service"
)

// A paused company's auto stage does not advance on its own (spec 4.5). The
// artifacts still link; the stage moves when the company resumes and the sweep runs.
func TestInitiative_APausedCompanyHoldsAnAutoStageUntilResume(t *testing.T) {
	fx := newIniWorkspace(t)
	ctx := context.Background()
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM governance_event WHERE workspace_id = $1`, fx.ID)
	})
	created := fx.createInitiative(t, "Paused validate")
	fx.makeAutoExit(t, 0)

	css := &service.CompanyStateService{Queries: testHandler.Queries, TxStarter: testHandler.TxStarter, Tasks: testHandler.TaskService}
	admin := service.GovernanceActor{Type: "member", ID: parseUUID(fx.AdminID)}
	if _, err := css.Set(ctx, parseUUID(fx.ID), admin, service.CompanyStatePaused); err != nil {
		t.Fatalf("pause: %v", err)
	}
	link := func(kind string) {
		t.Helper()
		if _, err := testHandler.initiativeService().LinkArtifact(ctx, parseUUID(fx.ID), parseUUID(created.ID),
			parseUUID(uuid.NewString()), kind, service.InitiativeActor{Type: "member", ID: parseUUID(fx.AdminID)}); err != nil {
			t.Fatalf("link %s: %v", kind, err)
		}
	}
	link("validation_report")
	link("competitor_profile")
	if d := fx.detail(t, created.ID); d.Initiative.StageKey != "validate" {
		t.Fatalf("a paused company advanced the stage to %q", d.Initiative.StageKey)
	}

	if _, err := css.Set(ctx, parseUUID(fx.ID), admin, service.CompanyStateRunning); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if _, err := testHandler.initiativeService().SweepStages(ctx, 500); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if d := fx.detail(t, created.ID); d.Initiative.StageKey != "define" {
		t.Fatalf("after resume the sweep left the stage at %q, want define", d.Initiative.StageKey)
	}
}

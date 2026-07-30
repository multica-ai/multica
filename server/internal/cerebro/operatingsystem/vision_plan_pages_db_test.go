package operatingsystem

// Integration tests for the building-blocks model (FIR-3589): Vision and
// Traction are seeded pages rather than hard-coded layouts, every section is a
// block that sits in one column of one page, and a workspace can add pages of
// its own and move blocks between them.
//
// Shares the fixture and TestMain in vision_plan_links_db_test.go, so they skip
// cleanly when no test DB is reachable. Run against a migrated DB, e.g.:
//
//	DATABASE_URL=postgres://multica:multica@127.0.0.1:5432/multica?sslmode=disable \
//	  go test ./internal/cerebro/operatingsystem/ -run TestVisionPlanPages -v

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
)

func vplPage(plan VisionPlanResponse, key string) *VisionPlanPageResponse {
	for i, page := range plan.Pages {
		if page.Key == key {
			return &plan.Pages[i]
		}
	}
	return nil
}

func vplSection(plan VisionPlanResponse, key string) *VisionPlanSectionResponse {
	for i, section := range plan.Sections {
		if section.Key == key {
			return &plan.Sections[i]
		}
	}
	return nil
}

func TestVisionPlanPagesSeedVisionAndTraction(t *testing.T) {
	vplSkip(t)
	ctx := context.Background()
	plan, err := vplService.ListVisionPlan(ctx, vplWsID)
	if err != nil {
		t.Fatalf("list vision plan: %v", err)
	}

	vision := vplPage(plan, "vision")
	traction := vplPage(plan, "traction")
	if vision == nil || traction == nil {
		t.Fatalf("pages = %+v, want a vision and a traction page", plan.Pages)
	}
	if len(vision.RowColumnCounts) != 1 || vision.RowColumnCounts[0] != 2 {
		t.Errorf("vision rows = %v, want one row of 2 columns", vision.RowColumnCounts)
	}
	if len(traction.RowColumnCounts) != 1 || traction.RowColumnCounts[0] != 3 {
		t.Errorf("traction rows = %v, want one row of 3 columns", traction.RowColumnCounts)
	}

	// Every block lands on a page, so nothing can become invisible.
	for _, section := range plan.Sections {
		if section.PageID == "" {
			t.Errorf("section %q has no page", section.Key)
		}
	}

	coreValues := vplSection(plan, "core-values")
	if coreValues == nil || coreValues.PageID != vision.ID || coreValues.ColumnIndex != 0 {
		t.Errorf("core-values = %+v, want column 0 of the vision page", coreValues)
	}
	picture := vplSection(plan, "three-year-picture")
	if picture == nil || picture.PageID != vision.ID || picture.ColumnIndex != 1 {
		t.Errorf("three-year-picture = %+v, want column 1 of the vision page", picture)
	}
	goals := vplSection(plan, "goals-board")
	if goals == nil || goals.SectionType != "goals" || goals.PageID != traction.ID || goals.ColumnIndex != 1 {
		t.Errorf("goals-board = %+v, want a goals block in column 1 of the traction page", goals)
	}
	issues := vplSection(plan, "issues-list")
	if issues == nil || issues.PageID != traction.ID || issues.ColumnIndex != 2 {
		t.Errorf("issues-list = %+v, want column 2 of the traction page", issues)
	}
}

func TestVisionPlanPageCreateAndMoveBlock(t *testing.T) {
	vplSkip(t)
	ctx := context.Background()
	plan, err := vplService.ListVisionPlan(ctx, vplWsID)
	if err != nil {
		t.Fatalf("list vision plan: %v", err)
	}

	page, err := vplService.CreateVisionPlanPage(ctx, vplWsID, VisionPlanPageInput{Name: "Accountability", RowColumnCounts: []int32{3, 1}, Position: 2})
	if err != nil {
		t.Fatalf("create page: %v", err)
	}
	defer func() {
		if _, err := vplService.DeleteVisionPlanPage(context.Background(), vplWsID, mustParseUUIDForTest(t, page.ID)); err != nil {
			t.Errorf("delete page: %v", err)
		}
	}()
	if page.Key == "" || page.Name != "Accountability" || len(page.RowColumnCounts) != 2 || page.RowColumnCounts[0] != 3 || page.RowColumnCounts[1] != 1 {
		t.Fatalf("page = %+v, want a keyed Accountability page with a 3-column row over a 1-column row", page)
	}

	// A block created on the new page comes back in that page's row and column.
	block, err := vplService.CreateVisionPlanSection(ctx, vplWsID, VisionPlanSectionInput{
		Name: "Seats", SectionType: "list", Position: 0, PageID: page.ID, RowIndex: 1, ColumnIndex: 0,
	})
	if err != nil {
		t.Fatalf("create block: %v", err)
	}
	if block.PageID != page.ID || block.RowIndex != 1 || block.ColumnIndex != 0 {
		t.Fatalf("block = %+v, want page %s row 1 column 0", block, page.ID)
	}

	// Moving an existing seeded block to the new page sticks, and the seeding
	// pass must not drag it back.
	coreValues := vplSection(plan, "core-values")
	if coreValues == nil {
		t.Fatal("core-values section missing")
	}
	moved, err := vplService.UpdateVisionPlanSection(ctx, vplWsID, mustParseUUIDForTest(t, coreValues.ID), VisionPlanSectionInput{
		Name: coreValues.Name, SectionType: coreValues.SectionType, Position: 0, PageID: page.ID, ColumnIndex: 0,
	})
	if err != nil {
		t.Fatalf("move block: %v", err)
	}
	if moved.PageID != page.ID {
		t.Fatalf("moved = %+v, want page %s", moved, page.ID)
	}

	reread, err := vplService.ListVisionPlan(ctx, vplWsID)
	if err != nil {
		t.Fatalf("re-list vision plan: %v", err)
	}
	if got := vplSection(reread, "core-values"); got == nil || got.PageID != page.ID {
		t.Fatalf("core-values after re-read = %+v, want page %s", got, page.ID)
	}

	// Put it back so later runs in this fixture start from the seeded layout.
	vision := vplPage(reread, "vision")
	if vision == nil {
		t.Fatal("vision page missing")
	}
	if _, err := vplService.UpdateVisionPlanSection(ctx, vplWsID, mustParseUUIDForTest(t, coreValues.ID), VisionPlanSectionInput{
		Name: coreValues.Name, SectionType: coreValues.SectionType, Position: 0, PageID: vision.ID, ColumnIndex: 0,
	}); err != nil {
		t.Fatalf("restore block: %v", err)
	}
}

// Deleting a page takes its blocks with it, so this runs in a throwaway
// workspace rather than tearing down the shared fixture.
func TestVisionPlanLastPageCannotBeDeleted(t *testing.T) {
	vplSkip(t)
	ctx := context.Background()
	wsID := vplTempWorkspace(t, ctx, "vision-plan-page-delete-tests", "VPD")

	plan, err := vplService.ListVisionPlan(ctx, wsID)
	if err != nil {
		t.Fatalf("list vision plan: %v", err)
	}
	if len(plan.Pages) < 2 {
		t.Fatalf("pages = %d, want at least 2 for this test", len(plan.Pages))
	}

	deleted, err := vplService.DeleteVisionPlanPage(ctx, wsID, mustParseUUIDForTest(t, plan.Pages[0].ID))
	if err != nil || !deleted {
		t.Fatalf("delete first page: deleted = %v err = %v", deleted, err)
	}
	remaining, err := vplService.ListVisionPlan(ctx, wsID)
	if err != nil {
		t.Fatalf("re-list vision plan: %v", err)
	}
	if len(remaining.Pages) != 1 {
		t.Fatalf("pages = %d, want 1 before the last-page check", len(remaining.Pages))
	}

	lastDeleted, err := vplService.DeleteVisionPlanPage(ctx, wsID, mustParseUUIDForTest(t, remaining.Pages[0].ID))
	if err != nil {
		t.Fatalf("delete last page: %v", err)
	}
	if lastDeleted {
		t.Fatal("the last page was deleted; a workspace must keep somewhere to put a block")
	}
}

func vplTempWorkspace(t *testing.T, ctx context.Context, slug, prefix string) pgtype.UUID {
	t.Helper()
	var wsID pgtype.UUID
	if err := vplPool.QueryRow(ctx, `INSERT INTO workspace (name, slug, description, issue_prefix) VALUES ($1, $2, $3, $4) RETURNING id`,
		slug, slug, "Temporary", prefix).Scan(&wsID); err != nil {
		t.Fatalf("create workspace %s: %v", slug, err)
	}
	t.Cleanup(func() {
		background := context.Background()
		for _, stmt := range []string{
			`DELETE FROM cerebro_strategy_item WHERE workspace_id = $1`,
			`DELETE FROM cerebro_vision_plan_section WHERE workspace_id = $1`,
			`DELETE FROM cerebro_vision_plan_page WHERE workspace_id = $1`,
			`DELETE FROM workspace WHERE id = $1`,
		} {
			if _, err := vplPool.Exec(background, stmt, wsID); err != nil {
				t.Errorf("cleanup %q: %v", stmt, err)
			}
		}
	})
	return wsID
}

func mustParseUUIDForTest(t *testing.T, value string) pgtype.UUID {
	t.Helper()
	parsed, err := util.ParseUUID(value)
	if err != nil {
		t.Fatalf("parse uuid %q: %v", value, err)
	}
	return parsed
}

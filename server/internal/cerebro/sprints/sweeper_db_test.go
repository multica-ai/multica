package sprints

// Integration tests for the sweeper covering the two regressions QA hit on
// FIR-2666 PR #825 (FIR-2699):
//
//   1. Sweeper ran even when the workspace-level `cerebro_sprints` flag was
//      OFF — auto_create_enabled on a sprint_settings row should not be
//      enough on its own.
//   2. Cloning a recurring task into the next sprint inserted an issue with
//      creator_id = '00000000-…' which hit the issue.creator_id NOT NULL
//      constraint and rolled back the whole transaction.
//
// Tests skip cleanly when no test DB is reachable, same pattern as
// feature_flags/store_test.go.

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	sweeperTestEmail         = "sprint-sweeper-test@multica.ai"
	sweeperTestName          = "Sprint Sweeper Test"
	sweeperTestWorkspaceSlug = "sprint-sweeper-tests"
)

var (
	sweeperTestPool        *pgxpool.Pool
	sweeperTestWorkspaceID pgtype.UUID
	sweeperTestUserID      pgtype.UUID
	sweeperTestProjectID   pgtype.UUID
)

func TestMain(m *testing.M) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Printf("Skipping sprint sweeper integration tests: %v\n", err)
		os.Exit(m.Run())
	}
	if err := pool.Ping(ctx); err != nil {
		fmt.Printf("Skipping sprint sweeper integration tests: db not reachable: %v\n", err)
		pool.Close()
		os.Exit(m.Run())
	}
	if err := cleanupSweeperFixture(ctx, pool); err != nil {
		fmt.Printf("Failed to clean sweeper fixture: %v\n", err)
		pool.Close()
		os.Exit(1)
	}
	if err := setupSweeperFixture(ctx, pool); err != nil {
		fmt.Printf("Failed to set up sweeper fixture: %v\n", err)
		_ = cleanupSweeperFixture(ctx, pool)
		pool.Close()
		os.Exit(1)
	}
	sweeperTestPool = pool
	code := m.Run()
	if err := cleanupSweeperFixture(context.Background(), pool); err != nil {
		fmt.Printf("Failed to clean sweeper fixture: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	pool.Close()
	os.Exit(code)
}

func setupSweeperFixture(ctx context.Context, pool *pgxpool.Pool) error {
	if err := pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, sweeperTestName, sweeperTestEmail).Scan(&sweeperTestUserID); err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, "Sprint Sweeper Tests", sweeperTestWorkspaceSlug, "Temporary workspace", "SPT").Scan(&sweeperTestWorkspaceID); err != nil {
		return fmt.Errorf("create workspace: %w", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, sweeperTestWorkspaceID, sweeperTestUserID); err != nil {
		return fmt.Errorf("create member: %w", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title)
		VALUES ($1, $2)
		RETURNING id
	`, sweeperTestWorkspaceID, "Sweeper Project").Scan(&sweeperTestProjectID); err != nil {
		return fmt.Errorf("create project: %w", err)
	}
	return nil
}

func cleanupSweeperFixture(ctx context.Context, pool *pgxpool.Pool) error {
	// cerebro_feature_flags + cerebro_sprint_* have no workspace FK cascade.
	bySlug := []string{
		`DELETE FROM cerebro_feature_flags WHERE workspace_id IN (SELECT id FROM workspace WHERE slug = $1)`,
		`DELETE FROM cerebro_sprint_issue WHERE sprint_id IN (SELECT s.id FROM cerebro_sprint s JOIN workspace w ON w.id = s.workspace_id WHERE w.slug = $1)`,
		`DELETE FROM cerebro_sprint_recurring_task WHERE workspace_id IN (SELECT id FROM workspace WHERE slug = $1)`,
		`DELETE FROM cerebro_sprint WHERE workspace_id IN (SELECT id FROM workspace WHERE slug = $1)`,
		`DELETE FROM cerebro_sprint_settings WHERE workspace_id IN (SELECT id FROM workspace WHERE slug = $1)`,
		`DELETE FROM issue WHERE workspace_id IN (SELECT id FROM workspace WHERE slug = $1)`,
		`DELETE FROM project WHERE workspace_id IN (SELECT id FROM workspace WHERE slug = $1)`,
		`DELETE FROM member WHERE workspace_id IN (SELECT id FROM workspace WHERE slug = $1)`,
		`DELETE FROM workspace WHERE slug = $1`,
	}
	for _, stmt := range bySlug {
		if _, err := pool.Exec(ctx, stmt, sweeperTestWorkspaceSlug); err != nil {
			return fmt.Errorf("cleanup %q: %w", stmt, err)
		}
	}
	if _, err := pool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, sweeperTestEmail); err != nil {
		return fmt.Errorf("cleanup user: %w", err)
	}
	return nil
}

// resetSweeperState wipes everything the sweeper writes between sub-tests so
// each scenario starts with a known sprint_settings + sprints state.
func resetSweeperState(t *testing.T, ctx context.Context) {
	t.Helper()
	for _, stmt := range []string{
		`DELETE FROM cerebro_feature_flags WHERE workspace_id = $1`,
		`DELETE FROM cerebro_sprint_issue WHERE sprint_id IN (SELECT id FROM cerebro_sprint WHERE workspace_id = $1)`,
		`DELETE FROM cerebro_sprint_recurring_task WHERE workspace_id = $1`,
		`DELETE FROM cerebro_sprint WHERE workspace_id = $1`,
		`DELETE FROM cerebro_sprint_settings WHERE workspace_id = $1`,
		`DELETE FROM issue WHERE workspace_id = $1`,
	} {
		if _, err := sweeperTestPool.Exec(ctx, stmt, sweeperTestWorkspaceID); err != nil {
			t.Fatalf("reset %q: %v", stmt, err)
		}
	}
}

func newSweeperForTest(t *testing.T, now time.Time) *Sweeper {
	t.Helper()
	sw := NewSweeper(sweeperTestPool, cerebrodb.New(sweeperTestPool), db.New(sweeperTestPool))
	sw.nowFunc = func() time.Time { return now }
	return sw
}

func seedSettingsWithLeadDays(t *testing.T, ctx context.Context, leadDays int32) {
	t.Helper()
	q := cerebrodb.New(sweeperTestPool)
	if _, err := q.UpsertCerebroSprintSettings(ctx, cerebrodb.UpsertCerebroSprintSettingsParams{
		ProjectID:             sweeperTestProjectID,
		WorkspaceID:           sweeperTestWorkspaceID,
		Enabled:               true,
		DurationUnit:          UnitWeek,
		DurationCount:         2,
		StartWeekday:          1,
		NameTemplate:          "Sprint {n}",
		AutoCreateEnabled:     true,
		AutoCreateLeadDays:    leadDays,
		MoveIncompleteEnabled: true,
		Timezone:              "UTC",
	}); err != nil {
		t.Fatalf("upsert settings: %v", err)
	}
}

func seedActiveSprintEndingOn(t *testing.T, ctx context.Context, end time.Time) cerebrodb.CerebroSprint {
	t.Helper()
	q := cerebrodb.New(sweeperTestPool)
	sprint, err := q.CreateCerebroSprint(ctx, cerebrodb.CreateCerebroSprintParams{
		WorkspaceID: sweeperTestWorkspaceID,
		ProjectID:   sweeperTestProjectID,
		Name:        "Sprint 1",
		SequenceNo:  1,
		Status:      StatusActive,
		StartDate:   pgtype.Date{Time: end.AddDate(0, 0, -13), Valid: true},
		EndDate:     pgtype.Date{Time: end, Valid: true},
	})
	if err != nil {
		t.Fatalf("create active sprint: %v", err)
	}
	return sprint
}

// TestSweeperTick_SkipsWhenFlagOff is the FIR-2699 regression: even with
// auto_create_enabled on the settings row, the sweeper must NOT touch a
// workspace whose cerebro_sprints feature flag is OFF (the default).
func TestSweeperTick_SkipsWhenFlagOff(t *testing.T) {
	if sweeperTestPool == nil {
		t.Skip("no test database")
	}
	ctx := context.Background()
	resetSweeperState(t, ctx)

	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	seedSettingsWithLeadDays(t, ctx, 2)
	// Active sprint ends today; without the flag check the sweeper would
	// create the next sprint.
	seedActiveSprintEndingOn(t, ctx, now)

	sw := newSweeperForTest(t, now)
	if err := sw.Tick(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}

	q := cerebrodb.New(sweeperTestPool)
	sprints, err := q.ListCerebroSprintsByProject(ctx, sweeperTestProjectID)
	if err != nil {
		t.Fatalf("list sprints: %v", err)
	}
	if len(sprints) != 1 {
		t.Fatalf("flag OFF must keep sprint count at 1, got %d", len(sprints))
	}
	if sprints[0].Status != StatusActive {
		t.Fatalf("flag OFF must leave status untouched, got %q", sprints[0].Status)
	}
}

// TestSweeperTick_RunsWhenFlagOn proves the sweeper acts when the workspace
// has opted into cerebro_sprints (workspace-level override row enabled=TRUE).
func TestSweeperTick_RunsWhenFlagOn(t *testing.T) {
	if sweeperTestPool == nil {
		t.Skip("no test database")
	}
	ctx := context.Background()
	resetSweeperState(t, ctx)

	q := cerebrodb.New(sweeperTestPool)
	if err := q.UpsertCerebroWorkspaceFeatureFlag(ctx, cerebrodb.UpsertCerebroWorkspaceFeatureFlagParams{
		WorkspaceID: sweeperTestWorkspaceID,
		FlagKey:     "cerebro_sprints",
		Enabled:     true,
		Locked:      false,
	}); err != nil {
		t.Fatalf("enable flag: %v", err)
	}

	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	seedSettingsWithLeadDays(t, ctx, 2)
	seedActiveSprintEndingOn(t, ctx, now)

	sw := newSweeperForTest(t, now)
	if err := sw.Tick(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}

	sprints, err := q.ListCerebroSprintsByProject(ctx, sweeperTestProjectID)
	if err != nil {
		t.Fatalf("list sprints: %v", err)
	}
	if len(sprints) != 2 {
		t.Fatalf("flag ON should create next sprint (count=2), got %d", len(sprints))
	}
}

// TestSweeperSweepProject_ReportsCountersAndHonorsFlag covers the manual
// sweep trigger added for FIR-2699 QA. It proves:
//   1. The endpoint reports flag_enabled=false and does nothing when the
//      workspace-level cerebro_sprints flag is OFF.
//   2. With the flag ON + an active sprint within the lead-days window,
//      SweepProject creates the next sprint, clones a recurring task, and
//      reports both counts in the returned ProjectSweepResult.
func TestSweeperSweepProject_ReportsCountersAndHonorsFlag(t *testing.T) {
	if sweeperTestPool == nil {
		t.Skip("no test database")
	}
	ctx := context.Background()
	resetSweeperState(t, ctx)

	q := cerebrodb.New(sweeperTestPool)
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	seedSettingsWithLeadDays(t, ctx, 2)
	seedActiveSprintEndingOn(t, ctx, now)
	if _, err := q.CreateCerebroSprintRecurringTask(ctx, cerebrodb.CreateCerebroSprintRecurringTaskParams{
		WorkspaceID:  sweeperTestWorkspaceID,
		ProjectID:    sweeperTestProjectID,
		CadenceUnit:  UnitWeek,
		CadenceCount: 2,
		Title:        "Sweep-endpoint recurring",
		Enabled:      true,
	}); err != nil {
		t.Fatalf("create recurring task: %v", err)
	}

	sw := newSweeperForTest(t, now)

	// Flag OFF: SweepProject returns a result with FlagEnabled=false and
	// touches nothing.
	off, err := sw.SweepProject(ctx, sweeperTestProjectID)
	if err != nil {
		t.Fatalf("sweep with flag off: %v", err)
	}
	if off.FlagEnabled {
		t.Fatalf("flag_enabled = true with no override row, want false")
	}
	if off.NextSprintCreated.Valid || off.RecurringTasksCloned != 0 {
		t.Fatalf("flag off must produce no changes, got %+v", off)
	}

	// Flag ON: SweepProject runs the full pipeline and reports counts.
	if err := q.UpsertCerebroWorkspaceFeatureFlag(ctx, cerebrodb.UpsertCerebroWorkspaceFeatureFlagParams{
		WorkspaceID: sweeperTestWorkspaceID,
		FlagKey:     "cerebro_sprints",
		Enabled:     true,
	}); err != nil {
		t.Fatalf("enable flag: %v", err)
	}
	on, err := sw.SweepProject(ctx, sweeperTestProjectID)
	if err != nil {
		t.Fatalf("sweep with flag on: %v", err)
	}
	if !on.FlagEnabled {
		t.Fatalf("flag_enabled = false after workspace override, want true")
	}
	if !on.NextSprintCreated.Valid {
		t.Fatalf("next_sprint_created must be set when an active sprint is in the lead-days window, got %+v", on)
	}
	if on.RecurringTasksCloned != 1 {
		t.Fatalf("recurring_tasks_cloned = %d, want 1", on.RecurringTasksCloned)
	}
}

// TestSweeperTick_ClonesRecurringTaskWithWorkspaceOwnerCreator is the second
// FIR-2699 regression. Before the fix, cloning a recurring task created an
// issue with creator_id = '00000000-…' which violates issue.creator_id NOT
// NULL. After the fix, the creator is the workspace owner (member).
func TestSweeperTick_ClonesRecurringTaskWithWorkspaceOwnerCreator(t *testing.T) {
	if sweeperTestPool == nil {
		t.Skip("no test database")
	}
	ctx := context.Background()
	resetSweeperState(t, ctx)

	q := cerebrodb.New(sweeperTestPool)
	if err := q.UpsertCerebroWorkspaceFeatureFlag(ctx, cerebrodb.UpsertCerebroWorkspaceFeatureFlagParams{
		WorkspaceID: sweeperTestWorkspaceID,
		FlagKey:     "cerebro_sprints",
		Enabled:     true,
	}); err != nil {
		t.Fatalf("enable flag: %v", err)
	}
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	seedSettingsWithLeadDays(t, ctx, 2)
	seedActiveSprintEndingOn(t, ctx, now)

	if _, err := q.CreateCerebroSprintRecurringTask(ctx, cerebrodb.CreateCerebroSprintRecurringTaskParams{
		WorkspaceID:  sweeperTestWorkspaceID,
		ProjectID:    sweeperTestProjectID,
		CadenceUnit:  UnitWeek,
		CadenceCount: 2,
		Title:        "Weekly stand-up notes",
		Description:  pgtype.Text{String: "auto-cloned", Valid: true},
		Enabled:      true,
	}); err != nil {
		t.Fatalf("create recurring task: %v", err)
	}

	sw := newSweeperForTest(t, now)
	if err := sw.Tick(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}

	// Exactly one issue should now exist in the workspace, owned by the
	// fixture user via creator_id (creator_type=member).
	var (
		gotCount     int
		gotCreatorID pgtype.UUID
		gotType      string
	)
	if err := sweeperTestPool.QueryRow(ctx, `
		SELECT count(*), MIN(creator_id::text)::uuid, MIN(creator_type)
		FROM issue WHERE workspace_id = $1
	`, sweeperTestWorkspaceID).Scan(&gotCount, &gotCreatorID, &gotType); err != nil {
		t.Fatalf("query issue: %v", err)
	}
	if gotCount != 1 {
		t.Fatalf("want exactly 1 cloned issue, got %d", gotCount)
	}
	if gotType != "member" {
		t.Fatalf("creator_type = %q, want %q", gotType, "member")
	}
	if util.UUIDToString(gotCreatorID) != util.UUIDToString(sweeperTestUserID) {
		t.Fatalf("creator_id = %s, want workspace owner %s",
			util.UUIDToString(gotCreatorID), util.UUIDToString(sweeperTestUserID))
	}
}

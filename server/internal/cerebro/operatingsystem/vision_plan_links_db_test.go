package operatingsystem

// Integration tests for coupling a Vision/Traction item to a Project or an
// Issue (FIR-3589): the connection is validated against the workspace on write
// and comes back on the Vision Plan with the label the board renders.
//
// They skip cleanly when no test DB is reachable, same pattern as the note and
// comments packages' *_db_test.go. Run against a migrated DB, e.g.:
//
//	DATABASE_URL=postgres://multica:multica@127.0.0.1:5432/multica?sslmode=disable \
//	  go test ./internal/cerebro/operatingsystem/ -run TestVisionPlanLinks -v

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	vplEmail = "vision-plan-links-test@multica.ai"
	vplSlug  = "vision-plan-links-tests"
)

var (
	vplPool    *pgxpool.Pool
	vplService *Service
	vplWsID    pgtype.UUID
	vplUser    pgtype.UUID
	vplMember  pgtype.UUID
)

func TestMain(m *testing.M) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		fmt.Println("Skipping vision-plan link integration tests: DATABASE_URL unset")
		os.Exit(m.Run())
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Printf("Skipping vision-plan link integration tests: %v\n", err)
		os.Exit(m.Run())
	}
	if err := pool.Ping(ctx); err != nil {
		fmt.Printf("Skipping vision-plan link integration tests: db not reachable: %v\n", err)
		pool.Close()
		os.Exit(m.Run())
	}
	_ = cleanupVPL(ctx, pool)
	if err := setupVPL(ctx, pool); err != nil {
		fmt.Printf("Failed to set up vision-plan link fixture: %v\n", err)
		_ = cleanupVPL(ctx, pool)
		pool.Close()
		os.Exit(1)
	}
	vplPool = pool
	vplService = NewService(cerebrodb.New(pool), db.New(pool))
	code := m.Run()
	if err := cleanupVPL(context.Background(), pool); err != nil {
		fmt.Printf("Failed to clean vision-plan link fixture: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	pool.Close()
	os.Exit(code)
}

func setupVPL(ctx context.Context, pool *pgxpool.Pool) error {
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		"Vision Plan Links Test", vplEmail).Scan(&vplUser); err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO workspace (name, slug, description, issue_prefix) VALUES ($1, $2, $3, $4) RETURNING id`,
		"Vision Plan Links Tests", vplSlug, "Temporary", "VPL").Scan(&vplWsID); err != nil {
		return fmt.Errorf("create workspace: %w", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner') RETURNING id`,
		vplWsID, vplUser).Scan(&vplMember); err != nil {
		return fmt.Errorf("create member: %w", err)
	}
	return nil
}

func cleanupVPL(ctx context.Context, pool *pgxpool.Pool) error {
	stmts := []string{
		`DELETE FROM cerebro_object_connection WHERE workspace_id IN (SELECT id FROM workspace WHERE slug = $1)`,
		`DELETE FROM cerebro_strategy_item WHERE workspace_id IN (SELECT id FROM workspace WHERE slug = $1)`,
		`DELETE FROM cerebro_vision_plan_section WHERE workspace_id IN (SELECT id FROM workspace WHERE slug = $1)`,
		`DELETE FROM cerebro_vision_plan_page WHERE workspace_id IN (SELECT id FROM workspace WHERE slug = $1)`,
		`DELETE FROM issue WHERE workspace_id IN (SELECT id FROM workspace WHERE slug = $1)`,
		`DELETE FROM project WHERE workspace_id IN (SELECT id FROM workspace WHERE slug = $1)`,
		`DELETE FROM member WHERE workspace_id IN (SELECT id FROM workspace WHERE slug = $1)`,
		`DELETE FROM workspace WHERE slug = $1`,
	}
	for _, stmt := range stmts {
		if _, err := pool.Exec(ctx, stmt, vplSlug); err != nil {
			return fmt.Errorf("cleanup %q: %w", stmt, err)
		}
	}
	_, _ = pool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, vplEmail)
	return nil
}

func vplSkip(t *testing.T) {
	t.Helper()
	if vplService == nil {
		t.Skip("no test database available")
	}
}

// The first Vision Plan read seeds the default sections, so the item can be
// created against a real section id.
func vplFirstSection(t *testing.T, ctx context.Context) string {
	t.Helper()
	plan, err := vplService.ListVisionPlan(ctx, vplWsID)
	if err != nil {
		t.Fatalf("list vision plan: %v", err)
	}
	if len(plan.Sections) == 0 {
		t.Fatalf("vision plan has no sections")
	}
	return plan.Sections[0].ID
}

func vplItem(t *testing.T, ctx context.Context, title string) VisionPlanItemResponse {
	t.Helper()
	item, err := vplService.CreateVisionPlanItem(ctx, vplWsID, VisionPlanItemInput{
		SectionID: vplFirstSection(t, ctx), Title: title, Position: 0, State: "active",
	})
	if err != nil {
		t.Fatalf("create vision plan item: %v", err)
	}
	return item
}

func vplProject(t *testing.T, ctx context.Context, title string) pgtype.UUID {
	t.Helper()
	var id pgtype.UUID
	if err := vplPool.QueryRow(ctx, `INSERT INTO project (workspace_id, title) VALUES ($1, $2) RETURNING id`,
		vplWsID, title).Scan(&id); err != nil {
		t.Fatalf("create project: %v", err)
	}
	return id
}

func vplIssue(t *testing.T, ctx context.Context, title string) pgtype.UUID {
	t.Helper()
	var id pgtype.UUID
	if err := vplPool.QueryRow(ctx,
		`INSERT INTO issue (workspace_id, title, creator_type, creator_id, number)
		 VALUES ($1, $2, 'member', $3, (SELECT COALESCE(MAX(number),0)+1 FROM issue WHERE workspace_id = $1))
		 RETURNING id`, vplWsID, title, vplMember).Scan(&id); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	return id
}

func TestVisionPlanLinksReturnProjectAndIssue(t *testing.T) {
	vplSkip(t)
	ctx := context.Background()
	item := vplItem(t, ctx, "Own the outcome")
	projectID := vplProject(t, ctx, "Nordic launch")
	issueID := vplIssue(t, ctx, "Ship pricing page")

	for _, target := range []struct{ kind, id string }{
		{"project", util.UUIDToString(projectID)},
		{"issue", util.UUIDToString(issueID)},
	} {
		if _, err := vplService.CreateConnection(ctx, vplWsID, "member", vplMember, ObjectConnectionInput{
			SourceType: "strategy_item", SourceID: item.ID, TargetType: target.kind, TargetID: target.id,
			RelationshipType: "relates_to", Provenance: "manual",
		}); err != nil {
			t.Fatalf("connect %s: %v", target.kind, err)
		}
	}

	plan, err := vplService.ListVisionPlan(ctx, vplWsID)
	if err != nil {
		t.Fatalf("list vision plan: %v", err)
	}
	links := vplLinksFor(plan, item.ID)
	if len(links) != 2 {
		t.Fatalf("links = %+v, want 2", links)
	}
	byType := map[string]VisionPlanObjectLink{}
	for _, link := range links {
		byType[link.TargetType] = link
	}
	if byType["project"].Title != "Nordic launch" {
		t.Errorf("project link = %+v, want title Nordic launch", byType["project"])
	}
	if byType["issue"].Title != "Ship pricing page" || byType["issue"].Identifier == "" {
		t.Errorf("issue link = %+v, want title and identifier", byType["issue"])
	}
}

func TestVisionPlanLinkRejectsProjectOutsideWorkspace(t *testing.T) {
	vplSkip(t)
	ctx := context.Background()
	item := vplItem(t, ctx, "Care deeply")
	var otherWs pgtype.UUID
	if err := vplPool.QueryRow(ctx, `INSERT INTO workspace (name, slug, description, issue_prefix) VALUES ($1, $2, $3, $4) RETURNING id`,
		"Vision Plan Links Other", vplSlug+"-other", "Temporary", "VPO").Scan(&otherWs); err != nil {
		t.Fatalf("create other workspace: %v", err)
	}
	defer func() {
		_, _ = vplPool.Exec(context.Background(), `DELETE FROM project WHERE workspace_id = $1`, otherWs)
		_, _ = vplPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, otherWs)
	}()
	var foreignProject pgtype.UUID
	if err := vplPool.QueryRow(ctx, `INSERT INTO project (workspace_id, title) VALUES ($1, $2) RETURNING id`,
		otherWs, "Someone else's project").Scan(&foreignProject); err != nil {
		t.Fatalf("create foreign project: %v", err)
	}

	_, err := vplService.CreateConnection(ctx, vplWsID, "member", vplMember, ObjectConnectionInput{
		SourceType: "strategy_item", SourceID: item.ID, TargetType: "project", TargetID: util.UUIDToString(foreignProject),
		RelationshipType: "relates_to", Provenance: "manual",
	})
	if err == nil {
		t.Fatal("connecting a project from another workspace was accepted")
	}
}

func vplLinksFor(plan VisionPlanResponse, itemID string) []VisionPlanObjectLink {
	for _, section := range plan.Sections {
		for _, item := range section.Items {
			if item.ID == itemID {
				return item.Links
			}
		}
	}
	return nil
}

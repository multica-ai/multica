package service

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// openTestPool returns a pgxpool connected to the test database, or skips the
// test if no DATABASE_URL is configured or the database is unreachable.
func openTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Skipf("skipping: could not connect to database: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Skipf("skipping: database not reachable: %v", err)
	}
	return pool
}

// setupTemplateFixtures creates a workspace, member, and a template workflow
// with nodes and edges. Returns the workspace ID and template workflow ID.
// Caller is responsible for cleanup via cleanupTemplateFixtures.
func setupTemplateFixtures(t *testing.T, pool *pgxpool.Pool) (pgtype.UUID, pgtype.UUID) {
	t.Helper()
	ctx := context.Background()
	suffix := fmt.Sprintf("tpl-%d-%d", os.Getpid(), time.Now().UnixNano())

	var workspaceID string
	err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, 'template test workspace', 'TPL')
		RETURNING id
	`, "Template Test Workspace "+suffix, "template-test-"+suffix).Scan(&workspaceID)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	var userID string
	err = pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, "Template Test User "+suffix, "template-test-"+suffix+"@multica.ai").Scan(&userID)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, workspaceID, userID)
	if err != nil {
		t.Fatalf("create member: %v", err)
	}

	// Create a template workflow.
	var tmplID string
	err = pool.QueryRow(ctx, `
		INSERT INTO workflow (workspace_id, title, description, status, max_retries, created_by_type, created_by_id, is_template)
		VALUES ($1, 'Test Template', 'A test template', 'active', 3, 'member', $2, TRUE)
		RETURNING id
	`, workspaceID, userID).Scan(&tmplID)
	if err != nil {
		t.Fatalf("create template workflow: %v", err)
	}

	// Create 3 nodes.
	var n1, n2, n3 string
	err = pool.QueryRow(ctx, `
		INSERT INTO workflow_node (workflow_id, title, description, position_x, position_y, worker_type, critic_type, sort_order)
		VALUES ($1, 'Node 1', 'First node', 100, 50, 'agent', 'human', 0)
		RETURNING id
	`, tmplID).Scan(&n1)
	if err != nil {
		t.Fatalf("create node 1: %v", err)
	}
	err = pool.QueryRow(ctx, `
		INSERT INTO workflow_node (workflow_id, title, description, position_x, position_y, worker_type, critic_type, sort_order)
		VALUES ($1, 'Node 2', 'Second node', 350, 50, 'agent', 'human', 1)
		RETURNING id
	`, tmplID).Scan(&n2)
	if err != nil {
		t.Fatalf("create node 2: %v", err)
	}
	err = pool.QueryRow(ctx, `
		INSERT INTO workflow_node (workflow_id, title, description, position_x, position_y, worker_type, critic_type, sort_order)
		VALUES ($1, 'Node 3', 'Third node', 600, 50, 'human', 'agent', 2)
		RETURNING id
	`, tmplID).Scan(&n3)
	if err != nil {
		t.Fatalf("create node 3: %v", err)
	}

	// Create 2 edges: n1->n2, n2->n3.
	_, err = pool.Exec(ctx, `
		INSERT INTO workflow_edge (workflow_id, source_node_id, target_node_id)
		VALUES ($1, $2, $3)
	`, tmplID, n1, n2)
	if err != nil {
		t.Fatalf("create edge 1: %v", err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO workflow_edge (workflow_id, source_node_id, target_node_id)
		VALUES ($1, $2, $3)
	`, tmplID, n2, n3)
	if err != nil {
		t.Fatalf("create edge 2: %v", err)
	}

	wsUUID, _ := util.ParseUUID(workspaceID)
	tmplUUID, _ := util.ParseUUID(tmplID)

	t.Cleanup(func() {
		cleanupTemplateFixtures(t, pool, workspaceID)
	})
	return wsUUID, tmplUUID
}

// cleanupTemplateFixtures removes all data created for template tests.
func cleanupTemplateFixtures(t *testing.T, pool *pgxpool.Pool, workspaceID string) {
	t.Helper()
	ctx := context.Background()
	pool.Exec(ctx, `DELETE FROM workflow_edge WHERE workflow_id IN (SELECT id FROM workflow WHERE workspace_id = $1)`, workspaceID)
	pool.Exec(ctx, `DELETE FROM workflow_node WHERE workflow_id IN (SELECT id FROM workflow WHERE workspace_id = $1)`, workspaceID)
	pool.Exec(ctx, `DELETE FROM workflow WHERE workspace_id = $1`, workspaceID)
	pool.Exec(ctx, `DELETE FROM member WHERE workspace_id = $1`, workspaceID)
	pool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, workspaceID)
	pool.Exec(ctx, `DELETE FROM "user" WHERE email LIKE 'template-test-%'`)
}

// TestCloneWorkflowFromTemplate verifies that cloning a template workflow
// creates a new workflow with the same number of nodes and edges, properly
// mapped to the new workflow, within a single transaction.
func TestCloneWorkflowFromTemplate(t *testing.T) {
	pool := openTestPool(t)
	defer pool.Close()

	wsID, tmplID := setupTemplateFixtures(t, pool)
	q := db.New(pool)

	creatorID := testUUID(99)
	creatorID.Valid = true // our member didn't need a real user for this

	svc := NewWorkflowService(q, pool, nil, nil)

	cloned, clonedNodes, clonedEdges, err := svc.CloneWorkflowFromTemplate(
		context.Background(),
		tmplID,
		wsID,
		"Cloned Workflow",
		"A cloned workflow from template",
		"member",
		creatorID,
	)
	if err != nil {
		t.Fatalf("CloneWorkflowFromTemplate: %v", err)
	}

	// Verify the returned workflow.
	if cloned.Title != "Cloned Workflow" {
		t.Fatalf("expected title 'Cloned Workflow', got %q", cloned.Title)
	}
	if cloned.Status != "draft" {
		t.Fatalf("expected status 'draft', got %q", cloned.Status)
	}
	if cloned.IsTemplate {
		t.Fatal("cloned workflow must not be a template")
	}
	if util.UUIDToString(cloned.SourceTemplateID) != util.UUIDToString(tmplID) {
		t.Fatalf("source_template_id mismatch: got %s, want %s",
			util.UUIDToString(cloned.SourceTemplateID), util.UUIDToString(tmplID))
	}
	if cloned.MaxRetries != 3 {
		t.Fatalf("expected max_retries 3, got %d", cloned.MaxRetries)
	}

	// Verify node count.
	if len(clonedNodes) != 3 {
		t.Fatalf("expected 3 cloned nodes, got %d", len(clonedNodes))
	}
	// Verify each node belongs to the new workflow.
	for _, n := range clonedNodes {
		if util.UUIDToString(n.WorkflowID) != util.UUIDToString(cloned.ID) {
			t.Fatalf("node %s belongs to workflow %s, want %s",
				n.Title, util.UUIDToString(n.WorkflowID), util.UUIDToString(cloned.ID))
		}
	}

	// Verify edge count.
	if len(clonedEdges) != 2 {
		t.Fatalf("expected 2 cloned edges, got %d", len(clonedEdges))
	}
	// Verify each edge belongs to the new workflow and references cloned nodes.
	nodeIDs := make(map[string]bool)
	for _, n := range clonedNodes {
		nodeIDs[util.UUIDToString(n.ID)] = true
	}
	for _, e := range clonedEdges {
		if util.UUIDToString(e.WorkflowID) != util.UUIDToString(cloned.ID) {
			t.Fatalf("edge belongs to workflow %s, want %s",
				util.UUIDToString(e.WorkflowID), util.UUIDToString(cloned.ID))
		}
		if !nodeIDs[util.UUIDToString(e.SourceNodeID)] {
			t.Fatal("edge source_node_id references a node not in cloned nodes")
		}
		if !nodeIDs[util.UUIDToString(e.TargetNodeID)] {
			t.Fatal("edge target_node_id references a node not in cloned nodes")
		}
	}

	// Verify data in DB matches returned data.
	dbNodes, err := q.ListWorkflowNodes(context.Background(), cloned.ID)
	if err != nil {
		t.Fatalf("ListWorkflowNodes: %v", err)
	}
	if len(dbNodes) != 3 {
		t.Fatalf("DB has %d nodes, expected 3", len(dbNodes))
	}
	dbEdges, err := q.ListWorkflowEdges(context.Background(), cloned.ID)
	if err != nil {
		t.Fatalf("ListWorkflowEdges: %v", err)
	}
	if len(dbEdges) != 2 {
		t.Fatalf("DB has %d edges, expected 2", len(dbEdges))
	}

	// Clean up the cloned workflow.
	pool.Exec(context.Background(), `DELETE FROM workflow_edge WHERE workflow_id = $1`, cloned.ID)
	pool.Exec(context.Background(), `DELETE FROM workflow_node WHERE workflow_id = $1`, cloned.ID)
	pool.Exec(context.Background(), `DELETE FROM workflow WHERE id = $1`, cloned.ID)
}

// TestCloneWorkflowFromTemplate_RejectsNonTemplate verifies that attempting to
// clone a non-template workflow returns an error.
func TestCloneWorkflowFromTemplate_RejectsNonTemplate(t *testing.T) {
	pool := openTestPool(t)
	defer pool.Close()

	wsID, _ := setupTemplateFixtures(t, pool)
	q := db.New(pool)
	ctx := context.Background()

	// Create a non-template workflow directly.
	var workflowID string
	err := pool.QueryRow(ctx, `
		INSERT INTO workflow (workspace_id, title, status, max_retries, created_by_type, created_by_id, is_template)
		VALUES ($1, 'Not A Template', 'active', 3, 'member', $2, FALSE)
		RETURNING id
	`, util.UUIDToString(wsID), "00000000-0000-0000-0000-000000000001").Scan(&workflowID)
	if err != nil {
		t.Fatalf("create non-template workflow: %v", err)
	}
	wfUUID, _ := util.ParseUUID(workflowID)

	svc := NewWorkflowService(q, pool, nil, nil)

	_, _, _, err = svc.CloneWorkflowFromTemplate(ctx, wfUUID, wsID, "Clone", "", "member", testUUID(2))
	if err == nil {
		t.Fatal("expected error cloning non-template workflow, got nil")
	}
}

// TestSetWorkflowTemplate verifies that SetWorkflowTemplate toggles the
// is_template flag correctly.
func TestSetWorkflowTemplate(t *testing.T) {
	pool := openTestPool(t)
	defer pool.Close()

	wsID, _ := setupTemplateFixtures(t, pool)
	q := db.New(pool)
	ctx := context.Background()

	// Create an active workflow.
	var workflowID string
	err := pool.QueryRow(ctx, `
		INSERT INTO workflow (workspace_id, title, status, max_retries, created_by_type, created_by_id)
		VALUES ($1, 'Togglable Workflow', 'active', 3, 'member', $2)
		RETURNING id
	`, util.UUIDToString(wsID), "00000000-0000-0000-0000-000000000001").Scan(&workflowID)
	if err != nil {
		t.Fatalf("create workflow: %v", err)
	}
	wfUUID, _ := util.ParseUUID(workflowID)

	svc := NewWorkflowService(q, pool, nil, nil)

	// Set as template.
	updated, err := svc.SetWorkflowTemplate(ctx, wfUUID, true)
	if err != nil {
		t.Fatalf("SetWorkflowTemplate(true): %v", err)
	}
	if !updated.IsTemplate {
		t.Fatal("expected is_template=true after SetWorkflowTemplate(true)")
	}

	// Unset as template.
	updated, err = svc.SetWorkflowTemplate(ctx, wfUUID, false)
	if err != nil {
		t.Fatalf("SetWorkflowTemplate(false): %v", err)
	}
	if updated.IsTemplate {
		t.Fatal("expected is_template=false after SetWorkflowTemplate(false)")
	}

	// Verify DB state.
	wf, err := q.GetWorkflow(ctx, wfUUID)
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	if wf.IsTemplate {
		t.Fatal("expected is_template=false in DB after SetWorkflowTemplate(false)")
	}
}

// TestDeleteWorkflowWithTemplateCheck_BlocksWithDerivedWorkflows verifies that
// deleting a template with derived workflows returns an error.
func TestDeleteWorkflowWithTemplateCheck_BlocksWithDerivedWorkflows(t *testing.T) {
	pool := openTestPool(t)
	defer pool.Close()

	wsID, tmplID := setupTemplateFixtures(t, pool)
	q := db.New(pool)
	ctx := context.Background()

	svc := NewWorkflowService(q, pool, nil, nil)

	// Clone from the template to create a derived workflow.
	cloned, _, _, err := svc.CloneWorkflowFromTemplate(ctx, tmplID, wsID, "Derived Workflow", "", "member", testUUID(1))
	if err != nil {
		t.Fatalf("CloneWorkflowFromTemplate: %v", err)
	}
	defer func() {
		pool.Exec(ctx, `DELETE FROM workflow_edge WHERE workflow_id = $1`, cloned.ID)
		pool.Exec(ctx, `DELETE FROM workflow_node WHERE workflow_id = $1`, cloned.ID)
		pool.Exec(ctx, `DELETE FROM workflow WHERE id = $1`, cloned.ID)
	}()

	// Try to delete the template — should fail because there's a derived workflow.
	err = svc.DeleteWorkflowWithTemplateCheck(ctx, tmplID)
	if err == nil {
		t.Fatal("expected error deleting template with derived workflows, got nil")
	}
}

// TestDeleteWorkflowWithTemplateCheck_AllowsWithNoDerivedWorkflows verifies that
// deleting a template with no derived workflows does NOT return an error.
func TestDeleteWorkflowWithTemplateCheck_AllowsWithNoDerivedWorkflows(t *testing.T) {
	pool := openTestPool(t)
	defer pool.Close()

	_, tmplID := setupTemplateFixtures(t, pool)
	q := db.New(pool)
	ctx := context.Background()

	svc := NewWorkflowService(q, pool, nil, nil)

	// No derived workflows exist — should be allowed.
	err := svc.DeleteWorkflowWithTemplateCheck(ctx, tmplID)
	if err != nil {
		t.Fatalf("expected no error when no derived workflows exist, got: %v", err)
	}
}

// TestCanManageWorkflows verifies the CanManageWorkflows helper correctly reads
// the user.can_manage_workflows flag.
func TestCanManageWorkflows(t *testing.T) {
	pool := openTestPool(t)
	defer pool.Close()

	_, _ = setupTemplateFixtures(t, pool)
	q := db.New(pool)
	ctx := context.Background()

	// Create a user with can_manage_workflows flag set.
	var userID string
	err := pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email, can_manage_workflows)
		VALUES ('WF Admin User', 'wf-admin-test@multica.ai', TRUE)
		RETURNING id
	`).Scan(&userID)
	if err != nil {
		t.Fatalf("create admin user: %v", err)
	}
	defer pool.Exec(ctx, `DELETE FROM "user" WHERE email = 'wf-admin-test@multica.ai'`)

	userUUID, _ := util.ParseUUID(userID)

	svc := NewWorkflowService(q, pool, nil, nil)

	can, err := svc.CanManageWorkflows(ctx, userUUID)
	if err != nil {
		t.Fatalf("CanManageWorkflows: %v", err)
	}
	if !can {
		t.Fatal("expected can_manage_workflows=true for admin user")
	}

	// Create a non-admin user.
	var regularUserID string
	err = pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email, can_manage_workflows)
		VALUES ('Regular User', 'regular-test@multica.ai', FALSE)
		RETURNING id
	`).Scan(&regularUserID)
	if err != nil {
		t.Fatalf("create regular user: %v", err)
	}
	defer pool.Exec(ctx, `DELETE FROM "user" WHERE email = 'regular-test@multica.ai'`)

	regularUUID, _ := util.ParseUUID(regularUserID)

	can, err = svc.CanManageWorkflows(ctx, regularUUID)
	if err != nil {
		t.Fatalf("CanManageWorkflows(regular): %v", err)
	}
	if can {
		t.Fatal("expected can_manage_workflows=false for regular user")
	}
}

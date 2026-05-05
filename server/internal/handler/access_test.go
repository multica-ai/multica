package handler

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// EVAL for Phase 1 — every (role × project access × membership × is_private)
// combination produces the expected access result. Owner/admin always passes.
// Workspace project always passes for any member. Restricted project passes
// only for explicit members. Standalone is_private passes only for the
// creator + admins.

type accessFixture struct {
	ownerUserID    string
	adminUserID    string
	memberUserID   string
	outsideUserID  string
	workspaceID    string
	openProjectID  string
	restrictedID   string
}

func setupAccessFixture(t *testing.T) accessFixture {
	t.Helper()
	ctx := context.Background()

	mkUser := func(name, email string) string {
		var id string
		if err := testPool.QueryRow(ctx,
			`INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
			name, email,
		).Scan(&id); err != nil {
			t.Fatalf("create user: %v", err)
		}
		return id
	}

	mkMember := func(userID, role string) {
		if _, err := testPool.Exec(ctx,
			`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, $3)`,
			testWorkspaceID, userID, role,
		); err != nil {
			t.Fatalf("create member: %v", err)
		}
	}

	owner := mkUser("Access Owner", "access-owner@multica.test")
	admin := mkUser("Access Admin", "access-admin@multica.test")
	member := mkUser("Access Member", "access-member@multica.test")
	outside := mkUser("Access Outside", "access-outside@multica.test")
	mkMember(owner, "owner")
	mkMember(admin, "admin")
	mkMember(member, "member")
	mkMember(outside, "member")

	mkProject := func(title, access string) string {
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO project (workspace_id, title, status, priority, access)
			VALUES ($1, $2, 'in_progress', 'medium', $3)
			RETURNING id
		`, testWorkspaceID, title, access).Scan(&id); err != nil {
			t.Fatalf("create project: %v", err)
		}
		return id
	}

	openID := mkProject("access-open", "workspace")
	restrictedID := mkProject("access-restricted", "restricted")

	if _, err := testPool.Exec(ctx,
		`INSERT INTO project_member (workspace_id, project_id, user_id) VALUES ($1, $2, $3)`,
		testWorkspaceID, restrictedID, member,
	); err != nil {
		t.Fatalf("add project member: %v", err)
	}

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM project_member WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM project WHERE id = ANY($1)`, []string{openID, restrictedID})
		testPool.Exec(ctx, `DELETE FROM member WHERE user_id = ANY($1)`, []string{owner, admin, member, outside})
		testPool.Exec(ctx, `DELETE FROM "user" WHERE id = ANY($1)`, []string{owner, admin, member, outside})
	})

	return accessFixture{
		ownerUserID:   owner,
		adminUserID:   admin,
		memberUserID:  member,
		outsideUserID: outside,
		workspaceID:   testWorkspaceID,
		openProjectID: openID,
		restrictedID:  restrictedID,
	}
}

func loadMember(t *testing.T, userID string) db.Member {
	t.Helper()
	m, err := testHandler.Queries.GetMemberByUserAndWorkspace(context.Background(), db.GetMemberByUserAndWorkspaceParams{
		UserID:      util.ParseUUID(userID),
		WorkspaceID: util.ParseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("load member %s: %v", userID, err)
	}
	return m
}

func loadProject(t *testing.T, projectID string) db.Project {
	t.Helper()
	p, err := testHandler.Queries.GetProject(context.Background(), util.ParseUUID(projectID))
	if err != nil {
		t.Fatalf("load project %s: %v", projectID, err)
	}
	return p
}

func TestCanAccessProject_OwnerAdminAlwaysPass(t *testing.T) {
	f := setupAccessFixture(t)
	ctx := context.Background()

	owner := loadMember(t, f.ownerUserID)
	admin := loadMember(t, f.adminUserID)
	open := loadProject(t, f.openProjectID)
	restricted := loadProject(t, f.restrictedID)

	for _, m := range []db.Member{owner, admin} {
		for _, p := range []db.Project{open, restricted} {
			if !testHandler.canAccessProject(ctx, m, p) {
				t.Fatalf("%s should access %s", m.Role, p.Access)
			}
		}
	}
}

func TestCanAccessProject_MemberOnlyOpenAndExplicitRestricted(t *testing.T) {
	f := setupAccessFixture(t)
	ctx := context.Background()

	member := loadMember(t, f.memberUserID)
	outside := loadMember(t, f.outsideUserID)
	open := loadProject(t, f.openProjectID)
	restricted := loadProject(t, f.restrictedID)

	if !testHandler.canAccessProject(ctx, member, open) {
		t.Fatalf("member should access open project")
	}
	if !testHandler.canAccessProject(ctx, outside, open) {
		t.Fatalf("outside member should access open project")
	}
	if !testHandler.canAccessProject(ctx, member, restricted) {
		t.Fatalf("explicit project member should access restricted project")
	}
	if testHandler.canAccessProject(ctx, outside, restricted) {
		t.Fatalf("non-member member should NOT access restricted project")
	}
}

func TestCanAccessIssue_StandalonePrivacyRules(t *testing.T) {
	f := setupAccessFixture(t)
	ctx := context.Background()

	creator := loadMember(t, f.memberUserID)
	other := loadMember(t, f.outsideUserID)
	admin := loadMember(t, f.adminUserID)

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, position, number, is_private)
		VALUES ($1, 'standalone-private', 'todo', 'medium', 'member', $2, 0, 9999, true)
		RETURNING id
	`, f.workspaceID, f.memberUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
	})

	issue, err := testHandler.Queries.GetIssue(ctx, util.ParseUUID(issueID))
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}

	if !testHandler.canAccessIssue(ctx, creator, issue) {
		t.Fatalf("creator should access own private standalone issue")
	}
	if !testHandler.canAccessIssue(ctx, admin, issue) {
		t.Fatalf("admin should access any private standalone issue")
	}
	if testHandler.canAccessIssue(ctx, other, issue) {
		t.Fatalf("other member should NOT access private standalone issue")
	}
}

func TestCanAccessIssue_ProjectInheritance(t *testing.T) {
	f := setupAccessFixture(t)
	ctx := context.Background()

	member := loadMember(t, f.memberUserID)
	outside := loadMember(t, f.outsideUserID)

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, position, number, project_id)
		VALUES ($1, 'restricted-project-issue', 'todo', 'medium', 'member', $2, 0, 9998, $3)
		RETURNING id
	`, f.workspaceID, f.memberUserID, f.restrictedID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
	})

	issue, err := testHandler.Queries.GetIssue(ctx, util.ParseUUID(issueID))
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}

	if !testHandler.canAccessIssue(ctx, member, issue) {
		t.Fatalf("project member should access issue in restricted project")
	}
	if testHandler.canAccessIssue(ctx, outside, issue) {
		t.Fatalf("outsider should NOT access issue in restricted project")
	}
}

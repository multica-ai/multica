package handler

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// TestListIssuesMyAgentsToken verifies the {my_agents} token in assignee_filters
// expands server-side to the issues assigned to agents the requesting user owns,
// so a saved view never freezes a concrete agent UUID set.
func TestListIssuesMyAgentsToken(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	f := seedIssueFilterFixture(t)
	ws := "workspace_id=" + testWorkspaceID

	// agentProg is assigned to an agent owned by testUserID.
	got := listIssueIDs(t, ws+"&assignee_filters=agent:{my_agents}")
	assertContains(t, got, f.agentProg)
	assertExcludes(t, got, f.mineTodo, f.otherTodo, f.noAssignee)

	// {my_agents} only pairs with the agent actor type, and only in
	// assignee_filters (a creator is always a member).
	expectStatus(t, ws+"&assignee_filters=member:{my_agents}", http.StatusBadRequest)
	expectStatus(t, ws+"&creator_filters=agent:{my_agents}", http.StatusBadRequest)
}

// TestListIssuesMySquadsToken verifies the {my_squads} token expands to issues
// assigned to squads the requesting user belongs to.
func TestListIssuesMySquadsToken(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	f := seedIssueFilterFixture(t)
	ws := "workspace_id=" + testWorkspaceID
	ctx := context.Background()
	suffix := time.Now().UnixNano()

	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, leader_id, creator_id)
		VALUES ($1, $2, $3, $4) RETURNING id
	`, testWorkspaceID, fmt.Sprintf("Token Squad %d", suffix), f.agentID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID) })

	if _, err := testPool.Exec(ctx, `
		INSERT INTO squad_member (squad_id, member_type, member_id) VALUES ($1, 'member', $2)
	`, squadID, testUserID); err != nil {
		t.Fatalf("add squad member: %v", err)
	}

	var number int32
	if err := testPool.QueryRow(ctx, `
		UPDATE workspace SET issue_counter = GREATEST(
			issue_counter, (SELECT COALESCE(MAX(number), 0) FROM issue WHERE workspace_id = $1)
		) + 1 WHERE id = $1 RETURNING issue_counter
	`, testWorkspaceID).Scan(&number); err != nil {
		t.Fatalf("next issue number: %v", err)
	}
	var squadIssueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (
			workspace_id, title, status, priority,
			assignee_type, assignee_id, creator_type, creator_id, position, number
		)
		VALUES ($1, $2, 'todo', 'none', 'squad', $3, 'member', $4, 0, $5)
		RETURNING id
	`, testWorkspaceID, fmt.Sprintf("squad issue %d", suffix), squadID, testUserID, number).Scan(&squadIssueID); err != nil {
		t.Fatalf("create squad issue: %v", err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, squadIssueID) })

	got := listIssueIDs(t, ws+"&assignee_filters=squad:{my_squads}")
	assertContains(t, got, squadIssueID)
	assertExcludes(t, got, f.mineTodo, f.agentProg)

	expectStatus(t, ws+"&assignee_filters=agent:{my_squads}", http.StatusBadRequest)
}

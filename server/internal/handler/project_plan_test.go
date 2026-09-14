package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/projectplan"
	"github.com/multica-ai/multica/server/internal/testutil"
)

type projectPlanHandlerRoute struct {
	name    string
	method  string
	handler http.HandlerFunc
	params  []string
	body    any
}

func projectPlanHandlerRoutes() []projectPlanHandlerRoute {
	planID := uuid.NewString()
	phaseID := uuid.NewString()
	partID := uuid.NewString()
	issueID := uuid.NewString()
	return []projectPlanHandlerRoute{
		{name: "create manual", method: http.MethodPost, handler: testHandler.CreateProjectPlan, body: map[string]any{"kind": "prd", "title": "Plan"}},
		{name: "create from issue", method: http.MethodPost, handler: testHandler.CreateProjectPlanFromIssue, body: map[string]any{"kind": "prd", "source_issue_id": issueID}},
		{name: "update plan", method: http.MethodPatch, handler: testHandler.UpdateProjectPlan, params: []string{"planId", planID}, body: map[string]any{"title": "Updated"}},
		{name: "supersede", method: http.MethodPost, handler: testHandler.SupersedeProjectPlan, params: []string{"planId", planID}, body: map[string]any{}},
		{name: "add phase", method: http.MethodPost, handler: testHandler.AddProjectPlanPhase, params: []string{"planId", planID}, body: map[string]any{"title": "Phase"}},
		{name: "update phase", method: http.MethodPatch, handler: testHandler.UpdateProjectPlanPhase, params: []string{"planId", planID, "phaseId", phaseID}, body: map[string]any{"title": "Updated"}},
		{name: "reorder phases", method: http.MethodPatch, handler: testHandler.ReorderProjectPlanPhases, params: []string{"planId", planID}, body: map[string]any{"ordered_ids": []string{phaseID}}},
		{name: "delete phase", method: http.MethodDelete, handler: testHandler.DeleteProjectPlanPhase, params: []string{"planId", planID, "phaseId", phaseID}},
		{name: "add part", method: http.MethodPost, handler: testHandler.AddProjectPlanPart, params: []string{"planId", planID, "phaseId", phaseID}, body: map[string]any{"title": "Part"}},
		{name: "update part", method: http.MethodPatch, handler: testHandler.UpdateProjectPlanPart, params: []string{"planId", planID, "partId", partID}, body: map[string]any{"title": "Updated"}},
		{name: "reorder parts", method: http.MethodPatch, handler: testHandler.ReorderProjectPlanParts, params: []string{"planId", planID, "phaseId", phaseID}, body: map[string]any{"ordered_ids": []string{partID}}},
		{name: "delete part", method: http.MethodDelete, handler: testHandler.DeleteProjectPlanPart, params: []string{"planId", planID, "partId", partID}},
		{name: "link issue", method: http.MethodPost, handler: testHandler.LinkProjectPlanIssue, params: []string{"planId", planID, "partId", partID, "issueId", issueID}},
		{name: "unlink issue", method: http.MethodDelete, handler: testHandler.UnlinkProjectPlanIssue, params: []string{"planId", planID, "partId", partID, "issueId", issueID}},
		{name: "delete impact", method: http.MethodGet, handler: testHandler.GetProjectPlanDeleteImpact, params: []string{"planId", planID}},
		{name: "delete plan", method: http.MethodDelete, handler: testHandler.DeleteProjectPlan, params: []string{"planId", planID}},
	}
}

func projectPlanHandlerRequest(route projectPlanHandlerRoute, projectID string) *http.Request {
	params := append([]string{"id", projectID}, route.params...)
	return testutil.WithURLParams(
		newRequest(route.method, "/api/projects/"+projectID+"/plans", route.body),
		params...,
	)
}

func TestIssueDeletionRetainsPlanMembershipSnapshotsAndCoverage(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database unavailable")
	}
	ctx := context.Background()
	withFeatureFlag(t, testHandler, featureflags.ProjectPlans, true)

	projectID := dbfx.Project(t, "Plan deletion coverage")
	planID := dbfx.Insert(t, "project_plan", testutil.Cols{
		"workspace_id": testWorkspaceID, "project_id": projectID, "version": 1,
		"kind": "prd", "origin": "manual", "title": "Deletion coverage",
		"created_by_type": "member", "created_by_id": testUserID,
	})
	phaseID := dbfx.Insert(t, "project_plan_phase", testutil.Cols{
		"project_plan_id": planID, "title": "Phase", "position": 0,
	})
	part := func(title string, position int) string {
		return dbfx.Insert(t, "project_plan_part", testutil.Cols{
			"project_plan_id": planID, "project_plan_phase_id": phaseID,
			"title": title, "position": position,
		})
	}
	completePart := part("Complete", 0)
	inProgressPart := part("In progress", 1)
	startedZeroPart := part("Started at zero", 2)
	notStartedPart := part("Not started", 3)
	part("No tasks", 4)
	cancelledPart := part("Cancelled", 5)
	deletedPart := part("Deleted", 6)
	mixedPart := part("Mixed", 7)

	issue := func(title, status string) string {
		return dbfx.Issue(t, title, testutil.Cols{"project_id": projectID, "status": status})
	}
	completeIssue := issue("Complete issue", "done")
	inProgressDone := issue("In-progress completed issue", "done")
	inProgressOpen := issue("In-progress active issue", "in_progress")
	startedZeroIssue := issue("Started zero issue", "in_progress")
	notStartedIssue := issue("Not started issue", "todo")
	cancelledFirst := issue("Cancelled first issue", "cancelled")
	cancelledSecond := issue("Cancelled second issue", "cancelled")
	deletedSingle := issue("Deleted single issue", "todo")
	deletedBatchFirst := issue("Deleted batch first issue", "todo")
	deletedBatchSecond := issue("Deleted batch second issue", "todo")
	mixedLive := issue("Mixed live issue", "todo")
	mixedDeleted := issue("Mixed deleted issue", "todo")

	link := func(partID, issueID string, number int, title string) string {
		return dbfx.Insert(t, "project_plan_part_issue", testutil.Cols{
			"project_plan_id": planID, "project_plan_part_id": partID, "issue_id": issueID,
			"issue_number_snapshot": number, "issue_title_snapshot": title,
		})
	}
	link(completePart, completeIssue, 101, "Complete snapshot")
	link(inProgressPart, inProgressDone, 102, "In-progress done snapshot")
	link(inProgressPart, inProgressOpen, 103, "In-progress active snapshot")
	link(startedZeroPart, startedZeroIssue, 104, "Started zero snapshot")
	link(notStartedPart, notStartedIssue, 105, "Not started snapshot")
	link(cancelledPart, cancelledFirst, 106, "Cancelled first snapshot")
	link(cancelledPart, cancelledSecond, 107, "Cancelled second snapshot")
	singleLinkID := link(deletedPart, deletedSingle, 108, "Deleted single snapshot")
	link(deletedPart, deletedBatchFirst, 109, "Deleted batch first snapshot")
	link(deletedPart, deletedBatchSecond, 110, "Deleted batch second snapshot")
	link(mixedPart, mixedLive, 111, "Mixed live snapshot")
	link(mixedPart, mixedDeleted, 112, "Mixed deleted snapshot")

	deleteRequest := withURLParam(
		newRequest(http.MethodDelete, "/api/issues/"+deletedSingle, nil), "id", deletedSingle,
	)
	testutil.Call(t, testHandler.DeleteIssue, deleteRequest).Want(http.StatusNoContent)

	var singleLive bool
	var singleNumber int
	var singleTitle string
	if err := testPool.QueryRow(ctx, `
		SELECT issue_id IS NOT NULL, issue_number_snapshot, issue_title_snapshot
		FROM project_plan_part_issue WHERE id = $1`, singleLinkID,
	).Scan(&singleLive, &singleNumber, &singleTitle); err != nil {
		t.Fatalf("load single-deleted membership: %v", err)
	}
	if singleLive || singleNumber != 108 || singleTitle != "Deleted single snapshot" {
		t.Fatalf("single-deleted membership = live:%t number:%d title:%q, want false/108/snapshot",
			singleLive, singleNumber, singleTitle)
	}

	batchRequest := newRequest(http.MethodDelete, "/api/issues/batch", map[string]any{
		"issue_ids": []string{deletedBatchFirst, deletedBatchSecond, mixedDeleted},
	})
	testutil.Call(t, testHandler.BatchDeleteIssues, batchRequest).Want(http.StatusOK)

	var retainedLiveLinks int
	if err := testPool.QueryRow(ctx, `
		SELECT COUNT(*) FROM project_plan_part_issue
		WHERE issue_id IN ($1, $2, $3, $4)`,
		deletedSingle, deletedBatchFirst, deletedBatchSecond, mixedDeleted,
	).Scan(&retainedLiveLinks); err != nil {
		t.Fatalf("count dangling plan memberships: %v", err)
	}
	if retainedLiveLinks != 0 {
		t.Fatalf("live plan memberships for deleted issues = %d, want 0", retainedLiveLinks)
	}

	request := withURLParam(
		newRequest(http.MethodGet, "/api/projects/"+projectID+"/plan", nil), "id", projectID,
	)
	var overview projectplan.Overview
	testutil.Call(t, testHandler.GetActiveProjectPlan, request).Want(http.StatusOK).JSON(&overview)
	parts := make(map[string]projectplan.Part)
	for _, phase := range overview.Phases {
		for _, planPart := range phase.Parts {
			parts[planPart.Title] = planPart
		}
	}
	assertPart := func(title, state string, rollup projectplan.TaskRollup) projectplan.Part {
		planPart, ok := parts[title]
		if !ok {
			t.Fatalf("missing part %q in overview", title)
		}
		if planPart.CoverageState != state || planPart.Rollup != rollup {
			t.Fatalf("part %q = state:%q rollup:%+v, want %q %+v",
				title, planPart.CoverageState, planPart.Rollup, state, rollup)
		}
		return planPart
	}
	assertPart("Complete", projectplan.CoverageComplete, projectplan.TaskRollup{TasksDone: 1, TasksTotal: 1, Percent: 100})
	assertPart("In progress", projectplan.CoverageInProgress, projectplan.TaskRollup{TasksDone: 1, TasksTotal: 2, Percent: 50})
	assertPart("Started at zero", projectplan.CoverageInProgress, projectplan.TaskRollup{TasksDone: 0, TasksTotal: 1, Percent: 0})
	assertPart("Not started", projectplan.CoverageNotStarted, projectplan.TaskRollup{TasksDone: 0, TasksTotal: 1, Percent: 0})
	assertPart("No tasks", projectplan.CoverageNoTasksYet, projectplan.TaskRollup{})
	assertPart("Cancelled", projectplan.CoverageCoveredNoActiveTasks, projectplan.TaskRollup{})
	deleted := assertPart("Deleted", projectplan.CoverageCoveredNoActiveTasks, projectplan.TaskRollup{})
	mixed := assertPart("Mixed", projectplan.CoverageNotStarted, projectplan.TaskRollup{TasksDone: 0, TasksTotal: 1, Percent: 0})

	var foundDeletedSnapshot bool
	for _, row := range deleted.Issues {
		if row.Deleted && row.Title == "Deleted single snapshot" && row.Number == 108 && row.ID == nil {
			foundDeletedSnapshot = true
		}
	}
	if !foundDeletedSnapshot {
		t.Fatalf("deleted plan rows did not render the retained snapshot: %+v", deleted.Issues)
	}
	if len(mixed.Issues) != 2 || mixed.Issues[0].Deleted == mixed.Issues[1].Deleted {
		t.Fatalf("mixed plan rows = %+v, want one live and one deleted row", mixed.Issues)
	}
}

func TestGetActiveProjectPlanFlagAndResponse(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database unavailable")
	}
	projectID := dbfx.Project(t, "Plan read handler")
	planID := dbfx.Insert(t, "project_plan", testutil.Cols{
		"workspace_id": testWorkspaceID, "project_id": projectID, "version": 1,
		"kind": "prd", "origin": "manual", "title": "Handler overview",
		"created_by_type": "member", "created_by_id": testUserID,
	})
	phaseID := dbfx.Insert(t, "project_plan_phase", testutil.Cols{
		"project_plan_id": planID, "title": "Phase", "position": 0,
	})
	partID := dbfx.Insert(t, "project_plan_part", testutil.Cols{
		"project_plan_id": planID, "project_plan_phase_id": phaseID,
		"title": "Gap", "position": 0,
	})

	request := func() *http.Request {
		return withURLParam(
			newRequest(http.MethodGet, "/api/projects/"+projectID+"/plan", nil),
			"id", projectID,
		)
	}

	originalFlags := testHandler.FeatureFlags
	testHandler.FeatureFlags = nil
	t.Cleanup(func() { testHandler.FeatureFlags = originalFlags })
	// No flag provider: project_plans defaults off, and the read route fails
	// closed with the same 404 it returns for "no active plan".
	testutil.Call(t, testHandler.GetActiveProjectPlan, request()).Want(http.StatusNotFound)

	withFeatureFlag(t, testHandler, featureflags.ProjectPlans, false)
	testutil.Call(t, testHandler.GetActiveProjectPlan, request()).Want(http.StatusNotFound)

	withFeatureFlag(t, testHandler, featureflags.ProjectPlans, true)
	var overview projectplan.Overview
	testutil.Call(t, testHandler.GetActiveProjectPlan, request()).Want(http.StatusOK).JSON(&overview)
	if overview.Plan.ID != planID || overview.Plan.ProjectID != projectID {
		t.Fatalf("plan = %+v, want plan %s in project %s", overview.Plan, planID, projectID)
	}
	if overview.Rollup.PartsTotal != 1 || overview.Rollup.PartsWithoutTasks != 1 {
		t.Fatalf("rollup = %+v, want one uncovered part", overview.Rollup)
	}
	if len(overview.UncoveredParts) != 1 || overview.UncoveredParts[0].ID != partID {
		t.Fatalf("uncovered_parts = %+v, want %s", overview.UncoveredParts, partID)
	}
}

func TestGetProjectPlanScopesRetainedVersionToProject(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database unavailable")
	}
	projectID := dbfx.Project(t, "Plan read owner")
	otherProjectID := dbfx.Project(t, "Other plan owner")
	planID := dbfx.Insert(t, "project_plan", testutil.Cols{
		"workspace_id": testWorkspaceID, "project_id": projectID, "version": 1,
		"kind": "prd", "origin": "manual", "title": "Retained overview",
		"created_by_type": "member", "created_by_id": testUserID,
		"superseded_at": testutil.Raw("now()"),
	})
	withFeatureFlag(t, testHandler, featureflags.ProjectPlans, true)

	request := func(targetProjectID, targetPlanID string) *http.Request {
		req := newRequest(http.MethodGet,
			"/api/projects/"+targetProjectID+"/plans/"+targetPlanID, nil)
		return withURLParams(req, "id", targetProjectID, "planId", targetPlanID)
	}

	var overview projectplan.Overview
	testutil.Call(t, testHandler.GetProjectPlan, request(projectID, planID)).Want(http.StatusOK).JSON(&overview)
	if overview.Plan.ID != planID || !overview.Plan.Superseded {
		t.Fatalf("retained plan = %+v", overview.Plan)
	}
	testutil.Call(t, testHandler.GetProjectPlan, request(otherProjectID, planID)).Want(http.StatusNotFound)
	testutil.Call(t, testHandler.GetProjectPlan, request(projectID, "not-a-uuid")).Want(http.StatusBadRequest)
}

func TestProjectPlanWriteRoutesFlagOffAndAuthorization(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database unavailable")
	}
	projectID := dbfx.Project(t, "Plan write route gates")

	for _, route := range projectPlanHandlerRoutes() {
		t.Run(route.name+" flag off", func(t *testing.T) {
			withFeatureFlag(t, testHandler, featureflags.ProjectPlans, false)
			testutil.Call(t, route.handler, projectPlanHandlerRequest(route, projectID)).Want(http.StatusNotFound)
		})
		t.Run(route.name+" authorization denial", func(t *testing.T) {
			withFeatureFlag(t, testHandler, featureflags.ProjectPlans, true)
			request := projectPlanHandlerRequest(route, projectID)
			request.Header.Set("X-Workspace-ID", uuid.NewString())
			testutil.Call(t, route.handler, request).Want(http.StatusNotFound)
		})
	}
	if count := dbfx.Count(t, `SELECT count(*) FROM project_plan WHERE project_id = $1`, projectID); count != 0 {
		t.Fatalf("gated routes wrote %d plans", count)
	}
}

func TestProjectPlanWriteRoutesBogusPlan(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database unavailable")
	}
	withFeatureFlag(t, testHandler, featureflags.ProjectPlans, true)
	projectID := dbfx.Project(t, "Bogus plan routes")

	for _, route := range projectPlanHandlerRoutes() {
		if route.name == "create manual" || route.name == "create from issue" {
			continue
		}
		t.Run(route.name, func(t *testing.T) {
			testutil.Call(t, route.handler, projectPlanHandlerRequest(route, projectID)).Want(http.StatusNotFound)
		})
	}
}

func TestProjectPlanWriteRoutesHappyPath(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database unavailable")
	}
	withFeatureFlag(t, testHandler, featureflags.ProjectPlans, true)
	projectID := dbfx.Project(t, "Plan write happy path")
	agentID := createHandlerTestAgent(t, "Plan author agent", nil)
	taskID := createHandlerTestTaskForAgent(t, agentID)

	var planID string
	t.Run("create manual records the real agent", func(t *testing.T) {
		request := testutil.WithURLParams(newRequest(http.MethodPost, "/api/projects/"+projectID+"/plans", map[string]any{
			"kind": "prd", "title": "Agent plan", "description": "Initial", "attributes": map[string]any{"owner": "agent"},
		}), "id", projectID)
		request.Header.Set("X-Agent-ID", agentID)
		request.Header.Set("X-Task-ID", taskID)
		response := testutil.Decode[projectPlanResponse](t, testHandler.CreateProjectPlan, request, http.StatusCreated)
		planID = response.ID
		if response.CreatedByType != "agent" || response.CreatedByID != agentID {
			t.Fatalf("creator = %s/%s, want agent/%s", response.CreatedByType, response.CreatedByID, agentID)
		}
		var actorType, actorID string
		dbfx.QueryRow(t, `SELECT created_by_type, created_by_id FROM project_plan WHERE id = $1`, planID).Scan(&actorType, &actorID)
		if actorType != "agent" || actorID != agentID {
			t.Fatalf("stored creator = %s/%s, want agent/%s", actorType, actorID, agentID)
		}
	})

	t.Run("update plan", func(t *testing.T) {
		request := testutil.WithURLParams(newRequest(http.MethodPatch, "/plans/"+planID, map[string]any{
			"title": "Updated plan",
		}), "id", projectID, "planId", planID)
		response := testutil.Decode[projectPlanResponse](t, testHandler.UpdateProjectPlan, request, http.StatusOK)
		if response.Title != "Updated plan" {
			t.Fatalf("title = %q, want Updated plan", response.Title)
		}
	})

	var phaseA, phaseB string
	t.Run("add phases", func(t *testing.T) {
		add := func(title string, position int32) string {
			request := testutil.WithURLParams(newRequest(http.MethodPost, "/phases", map[string]any{
				"title": title, "position": position,
			}), "id", projectID, "planId", planID)
			return testutil.Decode[projectPlanPhaseResponse](t, testHandler.AddProjectPlanPhase, request, http.StatusCreated).ID
		}
		phaseA = add("Phase A", 0)
		phaseB = add("Phase B", 1)
	})

	t.Run("reorder phases", func(t *testing.T) {
		request := testutil.WithURLParams(newRequest(http.MethodPatch, "/phases/reorder", map[string]any{
			"ordered_ids": []string{phaseB, phaseA},
		}), "id", projectID, "planId", planID)
		testutil.Call(t, testHandler.ReorderProjectPlanPhases, request).Want(http.StatusNoContent)
	})

	t.Run("update phase", func(t *testing.T) {
		request := testutil.WithURLParams(newRequest(http.MethodPatch, "/phases/"+phaseA, map[string]any{
			"title": "Updated phase",
		}), "id", projectID, "planId", planID, "phaseId", phaseA)
		response := testutil.Decode[projectPlanPhaseResponse](t, testHandler.UpdateProjectPlanPhase, request, http.StatusOK)
		if response.Title != "Updated phase" {
			t.Fatalf("title = %q, want Updated phase", response.Title)
		}
	})

	var partA, partB string
	t.Run("add parts", func(t *testing.T) {
		add := func(title string, position int32) string {
			request := testutil.WithURLParams(newRequest(http.MethodPost, "/parts", map[string]any{
				"title": title, "acceptance_criteria": "Done", "position": position,
			}), "id", projectID, "planId", planID, "phaseId", phaseA)
			return testutil.Decode[projectPlanPartResponse](t, testHandler.AddProjectPlanPart, request, http.StatusCreated).ID
		}
		partA = add("Part A", 0)
		partB = add("Part B", 1)
	})

	t.Run("reorder parts", func(t *testing.T) {
		request := testutil.WithURLParams(newRequest(http.MethodPatch, "/parts/reorder", map[string]any{
			"ordered_ids": []string{partB, partA},
		}), "id", projectID, "planId", planID, "phaseId", phaseA)
		testutil.Call(t, testHandler.ReorderProjectPlanParts, request).Want(http.StatusNoContent)
	})

	t.Run("update part", func(t *testing.T) {
		request := testutil.WithURLParams(newRequest(http.MethodPatch, "/parts/"+partA, map[string]any{
			"title": "Updated part",
		}), "id", projectID, "planId", planID, "partId", partA)
		response := testutil.Decode[projectPlanPartResponse](t, testHandler.UpdateProjectPlanPart, request, http.StatusOK)
		if response.Title != "Updated part" {
			t.Fatalf("title = %q, want Updated part", response.Title)
		}
	})

	issueID := dbfx.Issue(t, "Linked plan issue", testutil.Cols{"project_id": projectID})
	t.Run("link and unlink issue", func(t *testing.T) {
		request := func(method string) *http.Request {
			return testutil.WithURLParams(newRequest(method, "/issues/"+issueID, nil),
				"id", projectID, "planId", planID, "partId", partA, "issueId", issueID)
		}
		link := testutil.Decode[projectPlanIssueLinkResponse](t, testHandler.LinkProjectPlanIssue, request(http.MethodPost), http.StatusCreated)
		if link.IssueID != issueID || link.ProjectPlanPartID != partA {
			t.Fatalf("link = %+v", link)
		}
		testutil.Call(t, testHandler.UnlinkProjectPlanIssue, request(http.MethodDelete)).Want(http.StatusNoContent)
	})

	t.Run("delete impact", func(t *testing.T) {
		request := testutil.WithURLParams(newRequest(http.MethodGet, "/delete-impact", nil), "id", projectID, "planId", planID)
		testutil.Decode[projectPlanDeleteImpactResponse](t, testHandler.GetProjectPlanDeleteImpact, request, http.StatusOK)
	})

	t.Run("delete part", func(t *testing.T) {
		request := testutil.WithURLParams(newRequest(http.MethodDelete, "/parts/"+partB, nil),
			"id", projectID, "planId", planID, "partId", partB)
		testutil.Call(t, testHandler.DeleteProjectPlanPart, request).Want(http.StatusNoContent)
	})

	t.Run("delete phase", func(t *testing.T) {
		request := testutil.WithURLParams(newRequest(http.MethodDelete, "/phases/"+phaseB, nil),
			"id", projectID, "planId", planID, "phaseId", phaseB)
		testutil.Call(t, testHandler.DeleteProjectPlanPhase, request).Want(http.StatusNoContent)
	})

	var supersedingPlanID string
	t.Run("supersede", func(t *testing.T) {
		request := testutil.WithURLParams(newRequest(http.MethodPost, "/supersede", map[string]any{
			"title": "Version two",
		}), "id", projectID, "planId", planID)
		response := testutil.Decode[projectPlanResponse](t, testHandler.SupersedeProjectPlan, request, http.StatusCreated)
		supersedingPlanID = response.ID
		if response.Version != 2 || response.Title != "Version two" {
			t.Fatalf("superseding plan = %+v", response)
		}
	})

	t.Run("delete plan", func(t *testing.T) {
		request := testutil.WithURLParams(newRequest(http.MethodDelete, "/plans/"+supersedingPlanID, nil),
			"id", projectID, "planId", supersedingPlanID)
		testutil.Decode[projectPlanDeleteImpactResponse](t, testHandler.DeleteProjectPlan, request, http.StatusOK)
	})

	t.Run("create from issue", func(t *testing.T) {
		issueProjectID := dbfx.Project(t, "Issue-sourced plan")
		sourceIssueID := dbfx.Issue(t, "Source issue", testutil.Cols{
			"project_id": issueProjectID, "description": "Source description",
		})
		request := testutil.WithURLParams(newRequest(http.MethodPost, "/plans/from-issue", map[string]any{
			"kind": "prd", "source_issue_id": sourceIssueID,
		}), "id", issueProjectID)
		response := testutil.Decode[projectPlanResponse](t, testHandler.CreateProjectPlanFromIssue, request, http.StatusCreated)
		if response.Origin != "issue" || response.SourceIssueID == nil || *response.SourceIssueID != sourceIssueID {
			t.Fatalf("issue-sourced plan = %+v", response)
		}
	})
}

func TestAgentAuthorsProjectPlanFromIssue(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database unavailable")
	}
	withFeatureFlag(t, testHandler, featureflags.ProjectPlans, true)
	projectID := dbfx.Project(t, "Agent-authored issue plan")
	agentID := createHandlerTestAgent(t, "Plan author agent", nil)
	taskID := createHandlerTestTaskForAgent(t, agentID)
	sourceDescription := "Exact PRD description used for provenance"
	sourceIssueID := dbfx.Issue(t, "Source PRD", testutil.Cols{
		"project_id": projectID, "description": sourceDescription,
	})

	agentRequest := func(method, path string, body any, params ...string) *http.Request {
		request := testutil.WithURLParams(newRequest(method, path, body), params...)
		request.Header.Set("X-Agent-ID", agentID)
		request.Header.Set("X-Task-ID", taskID)
		return request
	}

	planResponse := testutil.Decode[projectPlanResponse](t, testHandler.CreateProjectPlanFromIssue,
		agentRequest(http.MethodPost, "/plans/from-issue", map[string]any{
			"kind": "prd", "source_issue_id": sourceIssueID,
		}, "id", projectID), http.StatusCreated)
	if planResponse.Origin != "issue" || planResponse.CreatedByType != "agent" || planResponse.CreatedByID != agentID {
		t.Fatalf("authored plan = %+v, want issue origin created by agent %s", planResponse, agentID)
	}

	var sourceID, snapshot, digest, createdByType, createdByID string
	var revision int64
	dbfx.QueryRow(t, `
		SELECT source_issue_id, source_issue_revision, source_description_snapshot,
		       source_content_sha256, created_by_type, created_by_id
		FROM project_plan WHERE id = $1`, planResponse.ID,
	).Scan(&sourceID, &revision, &snapshot, &digest, &createdByType, &createdByID)
	wantDigest := sha256.Sum256([]byte(sourceDescription))
	if sourceID != sourceIssueID || revision < 1 || snapshot != sourceDescription || digest != hex.EncodeToString(wantDigest[:]) {
		t.Fatalf("stored provenance = issue %s revision %d snapshot %q digest %s", sourceID, revision, snapshot, digest)
	}
	if createdByType != "agent" || createdByID != agentID {
		t.Fatalf("stored creator = %s/%s, want agent/%s", createdByType, createdByID, agentID)
	}

	phase := testutil.Decode[projectPlanPhaseResponse](t, testHandler.AddProjectPlanPhase,
		agentRequest(http.MethodPost, "/phases", map[string]any{
			"title": "Implementation", "position": 0,
		}, "id", projectID, "planId", planResponse.ID), http.StatusCreated)
	linkedPart := testutil.Decode[projectPlanPartResponse](t, testHandler.AddProjectPlanPart,
		agentRequest(http.MethodPost, "/parts", map[string]any{
			"title": "Linked work", "position": 0,
		}, "id", projectID, "planId", planResponse.ID, "phaseId", phase.ID), http.StatusCreated)
	zeroLinkPart := testutil.Decode[projectPlanPartResponse](t, testHandler.AddProjectPlanPart,
		agentRequest(http.MethodPost, "/parts", map[string]any{
			"title": "Awaiting decomposition", "position": 1,
		}, "id", projectID, "planId", planResponse.ID, "phaseId", phase.ID), http.StatusCreated)

	subIssueID := dbfx.Issue(t, "Orchestrated sub-issue", testutil.Cols{"project_id": projectID})
	link := testutil.Decode[projectPlanIssueLinkResponse](t, testHandler.LinkProjectPlanIssue,
		agentRequest(http.MethodPost, "/issues/"+subIssueID, map[string]any{},
			"id", projectID, "planId", planResponse.ID, "partId", linkedPart.ID, "issueId", subIssueID,
		), http.StatusCreated)
	if link.ProjectPlanPartID != linkedPart.ID || link.IssueID != subIssueID {
		t.Fatalf("link = %+v, want part %s issue %s", link, linkedPart.ID, subIssueID)
	}

	var overview projectplan.Overview
	testutil.Call(t, testHandler.GetActiveProjectPlan,
		testutil.WithURLParams(newRequest(http.MethodGet, "/plan", nil), "id", projectID),
	).Want(http.StatusOK).JSON(&overview)
	states := make(map[string]string)
	for _, gotPhase := range overview.Phases {
		for _, part := range gotPhase.Parts {
			states[part.ID] = part.CoverageState
		}
	}
	if got := states[linkedPart.ID]; got != projectplan.CoverageNotStarted {
		t.Errorf("linked part coverage = %q, want %q", got, projectplan.CoverageNotStarted)
	}
	if got := states[zeroLinkPart.ID]; got != projectplan.CoverageNoTasksYet {
		t.Errorf("zero-link part coverage = %q, want %q", got, projectplan.CoverageNoTasksYet)
	}
}

func TestProjectPlanWriteRoutesNestedNotFound(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database unavailable")
	}
	withFeatureFlag(t, testHandler, featureflags.ProjectPlans, true)
	projectID := dbfx.Project(t, "Nested plan route misses")
	planID := dbfx.Insert(t, "project_plan", testutil.Cols{
		"workspace_id": testWorkspaceID, "project_id": projectID, "version": 1,
		"kind": "prd", "origin": "manual", "title": "Nested misses",
		"created_by_type": "member", "created_by_id": testUserID,
	})
	phaseID := dbfx.Insert(t, "project_plan_phase", testutil.Cols{
		"project_plan_id": planID, "title": "Real phase", "position": 0,
	})
	partID := dbfx.Insert(t, "project_plan_part", testutil.Cols{
		"project_plan_id": planID, "project_plan_phase_id": phaseID,
		"title": "Real part", "position": 0,
	})
	missingID := uuid.NewString()

	tests := []projectPlanHandlerRoute{
		{name: "update phase", method: http.MethodPatch, handler: testHandler.UpdateProjectPlanPhase, params: []string{"planId", planID, "phaseId", missingID}, body: map[string]any{"title": "No"}},
		{name: "delete phase", method: http.MethodDelete, handler: testHandler.DeleteProjectPlanPhase, params: []string{"planId", planID, "phaseId", missingID}},
		{name: "add part", method: http.MethodPost, handler: testHandler.AddProjectPlanPart, params: []string{"planId", planID, "phaseId", missingID}, body: map[string]any{"title": "No"}},
		{name: "reorder parts", method: http.MethodPatch, handler: testHandler.ReorderProjectPlanParts, params: []string{"planId", planID, "phaseId", missingID}, body: map[string]any{"ordered_ids": []string{partID}}},
		{name: "update part", method: http.MethodPatch, handler: testHandler.UpdateProjectPlanPart, params: []string{"planId", planID, "partId", missingID}, body: map[string]any{"title": "No"}},
		{name: "delete part", method: http.MethodDelete, handler: testHandler.DeleteProjectPlanPart, params: []string{"planId", planID, "partId", missingID}},
		{name: "link issue", method: http.MethodPost, handler: testHandler.LinkProjectPlanIssue, params: []string{"planId", planID, "partId", missingID, "issueId", uuid.NewString()}},
		{name: "unlink issue", method: http.MethodDelete, handler: testHandler.UnlinkProjectPlanIssue, params: []string{"planId", planID, "partId", missingID, "issueId", uuid.NewString()}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testutil.Call(t, test.handler, projectPlanHandlerRequest(test, projectID)).Want(http.StatusNotFound)
		})
	}
}

func TestCreateProjectPlanFromIssueNotFound(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database unavailable")
	}
	withFeatureFlag(t, testHandler, featureflags.ProjectPlans, true)
	projectID := dbfx.Project(t, "Missing source issue")
	request := testutil.WithURLParams(newRequest(http.MethodPost, "/plans/from-issue", map[string]any{
		"kind": "prd", "source_issue_id": uuid.NewString(),
	}), "id", projectID)
	testutil.Call(t, testHandler.CreateProjectPlanFromIssue, request).Want(http.StatusNotFound)
}

func TestProjectPlanReorderRoutesRejectIncompleteOrder(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database unavailable")
	}
	withFeatureFlag(t, testHandler, featureflags.ProjectPlans, true)
	projectID := dbfx.Project(t, "Reorder validation")
	planID := dbfx.Insert(t, "project_plan", testutil.Cols{
		"workspace_id": testWorkspaceID, "project_id": projectID, "version": 1,
		"kind": "prd", "origin": "manual", "title": "Reorder",
		"created_by_type": "member", "created_by_id": testUserID,
	})
	phaseA := dbfx.Insert(t, "project_plan_phase", testutil.Cols{
		"project_plan_id": planID, "title": "A", "position": 0,
	})
	phaseB := dbfx.Insert(t, "project_plan_phase", testutil.Cols{
		"project_plan_id": planID, "title": "B", "position": 1,
	})
	partA := dbfx.Insert(t, "project_plan_part", testutil.Cols{
		"project_plan_id": planID, "project_plan_phase_id": phaseA, "title": "A", "position": 0,
	})
	dbfx.Insert(t, "project_plan_part", testutil.Cols{
		"project_plan_id": planID, "project_plan_phase_id": phaseA, "title": "B", "position": 1,
	})

	t.Run("phases", func(t *testing.T) {
		request := testutil.WithURLParams(newRequest(http.MethodPatch, "/phases/reorder", map[string]any{
			"ordered_ids": []string{phaseB},
		}), "id", projectID, "planId", planID)
		testutil.Call(t, testHandler.ReorderProjectPlanPhases, request).Want(http.StatusBadRequest)
	})
	t.Run("parts", func(t *testing.T) {
		request := testutil.WithURLParams(newRequest(http.MethodPatch, "/parts/reorder", map[string]any{
			"ordered_ids": []string{partA},
		}), "id", projectID, "planId", planID, "phaseId", phaseA)
		testutil.Call(t, testHandler.ReorderProjectPlanParts, request).Want(http.StatusBadRequest)
	})
}

func TestWriteProjectPlanErrorMapsDomainKinds(t *testing.T) {
	tests := []struct {
		kind projectplan.ErrorKind
		want int
	}{
		{kind: projectplan.ErrorDisabled, want: http.StatusNotFound},
		{kind: projectplan.ErrorNotFound, want: http.StatusNotFound},
		{kind: projectplan.ErrorInvalid, want: http.StatusBadRequest},
		{kind: projectplan.ErrorNotActive, want: http.StatusConflict},
		{kind: projectplan.ErrorActivePlanExists, want: http.StatusConflict},
		{kind: projectplan.ErrorVersionConflict, want: http.StatusConflict},
		{kind: projectplan.ErrorPositionConflict, want: http.StatusConflict},
		{kind: projectplan.ErrorIssueAlreadyLinked, want: http.StatusConflict},
		{kind: projectplan.ErrorUnavailable, want: http.StatusInternalServerError},
	}
	for _, test := range tests {
		t.Run(string(test.kind), func(t *testing.T) {
			request := newRequest(http.MethodPost, "/plans", nil)
			response := testutil.Call(t, func(w http.ResponseWriter, r *http.Request) {
				testHandler.writeProjectPlanError(w, r, &projectplan.Error{Kind: test.kind, Message: "mapped"})
			}, request).Want(test.want)
			if response.Map()["error"] != "mapped" {
				t.Fatalf("error body = %s", response.Text())
			}
		})
	}
}

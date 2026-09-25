package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// listActivitiesForIssue is a test helper that fetches up to 100 activity_log
// records for an issue. Uses the same query that backs the timeline endpoint.
func listActivitiesForIssue(t *testing.T, queries *db.Queries, issueID string) []db.ActivityLog {
	t.Helper()
	activities, err := queries.ListActivitiesForIssue(context.Background(), db.ListActivitiesForIssueParams{
		IssueID: util.MustParseUUID(issueID),
		Limit:   100,
	})
	if err != nil {
		t.Fatalf("ListActivitiesForIssue: %v", err)
	}
	return activities
}

func cleanupActivities(t *testing.T, issueID string) {
	t.Helper()
	testPool.Exec(context.Background(), `DELETE FROM activity_log WHERE issue_id = $1`, issueID)
}

func TestActivityIssueCreated(t *testing.T) {
	queries := db.New(testPool)
	bus := events.New()
	registerActivityListeners(bus, queries)

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupActivities(t, issueID)
		cleanupTestIssue(t, issueID)
	})

	bus.Publish(events.Event{
		Type:        protocol.EventIssueCreated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{
			"issue": handler.IssueResponse{
				ID:          issueID,
				WorkspaceID: testWorkspaceID,
				Title:       "activity test issue",
				Status:      "todo",
				Priority:    "medium",
				CreatorType: "member",
				CreatorID:   testUserID,
			},
		},
	})

	activities := listActivitiesForIssue(t, queries, issueID)
	if len(activities) != 1 {
		t.Fatalf("expected 1 activity, got %d", len(activities))
	}
	if activities[0].Action != "created" {
		t.Fatalf("expected action 'created', got %q", activities[0].Action)
	}
	if util.UUIDToString(activities[0].ActorID) != testUserID {
		t.Fatalf("expected actor_id %s, got %s", testUserID, util.UUIDToString(activities[0].ActorID))
	}
}

func TestActivityIssueCreated_AutopilotMapPayload(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	registerActivityListeners(bus, queries)

	agentID, _ := firstFixtureAgent(t)
	issueID := workspaceFixture(t).Issue(t, "autopilot-created activity", testutil.Cols{
		"creator_type": "agent",
		"creator_id":   agentID,
	})
	t.Cleanup(func() { cleanupActivities(t, issueID) })
	issue, err := queries.GetIssue(ctx, util.MustParseUUID(issueID))
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}

	var broadcasts []events.Event
	bus.Subscribe(protocol.EventActivityCreated, func(e events.Event) {
		broadcasts = append(broadcasts, e)
	})
	bus.Publish(events.Event{
		Type:        protocol.EventIssueCreated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "agent",
		ActorID:     agentID,
		Payload: map[string]any{
			// Use the same payload builder as AutopilotService.dispatchCreateIssue.
			"issue": service.IssueToMapResolved(ctx, queries, issue, "ACT"),
		},
	})

	activities := listActivitiesForIssue(t, queries, issueID)
	if len(activities) != 1 {
		t.Fatalf("expected one created activity for autopilot issue, got %d", len(activities))
	}
	activity := activities[0]
	if activity.Action != "created" || activity.ActorType.String != "agent" ||
		util.UUIDToString(activity.ActorID) != agentID || util.UUIDToString(activity.WorkspaceID) != testWorkspaceID {
		t.Fatalf("unexpected autopilot creation activity: %+v", activity)
	}
	if len(broadcasts) != 1 {
		t.Fatalf("expected one activity broadcast, got %d", len(broadcasts))
	}
	broadcast := broadcasts[0]
	if broadcast.WorkspaceID != testWorkspaceID || broadcast.ActorType != "agent" || broadcast.ActorID != agentID {
		t.Fatalf("unexpected activity broadcast attribution: %+v", broadcast)
	}
	payload, ok := broadcast.Payload.(map[string]any)
	if !ok || payload["issue_id"] != issueID {
		t.Fatalf("unexpected activity broadcast payload: %#v", broadcast.Payload)
	}
	entry, ok := payload["entry"].(map[string]any)
	if !ok || entry["id"] != util.UUIDToString(activity.ID) || entry["action"] != "created" {
		t.Fatalf("broadcast must reference the persisted creation activity: %#v", payload["entry"])
	}
}

func TestActivityIssueUpdated_StatusChanged(t *testing.T) {
	queries := db.New(testPool)
	bus := events.New()
	registerActivityListeners(bus, queries)

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupActivities(t, issueID)
		cleanupTestIssue(t, issueID)
	})

	bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{
			"issue": handler.IssueResponse{
				ID:          issueID,
				WorkspaceID: testWorkspaceID,
				Title:       "activity test issue",
				Status:      "in_progress",
				Priority:    "medium",
				CreatorType: "member",
				CreatorID:   testUserID,
			},
			"status_changed": true,
			"prev_status":    "todo",
		},
	})

	activities := listActivitiesForIssue(t, queries, issueID)
	if len(activities) != 1 {
		t.Fatalf("expected 1 activity, got %d", len(activities))
	}
	if activities[0].Action != "status_changed" {
		t.Fatalf("expected action 'status_changed', got %q", activities[0].Action)
	}

	var details map[string]string
	if err := json.Unmarshal(activities[0].Details, &details); err != nil {
		t.Fatalf("failed to unmarshal details: %v", err)
	}
	if details["from"] != "todo" {
		t.Fatalf("expected from 'todo', got %q", details["from"])
	}
	if details["to"] != "in_progress" {
		t.Fatalf("expected to 'in_progress', got %q", details["to"])
	}
}

func TestActivityIssueUpdated_AssigneeChanged(t *testing.T) {
	queries := db.New(testPool)
	bus := events.New()
	registerActivityListeners(bus, queries)

	assigneeEmail := "activity-assignee-test@multica.ai"
	assigneeID := createTestUser(t, assigneeEmail)
	t.Cleanup(func() { cleanupTestUser(t, assigneeEmail) })

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupActivities(t, issueID)
		cleanupTestIssue(t, issueID)
	})

	assigneeType := "member"
	bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{
			"issue": handler.IssueResponse{
				ID:           issueID,
				WorkspaceID:  testWorkspaceID,
				Title:        "activity test issue",
				Status:       "todo",
				Priority:     "medium",
				CreatorType:  "member",
				CreatorID:    testUserID,
				AssigneeType: &assigneeType,
				AssigneeID:   &assigneeID,
			},
			"assignee_changed":   true,
			"prev_assignee_type": (*string)(nil),
			"prev_assignee_id":   (*string)(nil),
		},
	})

	activities := listActivitiesForIssue(t, queries, issueID)
	if len(activities) != 1 {
		t.Fatalf("expected 1 activity, got %d", len(activities))
	}
	if activities[0].Action != "assignee_changed" {
		t.Fatalf("expected action 'assignee_changed', got %q", activities[0].Action)
	}

	var details map[string]string
	if err := json.Unmarshal(activities[0].Details, &details); err != nil {
		t.Fatalf("failed to unmarshal details: %v", err)
	}
	if details["to_type"] != "member" {
		t.Fatalf("expected to_type 'member', got %q", details["to_type"])
	}
	if details["to_id"] != assigneeID {
		t.Fatalf("expected to_id %q, got %q", assigneeID, details["to_id"])
	}
}

func TestActivityIssueUpdated_NoChangeFlags(t *testing.T) {
	queries := db.New(testPool)
	bus := events.New()
	registerActivityListeners(bus, queries)

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupActivities(t, issueID)
		cleanupTestIssue(t, issueID)
	})

	// Publish issue:updated with no change flags set
	bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{
			"issue": handler.IssueResponse{
				ID:          issueID,
				WorkspaceID: testWorkspaceID,
				Title:       "activity test issue",
				Status:      "todo",
				Priority:    "medium",
				CreatorType: "member",
				CreatorID:   testUserID,
			},
			"assignee_changed":    false,
			"status_changed":      false,
			"description_changed": false,
		},
	})

	activities := listActivitiesForIssue(t, queries, issueID)
	if len(activities) != 0 {
		t.Fatalf("expected 0 activities when no change flags, got %d", len(activities))
	}
}

func TestActivityIssueUpdated_TitleChanged(t *testing.T) {
	queries := db.New(testPool)
	bus := events.New()
	registerActivityListeners(bus, queries)

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupActivities(t, issueID)
		cleanupTestIssue(t, issueID)
	})

	bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{
			"issue": handler.IssueResponse{
				ID:          issueID,
				WorkspaceID: testWorkspaceID,
				Title:       "renamed issue",
				Status:      "todo",
				Priority:    "medium",
				CreatorType: "member",
				CreatorID:   testUserID,
			},
			"title_changed": true,
			"prev_title":    "activity test issue",
		},
	})

	activities := listActivitiesForIssue(t, queries, issueID)
	if len(activities) != 1 {
		t.Fatalf("expected 1 activity, got %d", len(activities))
	}
	if activities[0].Action != "title_changed" {
		t.Fatalf("expected action 'title_changed', got %q", activities[0].Action)
	}

	var details map[string]string
	if err := json.Unmarshal(activities[0].Details, &details); err != nil {
		t.Fatalf("failed to unmarshal details: %v", err)
	}
	if details["from"] != "activity test issue" {
		t.Fatalf("expected from 'activity test issue', got %q", details["from"])
	}
	if details["to"] != "renamed issue" {
		t.Fatalf("expected to 'renamed issue', got %q", details["to"])
	}
}

func TestActivityTaskCompleted(t *testing.T) {
	queries := db.New(testPool)
	bus := events.New()
	registerActivityListeners(bus, queries)

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupActivities(t, issueID)
		cleanupTestIssue(t, issueID)
	})

	agentID := testUserID // reuse as a stand-in for agent ID

	bus.Publish(events.Event{
		Type:        protocol.EventTaskCompleted,
		WorkspaceID: testWorkspaceID,
		ActorType:   "system",
		ActorID:     "",
		Payload: map[string]any{
			"task_id":  "00000000-0000-0000-0000-000000000001",
			"agent_id": agentID,
			"issue_id": issueID,
			"status":   "completed",
		},
	})

	activities := listActivitiesForIssue(t, queries, issueID)
	if len(activities) != 1 {
		t.Fatalf("expected 1 activity, got %d", len(activities))
	}
	if activities[0].Action != "task_completed" {
		t.Fatalf("expected action 'task_completed', got %q", activities[0].Action)
	}
	if util.UUIDToString(activities[0].ActorID) != agentID {
		t.Fatalf("expected actor_id %s, got %s", agentID, util.UUIDToString(activities[0].ActorID))
	}
}

func TestActivityTaskFailed(t *testing.T) {
	queries := db.New(testPool)
	bus := events.New()
	registerActivityListeners(bus, queries)

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupActivities(t, issueID)
		cleanupTestIssue(t, issueID)
	})

	agentID := testUserID

	bus.Publish(events.Event{
		Type:        protocol.EventTaskFailed,
		WorkspaceID: testWorkspaceID,
		ActorType:   "system",
		ActorID:     "",
		Payload: map[string]any{
			"task_id":  "00000000-0000-0000-0000-000000000002",
			"agent_id": agentID,
			"issue_id": issueID,
			"status":   "failed",
		},
	})

	activities := listActivitiesForIssue(t, queries, issueID)
	if len(activities) != 1 {
		t.Fatalf("expected 1 activity, got %d", len(activities))
	}
	if activities[0].Action != "task_failed" {
		t.Fatalf("expected action 'task_failed', got %q", activities[0].Action)
	}
}

// ---------------------------------------------------------------------------
// Duplicate marks (MUL-7349) log on both issues. Every publisher of a mark
// change carries both ends of it, the way UpdateIssue does, and the mark row
// stands in for the status row the same write would otherwise log.
// ---------------------------------------------------------------------------

func duplicateMarkEvent(issueID, identifier, status string, prev, next *string, extra map[string]any) events.Event {
	payload := map[string]any{
		"issue": handler.IssueResponse{
			ID:          issueID,
			WorkspaceID: testWorkspaceID,
			Identifier:  identifier,
			Title:       "duplicate activity test issue",
			Status:      status,
			Priority:    "medium",
			CreatorType: "member",
			CreatorID:   testUserID,
		},
		"duplicate_of_issue_id":      next,
		"prev_duplicate_of_issue_id": prev,
	}
	for k, v := range extra {
		payload[k] = v
	}
	return events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload:     payload,
	}
}

// activityDetails returns the details of the one activity with this action on
// the issue, failing when there are none or several.
func activityDetails(t *testing.T, queries *db.Queries, issueID, action string) map[string]string {
	t.Helper()
	var found []db.ActivityLog
	for _, a := range listActivitiesForIssue(t, queries, issueID) {
		if a.Action == action {
			found = append(found, a)
		}
	}
	if len(found) != 1 {
		t.Fatalf("expected exactly 1 %q activity on %s, got %d", action, issueID, len(found))
	}
	var details map[string]string
	if err := json.Unmarshal(found[0].Details, &details); err != nil {
		t.Fatalf("failed to unmarshal details: %v", err)
	}
	return details
}

func activityActions(t *testing.T, queries *db.Queries, issueID string) []string {
	t.Helper()
	var actions []string
	for _, a := range listActivitiesForIssue(t, queries, issueID) {
		actions = append(actions, a.Action)
	}
	return actions
}

func testIssueIdentifier(t *testing.T, queries *db.Queries, issueID string) string {
	t.Helper()
	ctx := context.Background()
	issue, err := queries.GetIssue(ctx, util.MustParseUUID(issueID))
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	ws, err := queries.GetWorkspace(ctx, issue.WorkspaceID)
	if err != nil {
		t.Fatalf("GetWorkspace: %v", err)
	}
	return service.IssueIdentifier(ws.IssuePrefix, issue.Number)
}

func newDuplicateTestIssues(t *testing.T, queries *db.Queries) (bus *events.Bus, duplicateID, originalID string) {
	t.Helper()
	bus = events.New()
	registerActivityListeners(bus, queries)
	duplicateID = createTestIssue(t, testWorkspaceID, testUserID)
	originalID = createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupActivities(t, duplicateID)
		cleanupActivities(t, originalID)
		cleanupTestIssue(t, duplicateID)
		cleanupTestIssue(t, originalID)
	})
	return bus, duplicateID, originalID
}

// Marking is a status write to cancelled; the mark row replaces the status
// row rather than sitting beside it.
func TestActivityIssueUpdated_DuplicateMarked(t *testing.T) {
	queries := db.New(testPool)
	bus, duplicateID, originalID := newDuplicateTestIssues(t, queries)

	bus.Publish(duplicateMarkEvent(duplicateID, "MUL-7624", "cancelled", nil, &originalID, map[string]any{
		"status_changed": true,
		"prev_status":    "todo",
	}))

	marked := activityDetails(t, queries, duplicateID, "duplicate_marked")
	if marked["original_id"] != originalID {
		t.Fatalf("original_id = %q, want %q", marked["original_id"], originalID)
	}
	if want := testIssueIdentifier(t, queries, originalID); marked["original_identifier"] != want {
		t.Fatalf("original_identifier = %q, want %q", marked["original_identifier"], want)
	}
	added := activityDetails(t, queries, originalID, "duplicate_added")
	if added["duplicate_id"] != duplicateID || added["duplicate_identifier"] != "MUL-7624" {
		t.Fatalf("duplicate_added details = %v", added)
	}
	if got := activityActions(t, queries, duplicateID); len(got) != 1 {
		t.Fatalf("expected only the duplicate_marked row on the duplicate, got %v", got)
	}
}

// Reopening removes the mark; the unmarked row carries the new status the
// suppressed status row would have named.
func TestActivityIssueUpdated_DuplicateUnmarked(t *testing.T) {
	queries := db.New(testPool)
	bus, duplicateID, originalID := newDuplicateTestIssues(t, queries)

	bus.Publish(duplicateMarkEvent(duplicateID, "MUL-7624", "todo", &originalID, nil, map[string]any{
		"status_changed": true,
		"prev_status":    "cancelled",
	}))

	unmarked := activityDetails(t, queries, duplicateID, "duplicate_unmarked")
	if unmarked["original_id"] != originalID || unmarked["reason"] != "" || unmarked["to"] != "todo" {
		t.Fatalf("duplicate_unmarked details = %v", unmarked)
	}
	removed := activityDetails(t, queries, originalID, "duplicate_removed")
	if removed["duplicate_id"] != duplicateID {
		t.Fatalf("duplicate_removed details = %v", removed)
	}
	if got := activityActions(t, queries, duplicateID); len(got) != 1 {
		t.Fatalf("expected only the duplicate_unmarked row on the duplicate, got %v", got)
	}
}

// Moving a mark from one original to another reads as leaving the first and
// joining the second, on all three issues. The status did not move.
func TestActivityIssueUpdated_DuplicateRepointed(t *testing.T) {
	queries := db.New(testPool)
	bus, duplicateID, firstID := newDuplicateTestIssues(t, queries)
	secondID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupActivities(t, secondID)
		cleanupTestIssue(t, secondID)
	})

	bus.Publish(duplicateMarkEvent(duplicateID, "MUL-7624", "cancelled", &firstID, &secondID, nil))

	unmarked := activityDetails(t, queries, duplicateID, "duplicate_unmarked")
	if unmarked["original_id"] != firstID || unmarked["to"] != "" {
		t.Fatalf("duplicate_unmarked details = %v", unmarked)
	}
	if got := activityDetails(t, queries, duplicateID, "duplicate_marked"); got["original_id"] != secondID {
		t.Fatalf("duplicate_marked original_id = %q, want %q", got["original_id"], secondID)
	}
	activityDetails(t, queries, firstID, "duplicate_removed")
	activityDetails(t, queries, secondID, "duplicate_added")
}

// Deleting the original clears the mark. The original's log goes with it, so
// only the duplicate gets a row, naming the identifier the delete path passed.
func TestActivityIssueUpdated_DuplicateOriginalDeleted(t *testing.T) {
	queries := db.New(testPool)
	bus, duplicateID, _ := newDuplicateTestIssues(t, queries)
	gone := "00000000-0000-4000-8000-00000000dead"

	bus.Publish(duplicateMarkEvent(duplicateID, "MUL-7624", "cancelled", &gone, nil, map[string]any{
		"prev_duplicate_of_identifier": "MUL-7349",
	}))

	unmarked := activityDetails(t, queries, duplicateID, "duplicate_unmarked")
	if unmarked["original_identifier"] != "MUL-7349" || unmarked["reason"] != "original_deleted" || unmarked["to"] != "" {
		t.Fatalf("duplicate_unmarked details = %v", unmarked)
	}
}

func TestActivityIssueUpdated_DuplicateUnchanged(t *testing.T) {
	queries := db.New(testPool)
	bus, duplicateID, originalID := newDuplicateTestIssues(t, queries)

	// Re-marking the same original is a no-op on the server and must not log.
	bus.Publish(duplicateMarkEvent(duplicateID, "MUL-7624", "cancelled", &originalID, &originalID, nil))

	if got := activityActions(t, queries, duplicateID); len(got) != 0 {
		t.Fatalf("expected no activities on the duplicate, got %v", got)
	}
	if got := activityActions(t, queries, originalID); len(got) != 0 {
		t.Fatalf("expected no activities on the original, got %v", got)
	}
}

// A plain cancel with no mark still logs its status row: only a mark change
// replaces it.
func TestActivityIssueUpdated_PlainCancelKeepsStatusRow(t *testing.T) {
	queries := db.New(testPool)
	bus, issueID, _ := newDuplicateTestIssues(t, queries)

	bus.Publish(duplicateMarkEvent(issueID, "MUL-7624", "cancelled", nil, nil, map[string]any{
		"status_changed": true,
		"prev_status":    "todo",
	}))

	if got := activityActions(t, queries, issueID); len(got) != 1 || got[0] != "status_changed" {
		t.Fatalf("expected only status_changed, got %v", got)
	}
}

// The real write path: marking and reopening through UpdateIssue produce one
// duplicate row per side and no generic status row.
func TestActivityDuplicateMarkThroughUpdateIssue(t *testing.T) {
	queries := db.New(testPool)
	duplicateID := createTestIssue(t, testWorkspaceID, testUserID)
	originalID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupActivities(t, duplicateID)
		cleanupActivities(t, originalID)
		cleanupTestIssue(t, duplicateID)
		cleanupTestIssue(t, originalID)
	})

	resp := authRequest(t, "PUT", "/api/issues/"+duplicateID, map[string]any{
		"duplicate_of_issue_id": originalID,
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("mark: expected 200, got %d", resp.StatusCode)
	}
	if got := activityActions(t, queries, duplicateID); len(got) != 1 || got[0] != "duplicate_marked" {
		t.Fatalf("after mark, duplicate activities = %v, want [duplicate_marked]", got)
	}
	if got := activityActions(t, queries, originalID); len(got) != 1 || got[0] != "duplicate_added" {
		t.Fatalf("after mark, original activities = %v, want [duplicate_added]", got)
	}

	resp = authRequest(t, "PUT", "/api/issues/"+duplicateID, map[string]any{"status": "todo"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reopen: expected 200, got %d", resp.StatusCode)
	}
	if got := activityActions(t, queries, duplicateID); len(got) != 2 || got[1] != "duplicate_unmarked" {
		t.Fatalf("after reopen, duplicate activities = %v, want [duplicate_marked duplicate_unmarked]", got)
	}
	if got := activityDetails(t, queries, duplicateID, "duplicate_unmarked"); got["to"] != "todo" {
		t.Fatalf("duplicate_unmarked to = %q, want todo", got["to"])
	}
	if got := activityActions(t, queries, originalID); len(got) != 2 || got[1] != "duplicate_removed" {
		t.Fatalf("after reopen, original activities = %v, want [duplicate_added duplicate_removed]", got)
	}
}

// A pointer an older server left behind is not a mark (MUL-7349). Servers
// from before the column existed reopened duplicates and deleted originals
// without touching it; the writes that later clear it must not log a mark
// being removed, and a real status move keeps its status row.
func TestActivityLeftoverDuplicatePointer(t *testing.T) {
	queries := db.New(testPool)
	send := func(t *testing.T, method, issueID string, body map[string]any) {
		t.Helper()
		resp := authRequest(t, method, "/api/issues/"+issueID, body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
			t.Fatalf("%s %s: status %d", method, issueID, resp.StatusCode)
		}
	}
	raw := func(t *testing.T, sql, issueID string) {
		t.Helper()
		if _, err := testPool.Exec(context.Background(), sql, issueID); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	marked := func(t *testing.T) (duplicateID, originalID string) {
		t.Helper()
		duplicateID = createTestIssue(t, testWorkspaceID, testUserID)
		originalID = createTestIssue(t, testWorkspaceID, testUserID)
		t.Cleanup(func() {
			cleanupActivities(t, duplicateID)
			cleanupActivities(t, originalID)
			cleanupTestIssue(t, duplicateID)
			cleanupTestIssue(t, originalID)
		})
		send(t, "PUT", duplicateID, map[string]any{"duplicate_of_issue_id": originalID})
		return duplicateID, originalID
	}
	const reopenedByOldServer = `UPDATE issue SET status = 'todo' WHERE id = $1`
	const deletedByOldServer = `DELETE FROM issue WHERE id = $1`
	wantActions := func(t *testing.T, issueID string, want ...string) {
		t.Helper()
		if got := activityActions(t, queries, issueID); strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("activities on %s = %v, want %v", issueID, got, want)
		}
	}
	wantStatusMove := func(t *testing.T, issueID, from, to string) {
		t.Helper()
		if got := activityDetails(t, queries, issueID, "status_changed"); got["from"] != from || got["to"] != to {
			t.Fatalf("status_changed = %v, want %s -> %s", got, from, to)
		}
	}

	t.Run("reopened, then moved", func(t *testing.T) {
		duplicateID, originalID := marked(t)
		raw(t, reopenedByOldServer, duplicateID)

		send(t, "PUT", duplicateID, map[string]any{"status": "in_progress"})

		wantActions(t, duplicateID, "duplicate_marked", "status_changed")
		wantStatusMove(t, duplicateID, "todo", "in_progress")
		wantActions(t, originalID, "duplicate_added")
	})

	t.Run("reopened, then edited", func(t *testing.T) {
		duplicateID, originalID := marked(t)
		raw(t, reopenedByOldServer, duplicateID)

		send(t, "PUT", duplicateID, map[string]any{"title": "renamed after a rollback"})

		wantActions(t, duplicateID, "duplicate_marked", "title_changed")
		wantActions(t, originalID, "duplicate_added")
	})

	t.Run("reopened, then its original deleted", func(t *testing.T) {
		duplicateID, originalID := marked(t)
		raw(t, reopenedByOldServer, duplicateID)

		send(t, "DELETE", originalID, nil)

		wantActions(t, duplicateID, "duplicate_marked")
	})

	// The mark was live but its original is gone, so there is no duplicate
	// row to name the move; the status row records it instead.
	t.Run("original deleted, then reopened", func(t *testing.T) {
		duplicateID, originalID := marked(t)
		raw(t, deletedByOldServer, originalID)

		send(t, "PUT", duplicateID, map[string]any{"status": "todo"})

		wantActions(t, duplicateID, "duplicate_marked", "status_changed")
		wantStatusMove(t, duplicateID, "cancelled", "todo")
	})
}

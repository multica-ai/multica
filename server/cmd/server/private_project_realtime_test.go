package main

import (
	"context"
	"errors"
	"sort"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	ppWorkspace     = "00000000-0000-0000-0000-0000000000a1"
	ppPrivate       = "00000000-0000-0000-0000-0000000000b1"
	ppPublic        = "00000000-0000-0000-0000-0000000000b2"
	ppMember        = "00000000-0000-0000-0000-0000000000c1"
	ppAdmin         = "00000000-0000-0000-0000-0000000000c2"
	ppSecretIssue   = "00000000-0000-0000-0000-0000000000d1"
	ppPublicIssue   = "00000000-0000-0000-0000-0000000000d2"
	ppSecretTask    = "00000000-0000-0000-0000-0000000000e1"
	ppSecretComment = "00000000-0000-0000-0000-0000000000f1"

	// A second private project with its own member, and issue-less runs.
	ppPrivate2         = "00000000-0000-0000-0000-0000000000b3"
	ppOther            = "00000000-0000-0000-0000-0000000000c3"
	ppSecretIssue2     = "00000000-0000-0000-0000-0000000000d3"
	ppAutopilotRun     = "00000000-0000-0000-0000-0000000000a3"
	ppAutopilot        = "00000000-0000-0000-0000-0000000000a4"
	ppAutopilotTask    = "00000000-0000-0000-0000-0000000000e2"
	ppQuickCreateTask  = "00000000-0000-0000-0000-0000000000e3"
	ppCrossProjectTask = "00000000-0000-0000-0000-0000000000e4"
)

type fakeAudienceQuerier struct {
	fail bool
}

func ppUUID(s string) pgtype.UUID {
	id, err := util.ParseUUID(s)
	if err != nil {
		panic(err)
	}
	return id
}

func (f fakeAudienceQuerier) ListPrivateProjectAudience(context.Context, pgtype.UUID) ([]db.ListPrivateProjectAudienceRow, error) {
	if f.fail {
		return nil, errors.New("db down")
	}
	return []db.ListPrivateProjectAudienceRow{
		{ProjectID: ppUUID(ppPrivate), UserID: ppUUID(ppMember)},
		{ProjectID: ppUUID(ppPrivate2), UserID: ppUUID(ppOther)},
	}, nil
}

func (fakeAudienceQuerier) ListWorkspaceManagerUserIDs(context.Context, pgtype.UUID) ([]pgtype.UUID, error) {
	return []pgtype.UUID{ppUUID(ppAdmin)}, nil
}

func (fakeAudienceQuerier) GetIssueProjectID(_ context.Context, id pgtype.UUID) (pgtype.UUID, error) {
	switch util.UUIDToString(id) {
	case ppSecretIssue:
		return ppUUID(ppPrivate), nil
	case ppPublicIssue:
		return ppUUID(ppPublic), nil
	case ppSecretIssue2:
		return ppUUID(ppPrivate2), nil
	}
	return pgtype.UUID{}, pgx.ErrNoRows
}

func (fakeAudienceQuerier) GetCommentIssueProjectID(_ context.Context, id pgtype.UUID) (pgtype.UUID, error) {
	if util.UUIDToString(id) == ppSecretComment {
		return ppUUID(ppPrivate), nil
	}
	return pgtype.UUID{}, pgx.ErrNoRows
}

func (fakeAudienceQuerier) GetAgentTask(_ context.Context, id pgtype.UUID) (db.AgentTaskQueue, error) {
	switch util.UUIDToString(id) {
	case ppSecretTask:
		return db.AgentTaskQueue{ID: id, IssueID: ppUUID(ppSecretIssue)}, nil
	case ppAutopilotTask:
		return db.AgentTaskQueue{ID: id, AutopilotRunID: ppUUID(ppAutopilotRun)}, nil
	case ppQuickCreateTask:
		return db.AgentTaskQueue{ID: id, Context: []byte(`{"type":"quick_create","project_id":"` + ppPrivate + `"}`)}, nil
	case ppCrossProjectTask:
		return db.AgentTaskQueue{ID: id, Context: []byte(`{"type":"quick_create","project_id":"` + ppPrivate + `","parent_issue_id":"` + ppSecretIssue2 + `"}`)}, nil
	}
	return db.AgentTaskQueue{}, pgx.ErrNoRows
}

func (fakeAudienceQuerier) GetAutopilotRun(_ context.Context, id pgtype.UUID) (db.AutopilotRun, error) {
	if util.UUIDToString(id) == ppAutopilotRun {
		return db.AutopilotRun{ID: id, AutopilotID: ppUUID(ppAutopilot)}, nil
	}
	return db.AutopilotRun{}, pgx.ErrNoRows
}

func (fakeAudienceQuerier) GetAutopilot(_ context.Context, id pgtype.UUID) (db.Autopilot, error) {
	if util.UUIDToString(id) == ppAutopilot {
		return db.Autopilot{ID: id, ProjectID: ppUUID(ppPrivate)}, nil
	}
	return db.Autopilot{}, pgx.ErrNoRows
}

func privateAudienceScopes(fb *fakeBroadcaster) []string {
	fb.mu.Lock()
	defer fb.mu.Unlock()
	var out []string
	for _, c := range fb.scopeCalls {
		if c.scopeType == realtime.ScopeWorkspaceMember {
			out = append(out, c.scopeID)
		}
	}
	sort.Strings(out)
	return out
}

func TestRegisterListeners_PrivateProjectEventsReachOnlyItsAudience(t *testing.T) {
	want := []string{
		realtime.WorkspaceMemberScopeID(ppWorkspace, ppMember),
		realtime.WorkspaceMemberScopeID(ppWorkspace, ppAdmin),
	}
	sort.Strings(want)
	private := []events.Event{
		{Type: protocol.EventIssueUpdated, Payload: map[string]any{"issue": map[string]any{"id": ppSecretIssue, "project_id": ppPrivate}}},
		{Type: protocol.EventIssueUpdated, Payload: map[string]any{"issue_id": ppSecretIssue}},
		{Type: protocol.EventCommentCreated, Payload: map[string]any{"comment": map[string]any{"id": ppSecretComment, "issue_id": ppSecretIssue}}},
		{Type: protocol.EventProjectUpdated, Payload: map[string]any{"project": map[string]any{"id": ppPrivate}}},
		{Type: protocol.EventProjectMembersChanged, Payload: map[string]any{"project_id": ppPrivate}},
		{Type: protocol.EventTaskMessage, TaskID: ppSecretTask, Payload: map[string]any{"task_id": ppSecretTask}},
		// Issue-less runs read the project too: a run-only autopilot and a
		// quick-create into it.
		{Type: protocol.EventTaskMessage, TaskID: ppAutopilotTask, Payload: map[string]any{"task_id": ppAutopilotTask}},
		{Type: protocol.EventTaskProgress, TaskID: ppAutopilotTask, Payload: map[string]any{"task_id": ppAutopilotTask}},
		{Type: protocol.EventTaskMessage, TaskID: ppQuickCreateTask, Payload: map[string]any{"task_id": ppQuickCreateTask}},
	}
	for _, e := range private {
		bus := events.New()
		fb := &fakeBroadcaster{}
		registerListeners(bus, fb, newPrivateProjectAudience(fakeAudienceQuerier{}))
		e.WorkspaceID = ppWorkspace
		bus.Publish(e)
		if len(fb.workspaceCalls) != 0 {
			t.Errorf("%s %v: broadcast to the whole workspace", e.Type, e.Payload)
		}
		if got := privateAudienceScopes(fb); len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
			t.Errorf("%s %v: delivered to %v, want %v", e.Type, e.Payload, got, want)
		}
	}

	public := []events.Event{
		{Type: protocol.EventIssueUpdated, Payload: map[string]any{"issue": map[string]any{"id": ppPublicIssue, "project_id": nil}}},
		{Type: protocol.EventIssueUpdated, Payload: map[string]any{"issue_id": ppPublicIssue}},
		// A deleted issue cannot be resolved: keep fanout, never go silent.
		{Type: protocol.EventIssueDeleted, Payload: map[string]any{"issue_id": "00000000-0000-0000-0000-00000000dead"}},
		{Type: protocol.EventAgentStatus, Payload: map[string]any{"agent_id": "a"}},
	}
	for _, e := range public {
		bus := events.New()
		fb := &fakeBroadcaster{}
		registerListeners(bus, fb, newPrivateProjectAudience(fakeAudienceQuerier{}))
		e.WorkspaceID = ppWorkspace
		bus.Publish(e)
		if len(fb.workspaceCalls) != 1 {
			t.Errorf("%s %v: workspace broadcasts = %d, want 1", e.Type, e.Payload, len(fb.workspaceCalls))
		}
	}
}

func TestRegisterListeners_PrivateProjectAudienceFailureDropsEvent(t *testing.T) {
	bus := events.New()
	fb := &fakeBroadcaster{}
	registerListeners(bus, fb, newPrivateProjectAudience(fakeAudienceQuerier{fail: true}))
	bus.Publish(events.Event{Type: protocol.EventIssueUpdated, WorkspaceID: ppWorkspace, Payload: map[string]any{"issue_id": ppSecretIssue}})
	if len(fb.workspaceCalls) != 0 || len(privateAudienceScopes(fb)) != 0 {
		t.Fatalf("an unresolved audience must drop the event, got workspace=%d scoped=%v", len(fb.workspaceCalls), privateAudienceScopes(fb))
	}
}

func TestRegisterListeners_PrivateTaskLifecycleKeepsWorkspaceFanout(t *testing.T) {
	for _, eventType := range []string{
		protocol.EventTaskQueued, protocol.EventTaskDispatch, protocol.EventTaskRunning,
		protocol.EventTaskCompleted, protocol.EventTaskFailed, protocol.EventTaskCancelled,
	} {
		bus := events.New()
		fb := &fakeBroadcaster{}
		registerListeners(bus, fb, newPrivateProjectAudience(fakeAudienceQuerier{}))
		bus.Publish(events.Event{Type: eventType, WorkspaceID: ppWorkspace, TaskID: ppSecretTask,
			Payload: map[string]any{"task_id": ppSecretTask, "issue_id": ppSecretIssue, "agent_id": "agent", "status": "x"}})
		if len(fb.workspaceCalls) != 1 || len(privateAudienceScopes(fb)) != 0 {
			t.Errorf("%s: workspace=%d scoped=%v; lifecycle events must reach every member", eventType, len(fb.workspaceCalls), privateAudienceScopes(fb))
		}
	}
	// Run content stays with the project audience.
	bus := events.New()
	fb := &fakeBroadcaster{}
	registerListeners(bus, fb, newPrivateProjectAudience(fakeAudienceQuerier{}))
	bus.Publish(events.Event{Type: protocol.EventTaskProgress, WorkspaceID: ppWorkspace, TaskID: ppSecretTask,
		Payload: protocol.TaskProgressPayload{TaskID: ppSecretTask, Summary: "secret step"}})
	if len(fb.workspaceCalls) != 0 || len(privateAudienceScopes(fb)) != 2 {
		t.Errorf("task:progress: workspace=%d scoped=%v; want only the project audience", len(fb.workspaceCalls), privateAudienceScopes(fb))
	}
}

func TestRegisterListeners_RunAcrossPrivateProjectsReachesOnlyTheirCommonAudience(t *testing.T) {
	bus := events.New()
	fb := &fakeBroadcaster{}
	registerListeners(bus, fb, newPrivateProjectAudience(fakeAudienceQuerier{}))
	bus.Publish(events.Event{Type: protocol.EventTaskMessage, WorkspaceID: ppWorkspace, TaskID: ppCrossProjectTask,
		Payload: map[string]any{"task_id": ppCrossProjectTask}})
	want := []string{realtime.WorkspaceMemberScopeID(ppWorkspace, ppAdmin)}
	if got := privateAudienceScopes(fb); len(fb.workspaceCalls) != 0 || len(got) != 1 || got[0] != want[0] {
		t.Fatalf("workspace=%d scoped=%v, want only %v: a member of one project must not read a run that also reads the other", len(fb.workspaceCalls), got, want)
	}
}

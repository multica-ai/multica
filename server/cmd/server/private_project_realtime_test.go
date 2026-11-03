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
	return []db.ListPrivateProjectAudienceRow{{ProjectID: ppUUID(ppPrivate), UserID: ppUUID(ppMember)}}, nil
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
	if util.UUIDToString(id) == ppSecretTask {
		return db.AgentTaskQueue{ID: id, IssueID: ppUUID(ppSecretIssue)}, nil
	}
	return db.AgentTaskQueue{}, pgx.ErrNoRows
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

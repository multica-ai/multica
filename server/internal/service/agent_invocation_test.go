package service

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type invocationQueriesStub struct {
	targets    []db.AgentInvocationTarget
	targetsErr error
	members    map[string]bool
	memberErr  error
}

func (s *invocationQueriesStub) ListAgentInvocationTargets(context.Context, pgtype.UUID) ([]db.AgentInvocationTarget, error) {
	if s.targetsErr != nil {
		return nil, s.targetsErr
	}
	return s.targets, nil
}

func (s *invocationQueriesStub) GetMemberByUserAndWorkspace(_ context.Context, arg db.GetMemberByUserAndWorkspaceParams) (db.Member, error) {
	if s.memberErr != nil {
		return db.Member{}, s.memberErr
	}
	key := util.UUIDToString(arg.UserID) + "/" + util.UUIDToString(arg.WorkspaceID)
	if s.members[key] {
		return db.Member{UserID: arg.UserID, WorkspaceID: arg.WorkspaceID}, nil
	}
	return db.Member{}, pgx.ErrNoRows
}

func TestCanInvokeAgentPrivateIsOwnerOnlyAcrossDispatchTypes(t *testing.T) {
	t.Parallel()
	owner := "11111111-1111-1111-1111-111111111111"
	other := "22222222-2222-2222-2222-222222222222"
	workspace := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	agent := db.Agent{
		ID:             util.MustParseUUID("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"),
		OwnerID:        util.MustParseUUID(owner),
		WorkspaceID:    util.MustParseUUID(workspace),
		PermissionMode: "private",
	}
	queries := &invocationQueriesStub{}

	if !CanInvokeAgent(t.Context(), queries, agent, "member", owner, owner, workspace) {
		t.Fatal("owner must be able to invoke their private agent directly")
	}
	if !CanInvokeAgent(t.Context(), queries, agent, "agent", other, owner, workspace) {
		t.Fatal("agent dispatch attributed to the owner must be admitted")
	}
	if CanInvokeAgent(t.Context(), queries, agent, "member", other, other, workspace) {
		t.Fatal("another member must not invoke a private agent")
	}
	if CanInvokeAgent(t.Context(), queries, agent, "agent", util.UUIDToString(agent.ID), other, workspace) {
		t.Fatal("cross-member agent dispatch must not invoke a private agent")
	}
	if CanInvokeAgent(t.Context(), queries, agent, "system", "", "", workspace) {
		t.Fatal("unattributed system dispatch must not invoke a private agent")
	}
}

func TestCanInvokeAgentPublicTargetsPreserveMemberAndWorkspaceBoundaries(t *testing.T) {
	t.Parallel()
	member := "33333333-3333-3333-3333-333333333333"
	foreign := "44444444-4444-4444-4444-444444444444"
	workspace := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	agent := db.Agent{
		ID:             util.MustParseUUID("cccccccc-cccc-cccc-cccc-cccccccccccc"),
		OwnerID:        util.MustParseUUID("11111111-1111-1111-1111-111111111111"),
		WorkspaceID:    util.MustParseUUID(workspace),
		PermissionMode: "public_to",
	}

	t.Run("member target requires matching human", func(t *testing.T) {
		queries := &invocationQueriesStub{targets: []db.AgentInvocationTarget{{
			AgentID:    agent.ID,
			TargetType: "member",
			TargetID:   util.MustParseUUID(member),
		}}}
		if !CanInvokeAgent(t.Context(), queries, agent, "agent", "dddddddd-dddd-dddd-dddd-dddddddddddd", member, workspace) {
			t.Fatal("agent dispatch with the targeted human originator must pass")
		}
		if CanInvokeAgent(t.Context(), queries, agent, "agent", "dddddddd-dddd-dddd-dddd-dddddddddddd", foreign, workspace) {
			t.Fatal("agent dispatch from another member must fail")
		}
		if CanInvokeAgent(t.Context(), queries, agent, "system", "", "", workspace) {
			t.Fatal("unattributed system dispatch must not match a member target")
		}
	})

	t.Run("workspace target requires membership for human", func(t *testing.T) {
		queries := &invocationQueriesStub{
			targets: []db.AgentInvocationTarget{{
				AgentID:    agent.ID,
				TargetType: "workspace",
				TargetID:   agent.WorkspaceID,
			}},
			members: map[string]bool{
				member + "/" + workspace: true,
			},
		}
		if !CanInvokeAgent(t.Context(), queries, agent, "member", member, member, workspace) {
			t.Fatal("workspace member must match a workspace target")
		}
		if CanInvokeAgent(t.Context(), queries, agent, "member", foreign, foreign, workspace) {
			t.Fatal("foreign member must not match a workspace target")
		}
		if !CanInvokeAgent(t.Context(), queries, agent, "system", "", "", workspace) {
			t.Fatal("workspace-scoped system automation must retain the approved workspace exception")
		}
	})
}

func TestCanInvokeAgentQueryFailuresFailClosed(t *testing.T) {
	t.Parallel()
	agent := db.Agent{
		ID:             util.MustParseUUID("cccccccc-cccc-cccc-cccc-cccccccccccc"),
		OwnerID:        util.MustParseUUID("11111111-1111-1111-1111-111111111111"),
		WorkspaceID:    util.MustParseUUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
		PermissionMode: "public_to",
	}
	if CanInvokeAgent(t.Context(), &invocationQueriesStub{targetsErr: errors.New("db unavailable")}, agent,
		"member", "33333333-3333-3333-3333-333333333333", "", "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa") {
		t.Fatal("target lookup failure must deny invocation")
	}
	queries := &invocationQueriesStub{
		targets: []db.AgentInvocationTarget{{
			AgentID:    agent.ID,
			TargetType: "workspace",
			TargetID:   agent.WorkspaceID,
		}},
		memberErr: errors.New("db unavailable"),
	}
	if CanInvokeAgent(t.Context(), queries, agent,
		"member", "33333333-3333-3333-3333-333333333333", "", "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa") {
		t.Fatal("workspace membership lookup failure must deny a human invocation")
	}
}

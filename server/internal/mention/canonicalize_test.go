package mention

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// nameResolverMock implements NameResolver for testing. Entries are keyed by
// the canonical UUID string (uuidToString output) of the entity.
type nameResolverMock struct {
	workspaceID pgtype.UUID
	agents      map[string]string // uuid string → agent.Name
	squads      map[string]string // uuid string → squad.Name
	users       map[string]string // uuid string → user.Name
}

func (m *nameResolverMock) GetAgentInWorkspace(_ context.Context, arg db.GetAgentInWorkspaceParams) (db.Agent, error) {
	if uuidToString(arg.WorkspaceID) != uuidToString(m.workspaceID) {
		return db.Agent{}, fmt.Errorf("wrong workspace")
	}
	name, ok := m.agents[uuidToString(arg.ID)]
	if !ok {
		return db.Agent{}, fmt.Errorf("agent not found")
	}
	return db.Agent{ID: arg.ID, WorkspaceID: arg.WorkspaceID, Name: name}, nil
}

func (m *nameResolverMock) GetSquadInWorkspace(_ context.Context, arg db.GetSquadInWorkspaceParams) (db.Squad, error) {
	if uuidToString(arg.WorkspaceID) != uuidToString(m.workspaceID) {
		return db.Squad{}, fmt.Errorf("wrong workspace")
	}
	name, ok := m.squads[uuidToString(arg.ID)]
	if !ok {
		return db.Squad{}, fmt.Errorf("squad not found")
	}
	return db.Squad{ID: arg.ID, WorkspaceID: arg.WorkspaceID, Name: name}, nil
}

func (m *nameResolverMock) GetUser(_ context.Context, id pgtype.UUID) (db.User, error) {
	name, ok := m.users[uuidToString(id)]
	if !ok {
		return db.User{}, fmt.Errorf("user not found")
	}
	return db.User{ID: id, Name: name}, nil
}

func TestCanonicalizeMentions(t *testing.T) {
	ctx := context.Background()
	ws := makeUUID("ws1")

	agentRealID := makeUUID("agent-real")
	agentRealUUID := uuidToString(agentRealID)
	agentBracketID := makeUUID("agent-brkts")
	agentBracketUUID := uuidToString(agentBracketID)
	agentMissingID := makeUUID("agent-gone-")
	agentMissingUUID := uuidToString(agentMissingID)

	squadID := makeUUID("squad-real-")
	squadUUID := uuidToString(squadID)

	userID := makeUUID("user-real--")
	userUUID := uuidToString(userID)

	resolver := &nameResolverMock{
		workspaceID: ws,
		agents: map[string]string{
			agentRealUUID:    "RealAgent",
			agentBracketUUID: "David[TF]",
		},
		squads: map[string]string{
			squadUUID: "RealSquad",
		},
		users: map[string]string{
			userUUID: "Alice",
		},
	}

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "agent mention with matching label is unchanged",
			input: "[@RealAgent](mention://agent/" + agentRealUUID + ")",
			want:  "[@RealAgent](mention://agent/" + agentRealUUID + ")",
		},
		{
			name:  "agent mention with mismatched label is rewritten to real name",
			input: "[@FakeName](mention://agent/" + agentRealUUID + ")",
			want:  "[@RealAgent](mention://agent/" + agentRealUUID + ")",
		},
		{
			name:  "agent mention with unresolvable uuid is stripped to plain text",
			input: "hi [@Ghost](mention://agent/" + agentMissingUUID + ") there",
			want:  "hi @Ghost there",
		},
		{
			name:  "squad mention is canonicalized",
			input: "[@WrongSquadName](mention://squad/" + squadUUID + ")",
			want:  "[@RealSquad](mention://squad/" + squadUUID + ")",
		},
		{
			name:  "member mention is canonicalized",
			input: "[@WrongUser](mention://member/" + userUUID + ")",
			want:  "[@Alice](mention://member/" + userUUID + ")",
		},
		{
			name:  "all mention is untouched",
			input: "[@all](mention://all/all)",
			want:  "[@all](mention://all/all)",
		},
		{
			name:  "issue mention is untouched",
			input: "[MUL-1](mention://issue/" + agentRealUUID + ")",
			want:  "[MUL-1](mention://issue/" + agentRealUUID + ")",
		},
		{
			name:  "mention inside inline code is untouched",
			input: "use `[@Wrong](mention://agent/" + agentRealUUID + ")` to delegate",
			want:  "use `[@Wrong](mention://agent/" + agentRealUUID + ")` to delegate",
		},
		{
			name:  "mention inside fenced code is untouched",
			input: "```\n[@Wrong](mention://agent/" + agentRealUUID + ")\n```",
			want:  "```\n[@Wrong](mention://agent/" + agentRealUUID + ")\n```",
		},
		{
			name: "multiple mentions are all canonicalized",
			input: "[@FakeA](mention://agent/" + agentRealUUID + ") cc [@FakeB](mention://member/" + userUUID +
				") and squad [@FakeC](mention://squad/" + squadUUID + ")",
			want: "[@RealAgent](mention://agent/" + agentRealUUID + ") cc [@Alice](mention://member/" + userUUID +
				") and squad [@RealSquad](mention://squad/" + squadUUID + ")",
		},
		{
			name:  "name containing brackets is escaped on rewrite",
			input: "[@WrongLabel](mention://agent/" + agentBracketUUID + ")",
			want:  "[@David\\[TF\\]](mention://agent/" + agentBracketUUID + ")",
		},
		{
			name:  "empty content is empty",
			input: "",
			want:  "",
		},
		{
			name:  "no mentions is no-op",
			input: "Just plain text without mentions.",
			want:  "Just plain text without mentions.",
		},
		{
			name: "mix of valid and stripped mentions",
			input: "[@FakeReal](mention://agent/" + agentRealUUID + ") and [@FakeGone](mention://agent/" +
				agentMissingUUID + ")",
			want: "[@RealAgent](mention://agent/" + agentRealUUID + ") and @FakeGone",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CanonicalizeMentions(ctx, resolver, ws, tt.input)
			if got != tt.want {
				t.Errorf("CanonicalizeMentions() =\n  %q\nwant:\n  %q", got, tt.want)
			}
		})
	}
}

// TestCanonicalizeMentions_CrossWorkspaceAgentStripped verifies that an agent
// mention whose UUID resolves only in a DIFFERENT workspace gets stripped, so
// the rendered comment does not falsely advertise an unrelated workspace's
// agent as part of this workspace's conversation.
func TestCanonicalizeMentions_CrossWorkspaceAgentStripped(t *testing.T) {
	ctx := context.Background()
	wsA := makeUUID("ws-A-------")
	wsB := makeUUID("ws-B-------")

	agentB := makeUUID("agent-in-B-")
	agentBUUID := uuidToString(agentB)

	// Resolver scoped to wsB only. Lookups against wsA fail by design.
	resolver := &nameResolverMock{
		workspaceID: wsB,
		agents:      map[string]string{agentBUUID: "AgentInB"},
	}

	input := "ping [@AgentInB](mention://agent/" + agentBUUID + ")"
	want := "ping @AgentInB"

	got := CanonicalizeMentions(ctx, resolver, wsA, input)
	if got != want {
		t.Errorf("cross-workspace agent should be stripped:\n got:  %q\n want: %q", got, want)
	}
}

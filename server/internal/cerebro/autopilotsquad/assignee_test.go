package autopilotsquad

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	testWorkspaceID = "00000000-0000-0000-0000-000000000001"
	testAgentID     = "00000000-0000-0000-0000-000000000002"
	testSquadID     = "00000000-0000-0000-0000-000000000003"
	testLeaderID    = "00000000-0000-0000-0000-000000000004"
)

func TestApplyUpdateRequiresAssigneeIDWhenChangingType(t *testing.T) {
	prev := db.Autopilot{
		WorkspaceID:  util.MustParseUUID(testWorkspaceID),
		AssigneeType: "agent",
		AssigneeID:   util.MustParseUUID(testAgentID),
	}
	nextType := "squad"

	err := ApplyUpdate(context.Background(), fakeStore{}, prev, UpdateInput{
		AssigneeTypeSent: true,
		AssigneeType:     &nextType,
	}, &db.UpdateAutopilotParams{})

	if err == nil || err.StatusCode != 400 || err.Message != "assignee_id is required when changing assignee_type" {
		t.Fatalf("unexpected error: %#v", err)
	}
}

func TestApplyUpdateRejectsArchivedSquad(t *testing.T) {
	prev := db.Autopilot{
		WorkspaceID:  util.MustParseUUID(testWorkspaceID),
		AssigneeType: "agent",
		AssigneeID:   util.MustParseUUID(testAgentID),
	}
	nextType := "squad"
	nextID := testSquadID

	err := ApplyUpdate(context.Background(), fakeStore{
		squads: map[string]db.Squad{
			testSquadID: {
				ID:          util.MustParseUUID(testSquadID),
				WorkspaceID: util.MustParseUUID(testWorkspaceID),
				LeaderID:    util.MustParseUUID(testLeaderID),
				ArchivedAt:  pgtype.Timestamptz{Valid: true},
			},
		},
	}, prev, UpdateInput{
		AssigneeTypeSent: true,
		AssigneeIDSent:   true,
		AssigneeType:     &nextType,
		AssigneeID:       &nextID,
	}, &db.UpdateAutopilotParams{})

	if err == nil || err.StatusCode != 422 || err.Message != "squad is archived; pick a different squad" {
		t.Fatalf("unexpected error: %#v", err)
	}
}

func TestApplyUpdateAcceptsSquadWithActiveLeader(t *testing.T) {
	prev := db.Autopilot{
		WorkspaceID:  util.MustParseUUID(testWorkspaceID),
		AssigneeType: "agent",
		AssigneeID:   util.MustParseUUID(testAgentID),
	}
	nextType := "squad"
	nextID := testSquadID
	params := db.UpdateAutopilotParams{}

	err := ApplyUpdate(context.Background(), fakeStore{
		agents: map[string]db.Agent{
			testLeaderID: {
				ID:          util.MustParseUUID(testLeaderID),
				WorkspaceID: util.MustParseUUID(testWorkspaceID),
			},
		},
		squads: map[string]db.Squad{
			testSquadID: {
				ID:          util.MustParseUUID(testSquadID),
				WorkspaceID: util.MustParseUUID(testWorkspaceID),
				LeaderID:    util.MustParseUUID(testLeaderID),
			},
		},
	}, prev, UpdateInput{
		AssigneeTypeSent: true,
		AssigneeIDSent:   true,
		AssigneeType:     &nextType,
		AssigneeID:       &nextID,
	}, &params)

	if err != nil {
		t.Fatalf("unexpected error: %#v", err)
	}
	if !params.AssigneeType.Valid || params.AssigneeType.String != "squad" {
		t.Fatalf("assignee type not applied: %#v", params.AssigneeType)
	}
	if util.UUIDToString(params.AssigneeID) != testSquadID {
		t.Fatalf("assignee id not applied: %s", util.UUIDToString(params.AssigneeID))
	}
}

type fakeStore struct {
	agents map[string]db.Agent
	squads map[string]db.Squad
}

func (s fakeStore) GetAgent(_ context.Context, id pgtype.UUID) (db.Agent, error) {
	agent, ok := s.agents[util.UUIDToString(id)]
	if !ok {
		return db.Agent{}, errors.New("agent not found")
	}
	return agent, nil
}

func (s fakeStore) GetAgentInWorkspace(_ context.Context, arg db.GetAgentInWorkspaceParams) (db.Agent, error) {
	agent, ok := s.agents[util.UUIDToString(arg.ID)]
	if !ok || util.UUIDToString(agent.WorkspaceID) != util.UUIDToString(arg.WorkspaceID) {
		return db.Agent{}, errors.New("agent not found")
	}
	return agent, nil
}

func (s fakeStore) GetSquadInWorkspace(_ context.Context, arg db.GetSquadInWorkspaceParams) (db.Squad, error) {
	squad, ok := s.squads[util.UUIDToString(arg.ID)]
	if !ok || util.UUIDToString(squad.WorkspaceID) != util.UUIDToString(arg.WorkspaceID) {
		return db.Squad{}, errors.New("squad not found")
	}
	return squad, nil
}

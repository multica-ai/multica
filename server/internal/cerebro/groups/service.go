package groups

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	EventGroupCreated       = "group:created"
	EventGroupUpdated       = "group:updated"
	EventGroupDeleted       = "group:deleted"
	EventGroupMemberAdded   = "group:member_added"
	EventGroupMemberRemoved = "group:member_removed"
)

var (
	ErrGroupNotFound          = errors.New("group not found")
	ErrGroupMemberNotFound    = errors.New("group member not found")
	ErrInvalidGroupName       = errors.New("group name is required")
	ErrUserNotWorkspaceMember = errors.New("user is not a workspace member")
)

type Service struct {
	Cerebro  *cerebrodb.Queries
	Upstream *db.Queries
	Bus      *events.Bus
}

func NewService(cerebro *cerebrodb.Queries, upstream *db.Queries, bus *events.Bus) *Service {
	return &Service{Cerebro: cerebro, Upstream: upstream, Bus: bus}
}

func (s *Service) List(ctx context.Context, workspaceID pgtype.UUID) ([]cerebrodb.CerebroGroup, error) {
	return s.Cerebro.ListCerebroGroups(ctx, workspaceID)
}

func (s *Service) Get(ctx context.Context, workspaceID, groupID pgtype.UUID) (cerebrodb.CerebroGroup, error) {
	group, err := s.Cerebro.GetCerebroGroup(ctx, groupID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return cerebrodb.CerebroGroup{}, ErrGroupNotFound
		}
		return cerebrodb.CerebroGroup{}, err
	}
	if group.WorkspaceID != workspaceID {
		return cerebrodb.CerebroGroup{}, ErrGroupNotFound
	}
	return group, nil
}

func (s *Service) Create(ctx context.Context, workspaceID, actorID pgtype.UUID, name string, description *string) (cerebrodb.CerebroGroup, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return cerebrodb.CerebroGroup{}, ErrInvalidGroupName
	}

	group, err := s.Cerebro.CreateCerebroGroup(ctx, cerebrodb.CreateCerebroGroupParams{
		WorkspaceID: workspaceID,
		Name:        name,
		Description: textOrEmpty(description),
		CreatedBy:   actorID,
	})
	if err != nil {
		return cerebrodb.CerebroGroup{}, err
	}
	s.publish(EventGroupCreated, workspaceID, actorID, map[string]any{"group": groupResponseFromModel(group)})
	return group, nil
}

func (s *Service) Update(ctx context.Context, workspaceID, actorID, groupID pgtype.UUID, name *string, description *string) (cerebrodb.CerebroGroup, error) {
	if _, err := s.Get(ctx, workspaceID, groupID); err != nil {
		return cerebrodb.CerebroGroup{}, err
	}

	var nameArg pgtype.Text
	if name != nil {
		trimmed := strings.TrimSpace(*name)
		if trimmed == "" {
			return cerebrodb.CerebroGroup{}, ErrInvalidGroupName
		}
		nameArg = pgtype.Text{String: trimmed, Valid: true}
	}

	group, err := s.Cerebro.UpdateCerebroGroup(ctx, cerebrodb.UpdateCerebroGroupParams{
		ID:          groupID,
		Name:        nameArg,
		Description: textFromPtr(description),
	})
	if err != nil {
		return cerebrodb.CerebroGroup{}, err
	}
	s.publish(EventGroupUpdated, workspaceID, actorID, map[string]any{"group": groupResponseFromModel(group)})
	return group, nil
}

func (s *Service) Delete(ctx context.Context, workspaceID, actorID, groupID pgtype.UUID) (cerebrodb.CerebroGroup, error) {
	group, err := s.Get(ctx, workspaceID, groupID)
	if err != nil {
		return cerebrodb.CerebroGroup{}, err
	}
	if err := s.Cerebro.DeleteCerebroGroup(ctx, groupID); err != nil {
		return cerebrodb.CerebroGroup{}, err
	}
	s.publish(EventGroupDeleted, workspaceID, actorID, map[string]any{"group": groupResponseFromModel(group)})
	return group, nil
}

func (s *Service) AddMember(ctx context.Context, workspaceID, actorID, groupID, userID pgtype.UUID) (cerebrodb.AddCerebroGroupMemberRow, error) {
	if _, err := s.Get(ctx, workspaceID, groupID); err != nil {
		return cerebrodb.AddCerebroGroupMemberRow{}, err
	}
	if err := s.requireWorkspaceMember(ctx, workspaceID, userID); err != nil {
		return cerebrodb.AddCerebroGroupMemberRow{}, err
	}

	member, err := s.Cerebro.AddCerebroGroupMember(ctx, cerebrodb.AddCerebroGroupMemberParams{
		GroupID: groupID,
		UserID:  userID,
		AddedBy: actorID,
	})
	if err != nil {
		return cerebrodb.AddCerebroGroupMemberRow{}, err
	}
	s.publish(EventGroupMemberAdded, workspaceID, actorID, map[string]any{
		"group_id": util.UUIDToString(groupID),
		"member":   groupMemberResponseFromAdd(member),
	})
	return member, nil
}

func (s *Service) RemoveMember(ctx context.Context, workspaceID, actorID, groupID, userID pgtype.UUID) (cerebrodb.RemoveCerebroGroupMemberRow, error) {
	if _, err := s.Get(ctx, workspaceID, groupID); err != nil {
		return cerebrodb.RemoveCerebroGroupMemberRow{}, err
	}

	member, err := s.Cerebro.RemoveCerebroGroupMember(ctx, cerebrodb.RemoveCerebroGroupMemberParams{
		GroupID: groupID,
		UserID:  userID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return cerebrodb.RemoveCerebroGroupMemberRow{}, ErrGroupMemberNotFound
		}
		return cerebrodb.RemoveCerebroGroupMemberRow{}, err
	}
	s.publish(EventGroupMemberRemoved, workspaceID, actorID, map[string]any{
		"group_id": util.UUIDToString(groupID),
		"member":   groupMemberResponseFromRemoved(member),
	})
	return member, nil
}

func (s *Service) ListMembers(ctx context.Context, workspaceID, groupID pgtype.UUID) ([]cerebrodb.ListCerebroGroupMembersRow, error) {
	if _, err := s.Get(ctx, workspaceID, groupID); err != nil {
		return nil, err
	}
	return s.Cerebro.ListCerebroGroupMembers(ctx, groupID)
}

func (s *Service) requireWorkspaceMember(ctx context.Context, workspaceID, userID pgtype.UUID) error {
	_, err := s.Upstream.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      userID,
		WorkspaceID: workspaceID,
	})
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrUserNotWorkspaceMember
	}
	return err
}

func (s *Service) publish(eventType string, workspaceID, actorID pgtype.UUID, payload any) {
	if s.Bus == nil {
		return
	}
	s.Bus.Publish(events.Event{
		Type:        eventType,
		WorkspaceID: util.UUIDToString(workspaceID),
		ActorType:   "member",
		ActorID:     util.UUIDToString(actorID),
		Payload:     payload,
	})
}

func textFromPtr(s *string) pgtype.Text {
	if s == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *s, Valid: true}
}

// textOrEmpty mirrors textFromPtr but treats nil as the empty string instead of
// SQL NULL, so a missing/null description on create maps to "" — avoids 23502
// regressions if a NOT NULL constraint ever gets added downstream, and keeps
// the response shape (`"description": ""`) consistent for empty descriptions.
func textOrEmpty(s *string) pgtype.Text {
	if s == nil {
		return pgtype.Text{String: "", Valid: true}
	}
	return pgtype.Text{String: *s, Valid: true}
}

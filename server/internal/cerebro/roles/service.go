// Package roles implements role management for the Multica permission engine
// (FIR-2130). A role is a first-class permission subject: grants attach to a
// role (subject_type='role'), and members or agents are assigned to roles. The
// permission resolver expands an actor's assigned roles into Actor.RoleIDs and
// matches role-scoped grants against them.
package roles

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
	EventRoleCreated    = "role:created"
	EventRoleUpdated    = "role:updated"
	EventRoleDeleted    = "role:deleted"
	EventRoleAssigned   = "role:assigned"
	EventRoleUnassigned = "role:unassigned"
)

// Subject types a role can be assigned to.
const (
	SubjectMember = "member"
	SubjectAgent  = "agent"
)

var (
	ErrRoleNotFound       = errors.New("role not found")
	ErrInvalidRoleName    = errors.New("role name is required")
	ErrInvalidSubjectType = errors.New("subject_type must be member or agent")
	ErrSubjectNotFound    = errors.New("subject not found in workspace")
	ErrAssignmentNotFound = errors.New("role assignment not found")
)

type Service struct {
	Cerebro  *cerebrodb.Queries
	Upstream *db.Queries
	Bus      *events.Bus
}

func NewService(cerebro *cerebrodb.Queries, upstream *db.Queries, bus *events.Bus) *Service {
	return &Service{Cerebro: cerebro, Upstream: upstream, Bus: bus}
}

func (s *Service) List(ctx context.Context, workspaceID pgtype.UUID) ([]cerebrodb.CerebroRole, error) {
	return s.Cerebro.ListCerebroRoles(ctx, workspaceID)
}

func (s *Service) Get(ctx context.Context, workspaceID, roleID pgtype.UUID) (cerebrodb.CerebroRole, error) {
	role, err := s.Cerebro.GetCerebroRole(ctx, roleID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return cerebrodb.CerebroRole{}, ErrRoleNotFound
		}
		return cerebrodb.CerebroRole{}, err
	}
	if role.WorkspaceID != workspaceID {
		return cerebrodb.CerebroRole{}, ErrRoleNotFound
	}
	return role, nil
}

func (s *Service) Create(ctx context.Context, workspaceID, actorID pgtype.UUID, name string, description *string) (cerebrodb.CerebroRole, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return cerebrodb.CerebroRole{}, ErrInvalidRoleName
	}
	role, err := s.Cerebro.CreateCerebroRole(ctx, cerebrodb.CreateCerebroRoleParams{
		WorkspaceID: workspaceID,
		Name:        name,
		Description: strOrEmpty(description),
		CreatedBy:   actorID,
	})
	if err != nil {
		return cerebrodb.CerebroRole{}, err
	}
	s.publish(EventRoleCreated, workspaceID, actorID, map[string]any{"role": roleResponseFromModel(role)})
	return role, nil
}

func (s *Service) Update(ctx context.Context, workspaceID, actorID, roleID pgtype.UUID, name, description *string) (cerebrodb.CerebroRole, error) {
	if _, err := s.Get(ctx, workspaceID, roleID); err != nil {
		return cerebrodb.CerebroRole{}, err
	}

	var nameArg pgtype.Text
	if name != nil {
		trimmed := strings.TrimSpace(*name)
		if trimmed == "" {
			return cerebrodb.CerebroRole{}, ErrInvalidRoleName
		}
		nameArg = pgtype.Text{String: trimmed, Valid: true}
	}

	role, err := s.Cerebro.UpdateCerebroRole(ctx, cerebrodb.UpdateCerebroRoleParams{
		ID:          roleID,
		Name:        nameArg,
		Description: textFromPtr(description),
	})
	if err != nil {
		return cerebrodb.CerebroRole{}, err
	}
	s.publish(EventRoleUpdated, workspaceID, actorID, map[string]any{"role": roleResponseFromModel(role)})
	return role, nil
}

func (s *Service) Delete(ctx context.Context, workspaceID, actorID, roleID pgtype.UUID) (cerebrodb.CerebroRole, error) {
	role, err := s.Get(ctx, workspaceID, roleID)
	if err != nil {
		return cerebrodb.CerebroRole{}, err
	}
	if err := s.Cerebro.DeleteCerebroRole(ctx, roleID); err != nil {
		return cerebrodb.CerebroRole{}, err
	}
	s.publish(EventRoleDeleted, workspaceID, actorID, map[string]any{"role": roleResponseFromModel(role)})
	return role, nil
}

func (s *Service) Assign(ctx context.Context, workspaceID, actorID, roleID, subjectID pgtype.UUID, subjectType string) (cerebrodb.AssignCerebroRoleRow, error) {
	if _, err := s.Get(ctx, workspaceID, roleID); err != nil {
		return cerebrodb.AssignCerebroRoleRow{}, err
	}
	subjectType = strings.ToLower(strings.TrimSpace(subjectType))
	if subjectType != SubjectMember && subjectType != SubjectAgent {
		return cerebrodb.AssignCerebroRoleRow{}, ErrInvalidSubjectType
	}
	if err := s.requireSubjectExists(ctx, workspaceID, subjectType, subjectID); err != nil {
		return cerebrodb.AssignCerebroRoleRow{}, err
	}

	assignment, err := s.Cerebro.AssignCerebroRole(ctx, cerebrodb.AssignCerebroRoleParams{
		RoleID:      roleID,
		SubjectType: subjectType,
		SubjectID:   subjectID,
		AddedBy:     actorID,
	})
	if err != nil {
		return cerebrodb.AssignCerebroRoleRow{}, err
	}
	s.publish(EventRoleAssigned, workspaceID, actorID, map[string]any{
		"role_id":    util.UUIDToString(roleID),
		"assignment": roleAssignmentResponseFromAssign(assignment),
	})
	return assignment, nil
}

func (s *Service) Unassign(ctx context.Context, workspaceID, actorID, roleID, subjectID pgtype.UUID, subjectType string) (cerebrodb.CerebroRoleAssignment, error) {
	if _, err := s.Get(ctx, workspaceID, roleID); err != nil {
		return cerebrodb.CerebroRoleAssignment{}, err
	}
	subjectType = strings.ToLower(strings.TrimSpace(subjectType))
	if subjectType != SubjectMember && subjectType != SubjectAgent {
		return cerebrodb.CerebroRoleAssignment{}, ErrInvalidSubjectType
	}

	assignment, err := s.Cerebro.UnassignCerebroRole(ctx, cerebrodb.UnassignCerebroRoleParams{
		RoleID:      roleID,
		SubjectType: subjectType,
		SubjectID:   subjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return cerebrodb.CerebroRoleAssignment{}, ErrAssignmentNotFound
		}
		return cerebrodb.CerebroRoleAssignment{}, err
	}
	s.publish(EventRoleUnassigned, workspaceID, actorID, map[string]any{
		"role_id":    util.UUIDToString(roleID),
		"assignment": roleAssignmentResponseFromModel(assignment),
	})
	return assignment, nil
}

func (s *Service) ListAssignments(ctx context.Context, workspaceID, roleID pgtype.UUID) ([]cerebrodb.ListCerebroRoleAssignmentsWithNamesRow, error) {
	if _, err := s.Get(ctx, workspaceID, roleID); err != nil {
		return nil, err
	}
	return s.Cerebro.ListCerebroRoleAssignmentsWithNames(ctx, roleID)
}

func (s *Service) requireSubjectExists(ctx context.Context, workspaceID pgtype.UUID, subjectType string, subjectID pgtype.UUID) error {
	switch subjectType {
	case SubjectMember:
		_, err := s.Upstream.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
			UserID:      subjectID,
			WorkspaceID: workspaceID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrSubjectNotFound
		}
		return err
	case SubjectAgent:
		_, err := s.Upstream.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{
			ID:          subjectID,
			WorkspaceID: workspaceID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrSubjectNotFound
		}
		return err
	}
	return ErrInvalidSubjectType
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

func strOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

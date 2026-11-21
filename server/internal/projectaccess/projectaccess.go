// Package projectaccess decides which private projects (MUL-8472) a viewer may
// see. It is the single source of the rule; handlers, the task service and the
// realtime fanout all call into it instead of re-deriving visibility.
//
// The rule: a project with visibility "private" is visible to its
// project_member rows and to nobody else. A workspace role grants no
// visibility: an owner or admin sees a private project only as one of its
// members (MUL-9284). Every other project is visible to every workspace
// member. An issue's visibility is its project's.
//
// A guest (MUL-9420) sees only the projects it is a member of, whatever their
// visibility, and nothing outside a project. Its hidden set therefore holds
// every other project plus NoProject, which stands in for "no project" so the
// same SQL predicates (COALESCE(project_id, NoProject)) hide project-less rows.
//
// A viewer is a set of principals (human user ids). A member request has one
// principal: the member. An agent run has two: the human who triggered it and
// the agent's owner, and sees a private project only when both can. A viewer
// with no principal sees no private project.
package projectaccess

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	VisibilityWorkspace = "workspace"
	VisibilityPrivate   = "private"

	RoleGuest = "guest"
)

// NoProject is the hidden-set entry for objects outside any project. It is the
// nil UUID, which no project id can be.
var NoProject = pgtype.UUID{Valid: true}

// IsGuestRole reports whether a workspace role is the project-scoped guest.
func IsGuestRole(role string) bool {
	return role == RoleGuest
}

// ValidVisibility reports whether v is a storable project visibility.
func ValidVisibility(v string) bool {
	return v == VisibilityWorkspace || v == VisibilityPrivate
}

// IsManagerRole reports whether a workspace role may manage the access of a
// project its holder can see. It grants no visibility of its own.
func IsManagerRole(role string) bool {
	return role == "owner" || role == "admin"
}

// Querier is the subset of *db.Queries this package reads.
type Querier interface {
	GetMemberByUserAndWorkspace(ctx context.Context, arg db.GetMemberByUserAndWorkspaceParams) (db.Member, error)
	ListHiddenProjectIDsForUser(ctx context.Context, arg db.ListHiddenProjectIDsForUserParams) ([]pgtype.UUID, error)
	ListHiddenProjectIDsForGuest(ctx context.Context, arg db.ListHiddenProjectIDsForGuestParams) ([]pgtype.UUID, error)
	ListPrivateProjectIDs(ctx context.Context, workspaceID pgtype.UUID) ([]pgtype.UUID, error)
}

// Hidden is the set of private projects a viewer may not see. The zero value
// hides nothing.
type Hidden struct {
	ids []pgtype.UUID
	set map[[16]byte]struct{}
}

func newHidden() Hidden { return Hidden{set: map[[16]byte]struct{}{}} }

func (h *Hidden) add(ids []pgtype.UUID) {
	for _, id := range ids {
		if !id.Valid {
			continue
		}
		if _, ok := h.set[id.Bytes]; ok {
			continue
		}
		h.set[id.Bytes] = struct{}{}
		h.ids = append(h.ids, id)
	}
}

// Empty reports whether the viewer sees every project.
func (h Hidden) Empty() bool { return len(h.ids) == 0 }

// CanSeeProject reports whether an object in projectID is visible. Objects
// outside any project (invalid projectID) are visible unless the set hides
// NoProject.
func (h Hidden) CanSeeProject(projectID pgtype.UUID) bool {
	if len(h.set) == 0 {
		return true
	}
	if !projectID.Valid {
		projectID = NoProject
	}
	_, hidden := h.set[projectID.Bytes]
	return !hidden
}

// Param is the value for a `hidden_project_ids` query argument: nil when
// nothing is hidden, so the SQL predicate short-circuits.
func (h Hidden) Param() []pgtype.UUID {
	if len(h.ids) == 0 {
		return nil
	}
	return h.ids
}

// IDs returns the hidden project ids (never nil, for dynamic SQL builders).
func (h Hidden) IDs() []pgtype.UUID {
	if h.ids == nil {
		return []pgtype.UUID{}
	}
	return h.ids
}

// ForMember computes the hidden set for one current workspace member: every
// private project they are not a member of, or for a guest every project they
// are not a member of and everything outside a project.
func ForMember(ctx context.Context, q Querier, member db.Member) (Hidden, error) {
	h := newHidden()
	if IsGuestRole(member.Role) {
		ids, err := q.ListHiddenProjectIDsForGuest(ctx, db.ListHiddenProjectIDsForGuestParams{
			WorkspaceID: member.WorkspaceID,
			UserID:      member.UserID,
		})
		if err != nil {
			return Hidden{}, err
		}
		h.add([]pgtype.UUID{NoProject})
		h.add(ids)
		return h, nil
	}
	ids, err := q.ListHiddenProjectIDsForUser(ctx, db.ListHiddenProjectIDsForUserParams{
		WorkspaceID: member.WorkspaceID,
		UserID:      member.UserID,
	})
	if err != nil {
		return Hidden{}, err
	}
	h.add(ids)
	return h, nil
}

// ForPrincipals computes the hidden set for a viewer whose access is bounded
// by every listed principal: a project is visible only when each of them can
// see it. Invalid ids are ignored; when none remain, every private project is
// hidden. A principal who is no longer a workspace member sees no private
// project.
func ForPrincipals(ctx context.Context, q Querier, workspaceID pgtype.UUID, principals ...pgtype.UUID) (Hidden, error) {
	h := newHidden()
	private, err := q.ListPrivateProjectIDs(ctx, workspaceID)
	if err != nil {
		return Hidden{}, err
	}
	all := newHidden()
	all.add(private)
	seen := map[[16]byte]struct{}{}
	var valid []pgtype.UUID
	for _, p := range principals {
		if !p.Valid {
			continue
		}
		if _, ok := seen[p.Bytes]; ok {
			continue
		}
		seen[p.Bytes] = struct{}{}
		valid = append(valid, p)
	}
	if len(valid) == 0 {
		return all, nil
	}
	for _, p := range valid {
		member, err := q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
			UserID:      p,
			WorkspaceID: workspaceID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return all, nil
		}
		if err != nil {
			return Hidden{}, err
		}
		// Without private projects only a guest has anything hidden.
		if len(private) == 0 && !IsGuestRole(member.Role) {
			continue
		}
		one, err := ForMember(ctx, q, member)
		if err != nil {
			return Hidden{}, err
		}
		h.add(one.ids)
	}
	return h, nil
}

// RunPrincipals returns the humans that bound an agent run's access: the
// run's originator (falling back to its accountable user, which is how
// owner_fallback and autopilot runs record their human) and the agent owner.
func RunPrincipals(task db.AgentTaskQueue, agentOwnerID pgtype.UUID) []pgtype.UUID {
	originator := task.OriginatorUserID
	if !originator.Valid {
		originator = task.AccountableUserID
	}
	return []pgtype.UUID{originator, agentOwnerID}
}

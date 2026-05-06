package handler

// CEREBRO-PATCH(access-handler): cerebro modification of upstream file

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Access decisions for project-level visibility (JEH-XXX project access).
//
// Three rules, in priority order:
//   1. Workspace owners and admins always have access (governance safety net)
//   2. project.access = 'workspace' → every workspace member has access
//   3. project.access = 'restricted' → only explicit project_member rows
//
// 404 vs 403: callers should respond 404 for missing access, never 403.
// 403 leaks the existence of the resource.

const (
	accessWorkspace  = "workspace"
	accessRestricted = "restricted"
)

// isWorkspaceAdmin returns true if the member's workspace role grants
// implicit access to every project in the workspace.
func isWorkspaceAdmin(member db.Member) bool {
	return member.Role == "owner" || member.Role == "admin"
}

// canAccessProject returns true when the member can see the project and
// everything inside it. Only consults DB for restricted projects where the
// member is not an admin.
func (h *Handler) canAccessProject(ctx context.Context, member db.Member, project db.Project) bool {
	if isWorkspaceAdmin(member) {
		return true
	}
	if project.Access == accessWorkspace {
		return true
	}
	ok, err := h.Queries.IsProjectMember(ctx, db.IsProjectMemberParams{
		ProjectID: project.ID,
		UserID:    member.UserID,
	})
	if err != nil {
		return false
	}
	return ok
}

// canAccessProjectByID is a convenience wrapper for handlers that have a
// project_id pgtype.UUID rather than a loaded project row. Returns false
// (not an error) when the project doesn't exist — callers respond 404.
func (h *Handler) canAccessProjectByID(ctx context.Context, member db.Member, projectID pgtype.UUID) bool {
	if isWorkspaceAdmin(member) {
		return true
	}
	project, err := h.Queries.GetProject(ctx, projectID)
	if err != nil {
		return false
	}
	if uuidToString(project.WorkspaceID) != uuidToString(member.WorkspaceID) {
		return false
	}
	return h.canAccessProject(ctx, member, project)
}

// audienceForIssue returns the set of user IDs that may receive WS events
// for an issue. Returns nil when the issue is open to the whole workspace
// — caller should broadcast normally in that case.
//
//   * issue in workspace-access project → nil (broadcast to all)
//   * issue in restricted project → project_members ∪ workspace owners/admins
//   * standalone non-private issue → nil
//   * standalone private issue → creator ∪ workspace owners/admins
func (h *Handler) audienceForIssue(ctx context.Context, issue db.Issue) []string {
	// Channels and DMs broadcast only to their participants. This isolates
	// chat traffic from the workspace event stream — non-members never see
	// a comment in a private DM, even via WS fan-out.
	if issue.Kind == "channel" || issue.Kind == "dm" {
		subs, err := h.Queries.ListIssueSubscribers(ctx, issue.ID)
		if err != nil {
			return []string{}
		}
		out := make([]string, 0, len(subs))
		for _, s := range subs {
			if s.UserType == "member" {
				out = append(out, uuidToString(s.UserID))
			}
		}
		return out
	}
	if issue.ProjectID.Valid {
		project, err := h.Queries.GetProject(ctx, issue.ProjectID)
		if err != nil {
			return nil
		}
		if project.Access != accessRestricted {
			return nil
		}
		return h.audienceForRestrictedProject(ctx, project)
	}
	if !issue.IsPrivate {
		return nil
	}
	// Standalone private issue → creator + admins.
	adminUUIDs, _ := h.Queries.ListWorkspaceAdminUserIDs(ctx, issue.WorkspaceID)
	out := make([]string, 0, len(adminUUIDs)+1)
	for _, u := range adminUUIDs {
		out = append(out, uuidToString(u))
	}
	if issue.CreatorType == "member" && issue.CreatorID.Valid {
		creator := uuidToString(issue.CreatorID)
		if !containsString(out, creator) {
			out = append(out, creator)
		}
	}
	return out
}

// audienceForRestrictedProject returns project_members ∪ workspace admins.
func (h *Handler) audienceForRestrictedProject(ctx context.Context, project db.Project) []string {
	adminUUIDs, _ := h.Queries.ListWorkspaceAdminUserIDs(ctx, project.WorkspaceID)
	members, _ := h.Queries.ListProjectMembers(ctx, project.ID)
	out := make([]string, 0, len(adminUUIDs)+len(members))
	for _, u := range adminUUIDs {
		out = append(out, uuidToString(u))
	}
	for _, m := range members {
		uid := uuidToString(m.UserID)
		if !containsString(out, uid) {
			out = append(out, uid)
		}
	}
	return out
}

func containsString(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// resolveMemberFromRequest loads the member either from the middleware-
// injected context (production path) or by re-querying using the X-User-ID
// header (test path / handlers without middleware). Returns ok=false when
// neither path produces a member — caller should respond 401.
func (h *Handler) resolveMemberFromRequest(r *http.Request) (db.Member, bool) {
	if m, ok := ctxMember(r.Context()); ok {
		return m, true
	}
	userID := requestUserID(r)
	if userID == "" {
		return db.Member{}, false
	}
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		return db.Member{}, false
	}
	m, err := h.Queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
		UserID:      parseUUID(userID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		return db.Member{}, false
	}
	return m, true
}

// canAccessIssue applies the access rules to an issue:
//   * issue with project_id → defer to canAccessProject
//   * standalone issue with is_private = true → creator + admins only
//   * standalone issue with is_private = false → workspace members
func (h *Handler) canAccessIssue(ctx context.Context, member db.Member, issue db.Issue) bool {
	// Channels and DMs are subscriber-gated, not project/privacy-gated.
	// Admins do NOT get implicit read access — a private DM stays private
	// even from workspace owners. This matches the chat_session model.
	if issue.Kind == "channel" || issue.Kind == "dm" {
		ok, err := h.Queries.IsIssueSubscriber(ctx, db.IsIssueSubscriberParams{
			IssueID:  issue.ID,
			UserType: "member",
			UserID:   member.UserID,
		})
		return err == nil && ok
	}
	if isWorkspaceAdmin(member) {
		return true
	}
	if issue.ProjectID.Valid {
		project, err := h.Queries.GetProject(ctx, issue.ProjectID)
		if err != nil {
			return false
		}
		return h.canAccessProject(ctx, member, project)
	}
	if !issue.IsPrivate {
		return true
	}
	if issue.CreatorType == "member" &&
		issue.CreatorID.Valid && uuidToString(issue.CreatorID) == uuidToString(member.UserID) {
		return true
	}
	return false
}

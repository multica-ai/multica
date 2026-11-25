package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/inbox"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/projectaccess"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// Owner recovery for private projects (MUL-9284).
//
// A private project is visible to its members only, so one whose members have
// all left would be lost to the workspace. The workspace owner is the way back
// in: they can list every private project by name and members, and add
// themselves to one. Joining is an ordinary membership, shown in the member
// list and announced to the existing members; until they join, the owner
// reads none of the project's content. Once on the list they manage its
// access like any owner or admin who is a member (canManageProjectAccess).
//
// These two handlers are the only code that reads private projects without
// filtering by membership.

// inboxTypeProjectOwnerJoined tells a private project's members that a
// workspace owner added themselves to it.
const inboxTypeProjectOwnerJoined = "project_owner_joined"

type PrivateProjectOverviewMember struct {
	UserID    string  `json:"user_id"`
	Name      string  `json:"name"`
	Email     string  `json:"email"`
	AvatarURL *string `json:"avatar_url"`
}

type PrivateProjectOverview struct {
	ID         string                         `json:"id"`
	Title      string                         `json:"title"`
	Icon       *string                        `json:"icon"`
	LeadType   *string                        `json:"lead_type"`
	LeadID     *string                        `json:"lead_id"`
	CreatedAt  string                         `json:"created_at"`
	ArchivedAt *string                        `json:"archived_at"`
	IsMember   bool                           `json:"is_member"`
	Members    []PrivateProjectOverviewMember `json:"members"`
}

// requireWorkspaceOwnerRecovery admits a human workspace owner and nobody
// else: not admins, not an agent running on an owner's behalf, and not a
// machine credential (task token or cloud node PAT) that auth stamps with its
// owner's user id. The router also wraps both routes in RequireHumanActor;
// this check is the handler's own backstop.
func (h *Handler) requireWorkspaceOwnerRecovery(w http.ResponseWriter, r *http.Request, workspaceID string) (db.Member, bool) {
	if isMachineCredentialActor(r) {
		writeError(w, http.StatusForbidden, "machine credentials cannot use workspace owner access to private projects")
		return db.Member{}, false
	}
	if actorType, _ := h.resolveActor(r, requestUserID(r), workspaceID); actorType == "agent" {
		writeError(w, http.StatusForbidden, "agents cannot use workspace owner access to private projects")
		return db.Member{}, false
	}
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return db.Member{}, false
	}
	if member.Role != "owner" {
		writeError(w, http.StatusForbidden, "only workspace owners can list or join private projects they are not a member of")
		return db.Member{}, false
	}
	return member, true
}

// ListPrivateProjectsForOwner — GET /api/projects/private
func (h *Handler) ListPrivateProjectsForOwner(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	owner, ok := h.requireWorkspaceOwnerRecovery(w, r, workspaceID)
	if !ok {
		return
	}
	projects, err := h.Queries.ListPrivateProjects(r.Context(), owner.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list private projects")
		return
	}
	memberRows, err := h.Queries.ListPrivateProjectMembers(r.Context(), owner.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list private project members")
		return
	}
	membersByProject := make(map[pgtype.UUID][]PrivateProjectOverviewMember, len(projects))
	joined := make(map[pgtype.UUID]bool, len(projects))
	for _, row := range memberRows {
		membersByProject[row.ProjectID] = append(membersByProject[row.ProjectID], PrivateProjectOverviewMember{
			UserID:    uuidToString(row.UserID),
			Name:      row.UserName,
			Email:     row.UserEmail,
			AvatarURL: textToPtr(row.UserAvatarUrl),
		})
		if row.UserID == owner.UserID {
			joined[row.ProjectID] = true
		}
	}
	resp := make([]PrivateProjectOverview, len(projects))
	for i, p := range projects {
		members := membersByProject[p.ID]
		if members == nil {
			members = []PrivateProjectOverviewMember{}
		}
		resp[i] = PrivateProjectOverview{
			ID:         uuidToString(p.ID),
			Title:      p.Title,
			Icon:       textToPtr(p.Icon),
			LeadType:   textToPtr(p.LeadType),
			LeadID:     uuidToPtr(p.LeadID),
			CreatedAt:  timestampToString(p.CreatedAt),
			ArchivedAt: timestampToPtr(p.ArchivedAt),
			IsMember:   joined[p.ID],
			Members:    members,
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": resp, "total": len(resp)})
}

// JoinPrivateProject — POST /api/projects/{id}/join
//
// The workspace owner adds themselves to a private project. Its lead is left
// as it is.
func (h *Handler) JoinPrivateProject(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	owner, ok := h.requireWorkspaceOwnerRecovery(w, r, workspaceID)
	if !ok {
		return
	}
	idUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "project id")
	if !ok {
		return
	}
	project, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{ID: idUUID, WorkspaceID: owner.WorkspaceID})
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	if projectVisibilityOrDefault(project.Visibility) != projectaccess.VisibilityPrivate {
		writeError(w, http.StatusBadRequest, "project is not private")
		return
	}

	ctx := r.Context()
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)

	existing, err := qtx.ListProjectMemberUserIDs(ctx, db.ListProjectMemberUserIDsParams{
		ProjectID: project.ID, WorkspaceID: project.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list project members")
		return
	}
	inserted, err := qtx.AddProjectMembers(ctx, db.AddProjectMembersParams{
		ProjectID:   project.ID,
		WorkspaceID: project.WorkspaceID,
		UserIds:     []pgtype.UUID{owner.UserID},
		AddedBy:     owner.UserID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to join project")
		return
	}
	if len(inserted) == 0 {
		// Already a member: nothing changed, nobody to tell.
		writeJSON(w, http.StatusOK, projectToResponse(project))
		return
	}

	if err := touchProjectSearchIndex(r, qtx, project); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update search index")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit project join")
		return
	}

	slog.Info("workspace owner joined private project", append(logger.RequestAttrs(r),
		"project_id", uuidToString(project.ID),
		"user_id", uuidToString(owner.UserID),
	)...)

	h.publishProjectMembersChanged(project, owner.UserID)
	h.notifyProjectOwnerJoined(ctx, project, owner.UserID, existing)
	writeJSON(w, http.StatusOK, projectToResponse(project))
}

// notifyProjectOwnerJoined tells the members a private project already had
// that a workspace owner let themselves in. Best effort: the join stands even
// if a notice fails, and the member list shows the owner either way.
func (h *Handler) notifyProjectOwnerJoined(ctx context.Context, project db.Project, ownerID pgtype.UUID, recipients []pgtype.UUID) {
	if len(recipients) == 0 {
		return
	}
	name := "A workspace owner"
	if u, err := h.Queries.GetUser(ctx, ownerID); err == nil && u.Name != "" {
		name = u.Name
	}
	details, _ := json.Marshal(map[string]string{
		"project_id":    uuidToString(project.ID),
		"project_title": project.Title,
		"joined_by":     name,
	})
	body := name + " is a workspace owner and added themselves to this private project. They can now see its issues."
	for _, recipient := range recipients {
		if recipient == ownerID {
			continue
		}
		_, _, err := inbox.Deliver(ctx, h.Queries, h.Bus, inbox.Notification{Params: db.CreateInboxItemParams{
			ID:            dbid.NewV7(),
			WorkspaceID:   project.WorkspaceID,
			RecipientType: "member",
			RecipientID:   recipient,
			Type:          inboxTypeProjectOwnerJoined,
			Severity:      "info",
			IssueID:       pgtype.UUID{},
			Title:         project.Title,
			Body:          pgtype.Text{String: body, Valid: true},
			ActorType:     pgtype.Text{String: "member", Valid: true},
			ActorID:       ownerID,
			Details:       details,
		}})
		if err != nil {
			slog.Warn("project owner-joined notice failed",
				"project_id", uuidToString(project.ID),
				"recipient_id", uuidToString(recipient),
				"error", err,
			)
		}
	}
}

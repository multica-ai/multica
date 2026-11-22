import { onlineManager, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { projectKeys, projectMembersOptions } from "./queries";
import { issueKeys } from "../issues/queries";
import { refreshFeaturesOnLockedError } from "../billing/paid-features";
import type { Project, ProjectVisibility } from "../types";

// Mirrors the server (canManageProjectAccess in project_member.go). The caller
// already sees the project, which on a private one means being a member: a
// role opens nothing by itself.
//
// Owners and admins manage access. On a private project so does the one
// member the server names as its manager: the lead when that is a person,
// otherwise the creator, otherwise the longest-standing member. A lead may be
// an agent or nobody, so the lead alone does not say who that is.
//
// Anyone may set the lead of a workspace-visible project, so leading one
// grants nothing; making it private takes an owner/admin.
export function canManageProjectAccess(
  project: Pick<Project, "lead_type" | "lead_id" | "visibility">,
  userId: string | null | undefined,
  role: string | null | undefined,
  managerId: string | null | undefined,
): boolean {
  if (!userId || role === "guest") return false;
  if (role === "owner" || role === "admin") return true;
  if (project.visibility !== "private") return false;
  // No manager named means the answer has not loaded, or the server predates
  // the rule. A person who leads the project is its manager either way.
  const manager = managerId ?? (project.lead_type === "member" ? project.lead_id : null);
  return manager === userId;
}

// Changing the lead of a private project can change who manages its access,
// so only access managers may; elsewhere the lead stays an ordinary field.
// A guest edits no project, so it sets no lead either.
export function canChangeProjectLead(
  project: Pick<Project, "lead_type" | "lead_id" | "visibility">,
  userId: string | null | undefined,
  role: string | null | undefined,
  managerId: string | null | undefined,
): boolean {
  if (role === "guest") return false;
  return project.visibility !== "private" || canManageProjectAccess(project, userId, role, managerId);
}

// The manager of a private project, for a caller whose role does not already
// settle whether they manage it. Owners, admins and workspace-visible
// projects cost no request, and neither does a caller whose role is not
// known yet. A guest manages nothing and may not list project members.
export function useProjectAccessManagerId(
  wsId: string,
  project: Pick<Project, "id" | "visibility">,
  userId: string | null | undefined,
  role: string | null | undefined,
): string | null {
  const needed =
    project.visibility === "private" && !!userId && !!role && role !== "owner" && role !== "admin" && role !== "guest";
  const { data } = useQuery({ ...projectMembersOptions(wsId, project.id), enabled: needed });
  return needed ? data?.manager_id ?? null : null;
}

type AccessChange =
  | { type: "visibility"; visibility: ProjectVisibility; memberIds: string[] }
  | { type: "add"; userId: string }
  | { type: "remove"; userId: string };

export function useProjectAccessMutation(wsId: string, projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    networkMode: "always",
    retry: false,
    mutationFn: async (change: AccessChange) => {
      if (!onlineManager.isOnline()) throw new Error("You're offline. Reconnect and try again.");
      if (change.type === "visibility") {
        await api.updateProject(projectId, { visibility: change.visibility, member_ids: change.memberIds });
      } else if (change.type === "add") {
        await api.addProjectMembers(projectId, [change.userId]);
      } else {
        await api.removeProjectMember(projectId, change.userId);
      }
    },
    onError: (err) => refreshFeaturesOnLockedError(qc, wsId, err),
    onSettled: async () => {
      await qc.invalidateQueries({ queryKey: projectKeys.all(wsId) });
      await qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
    },
  });
}

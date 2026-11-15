import { onlineManager, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { projectKeys } from "./queries";
import { issueKeys } from "../issues/queries";
import type { Project, ProjectVisibility } from "../types";

// Mirrors the server: owners/admins, or the member lead of a private project.
// Anyone may set the lead of a workspace-visible project, so leading one
// grants nothing; making it private takes an owner/admin.
export function canManageProjectAccess(project: Pick<Project, "lead_type" | "lead_id" | "visibility">, userId: string | null | undefined, role: string | null | undefined): boolean {
  if (!userId) return false;
  if (role === "owner" || role === "admin") return true;
  return project.visibility === "private" && project.lead_type === "member" && project.lead_id === userId;
}

// The lead of a private project manages its access, so only access managers
// may change it there; elsewhere the lead stays an ordinary field.
export function canChangeProjectLead(project: Pick<Project, "lead_type" | "lead_id" | "visibility">, userId: string | null | undefined, role: string | null | undefined): boolean {
  return project.visibility !== "private" || canManageProjectAccess(project, userId, role);
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
    onSettled: async () => {
      await qc.invalidateQueries({ queryKey: projectKeys.all(wsId) });
      await qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
    },
  });
}

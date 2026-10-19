"use client";

import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions } from "@multica/core/workspace/queries";
import type { Project } from "@multica/core/types";

/**
 * Who may change what in project workflows (MUL-7420), mirroring the server:
 * only admins edit workflows and their handoffs; admins and a project's member
 * lead switch the workflow the project uses.
 */
export function useWorkflowPermissions(project?: Pick<Project, "lead_type" | "lead_id"> | null): {
  isAdmin: boolean;
  canManageProject: boolean;
} {
  const wsId = useWorkspaceId();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const userId = useAuthStore((s) => s.user?.id);
  const role = members.find((m) => m.user_id === userId)?.role;
  const isAdmin = role === "owner" || role === "admin";
  const isLead = !!userId && project?.lead_type === "member" && project.lead_id === userId;
  return { isAdmin, canManageProject: isAdmin || isLead };
}

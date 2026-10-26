"use client";

import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions } from "@multica/core/workspace/queries";
import type { IssueWorkflow, Project } from "@multica/core/types";

/**
 * Who may change what in project workflows, mirroring the server: any member
 * creates workflows; a workflow's owner and workspace admins edit it and its
 * handoffs (MUL-8120); admins and a project's member lead switch the workflow
 * the project uses. Default has no owner, so only admins edit it.
 */
export function useWorkflowPermissions(
  project?: Pick<Project, "lead_type" | "lead_id"> | null,
  workflow?: Pick<IssueWorkflow, "owner_id"> | null,
): {
  isAdmin: boolean;
  canCreateWorkflow: boolean;
  canEditWorkflow: boolean;
  canManageProject: boolean;
} {
  const wsId = useWorkspaceId();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const userId = useAuthStore((s) => s.user?.id);
  const role = members.find((m) => m.user_id === userId)?.role;
  const isAdmin = role === "owner" || role === "admin";
  const isOwner = !!userId && !!workflow?.owner_id && workflow.owner_id === userId;
  const isLead = !!userId && project?.lead_type === "member" && project.lead_id === userId;
  return {
    isAdmin,
    canCreateWorkflow: !!role,
    canEditWorkflow: isAdmin || isOwner,
    canManageProject: isAdmin || isLead,
  };
}

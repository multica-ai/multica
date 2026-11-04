"use client";

import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions } from "@multica/core/workspace/queries";

/** Whether the viewer is an owner or admin of the workspace: the boss, who arranges floors and seats. */
export function useCompanyAdmin(): boolean {
  const wsId = useWorkspaceId();
  const userId = useAuthStore((s) => s.user?.id);
  const { data: members } = useQuery(memberListOptions(wsId));
  const role = members?.find((member) => member.user_id === userId)?.role;
  return role === "owner" || role === "admin";
}

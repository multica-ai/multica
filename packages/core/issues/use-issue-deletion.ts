"use client";

import { useQuery } from "@tanstack/react-query";
import { useCurrentMember } from "../permissions/use-current-member";
import { agentListOptions } from "../workspace/queries";
import type { Issue } from "../types";
import { canDeleteIssue } from "./deletion-permissions";

export function useIssueDeletion(wsId: string) {
  const { userId, role } = useCurrentMember(wsId);
  const { data: agents = [] } = useQuery({
    ...agentListOptions(wsId),
    enabled: role === "member",
  });
  return {
    canDelete: (issue: Issue) => canDeleteIssue(issue, userId, role, agents),
  };
}

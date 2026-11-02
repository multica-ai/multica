"use client";

import { useQuery } from "@tanstack/react-query";
import { useCurrentMember } from "../permissions/use-current-member";
import { agentListOptions, workspaceListOptions } from "../workspace/queries";
import type { Issue } from "../types";
import { canDeleteIssue } from "./deletion-permissions";

export function useIssueDeletion(wsId: string) {
  const { data: workspaces } = useQuery(workspaceListOptions());
  const workspace = workspaces?.find((item) => item.id === wsId);
  const restricted = workspace?.restrict_issue_deletion === true;
  const { userId, role } = useCurrentMember(wsId);
  const { data: agents = [] } = useQuery({
    ...agentListOptions(wsId),
    enabled: restricted && role === "member",
  });
  return {
    restricted,
    canDelete: (issue: Issue) =>
      !!workspace && canDeleteIssue(issue, restricted, userId, role, agents),
  };
}

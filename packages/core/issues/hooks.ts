"use client";

import { useQuery } from "@tanstack/react-query";
import { issueListOptions } from "./queries";

export function useIssueList(workspaceId: string) {
  return useQuery(issueListOptions(workspaceId));
}

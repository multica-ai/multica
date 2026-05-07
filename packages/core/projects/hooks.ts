"use client";

import { useQuery } from "@tanstack/react-query";
import { projectListOptions } from "./queries";

export function useProjectList(workspaceId: string) {
  return useQuery(projectListOptions(workspaceId));
}

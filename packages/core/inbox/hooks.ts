"use client";

import { useQuery } from "@tanstack/react-query";
import { inboxListOptions } from "./queries";

export function useInboxList(workspaceId: string) {
  return useQuery(inboxListOptions(workspaceId));
}

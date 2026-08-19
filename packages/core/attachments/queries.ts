import { infiniteQueryOptions } from "@tanstack/react-query";
import { api } from "../api";

const WORKSPACE_FILES_PAGE_SIZE = 50;

export const workspaceFileKeys = {
  all: (workspaceId: string) => ["attachments", "workspace", workspaceId] as const,
};

export function workspaceFilesOptions(workspaceId: string) {
  return infiniteQueryOptions({
    queryKey: workspaceFileKeys.all(workspaceId),
    queryFn: ({ pageParam }) =>
      api.listWorkspaceAttachments({
        limit: WORKSPACE_FILES_PAGE_SIZE,
        offset: pageParam,
      }),
    initialPageParam: 0,
    getNextPageParam: (lastPage) => lastPage.nextOffset ?? undefined,
    enabled: !!workspaceId,
  });
}

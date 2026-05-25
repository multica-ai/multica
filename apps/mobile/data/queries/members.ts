import { queryOptions } from "@tanstack/react-query";
import { api } from "@/data/api";

export const memberListOptions = (wsId: string | null) =>
  queryOptions({
    queryKey: ["members", wsId] as const,
    queryFn: ({ signal }) => api.listMembers(wsId!, { signal }),
    enabled: !!wsId,
  });

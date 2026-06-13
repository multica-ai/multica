import { queryOptions } from "@tanstack/react-query";
import { api } from "@/data/api";

export const assigneeFrequencyOptions = (wsId: string | null) =>
  queryOptions({
    queryKey: ["workspaces", wsId, "assignee-frequency"] as const,
    queryFn: ({ signal }) => api.getAssigneeFrequency({ signal }),
    enabled: !!wsId,
  });

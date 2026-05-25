import { queryOptions } from "@tanstack/react-query";
import { api } from "@/data/api";

export const squadListOptions = (wsId: string | null) =>
  queryOptions({
    queryKey: ["squads", wsId] as const,
    queryFn: ({ signal }) => api.listSquads({ signal }),
    enabled: !!wsId,
  });

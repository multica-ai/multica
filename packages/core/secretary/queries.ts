import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
export const secretaryKeys = { all: (workspaceId: string) => ["secretary", workspaceId] as const };
export const secretaryOptions = (workspaceId: string) => queryOptions({
  queryKey: secretaryKeys.all(workspaceId), queryFn: () => api.getSecretary(),
  refetchInterval: 30_000,
});

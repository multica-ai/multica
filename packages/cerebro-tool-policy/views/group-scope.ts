import { useQuery } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import type { ScopeOption } from "./data-source-scope";

export const GROUP_ARG = "group_id";

export function mapCerebroGroupsToScopeOptions(raw: unknown[]): ScopeOption[] {
  return (raw as Array<Record<string, unknown>>)
    .map((g) => {
      const id = String(g.id ?? "");
      const name = typeof g.name === "string" && g.name ? g.name : id;
      return { id, name };
    })
    .filter((o) => o.id);
}

export function useGroupScopeOptions(
  wsId: string,
  enabled: boolean,
): { options: ScopeOption[]; loading: boolean; error: boolean } {
  const { data, isLoading, isError } = useQuery({
    queryKey: ["tool-policy", "group-scope-options", wsId],
    queryFn: async (): Promise<ScopeOption[]> => {
      const raw = await api.listCerebroGroups<unknown>(wsId);
      const list = Array.isArray(raw) ? raw : [];
      return mapCerebroGroupsToScopeOptions(list);
    },
    enabled: !!wsId && enabled,
    staleTime: 60_000,
  });
  return { options: data ?? [], loading: isLoading, error: isError };
}

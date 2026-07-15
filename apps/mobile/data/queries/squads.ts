import { queryOptions } from "@tanstack/react-query";
import { api } from "@/data/api";

/**
 * `undefined` means a Workspace-wide identity lookup (for example resolving
 * an already-rendered Squad avatar). `null` means the caller explicitly has
 * no Space context, so no Squad can be selected for new work.
 */
export const squadListOptions = (
  wsId: string | null,
  spaceId?: string | null,
) =>
  queryOptions({
    queryKey:
      typeof spaceId === "string"
        ? (["squads", wsId, "space", spaceId] as const)
        : spaceId === null
          ? (["squads", wsId, "no-space"] as const)
          : (["squads", wsId] as const),
    queryFn: ({ signal }) =>
      api.listSquads({
        signal,
        ...(typeof spaceId === "string" ? { spaceId } : {}),
      }),
    enabled: !!wsId && spaceId !== null,
  });

import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const runtimeKeys = {
  all: (wsId: string) => ["runtimes", wsId] as const,
  list: (wsId: string) => [...runtimeKeys.all(wsId), "list"] as const,
  listMine: (wsId: string) => [...runtimeKeys.all(wsId), "list", "mine"] as const,
  latestVersion: () => ["runtimes", "latestVersion"] as const,
};

export function runtimeListOptions(wsId: string, owner?: "me") {
  return queryOptions({
    queryKey: owner === "me" ? runtimeKeys.listMine(wsId) : runtimeKeys.list(wsId),
    queryFn: () => api.listRuntimes({ workspace_id: wsId, owner }),
  });
}

const GITHUB_RELEASES_URL =
  "https://api.github.com/repos/multica-ai/multica/releases/latest";

export function latestCliVersionOptions() {
  return queryOptions({
    queryKey: runtimeKeys.latestVersion(),
    queryFn: async (): Promise<string | null> => {
      try {
        const resp = await fetch(GITHUB_RELEASES_URL, {
          headers: { Accept: "application/vnd.github+json" },
        });
        if (!resp.ok) return null;
        const data = await resp.json();
        return (data.tag_name as string) ?? null;
      } catch {
        return null;
      }
    },
    staleTime: 10 * 60 * 1000, // 10 minutes
  });
}

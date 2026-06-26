// data-source-scope.ts — the picker data for the WHEN-layer argument-value
// allowlist (FIR-2083). A connection declares a "scopable argument" (which tool
// argument is the scoping axis + which tool fills the choices). For the registry
// that is `query_run · data_source_id` filled by `data_sources_list`. The When
// popover on a firtal_registry rule reads this to render a search + multi-select
// grouped per folder, writing a `data_source_id` (specific sources) or
// `folder_id` (a whole folder, auto-covering future sources) arg-allowlist.
//
// This lives in the tool-policy package and talks to the connections API through
// the shared client, so the package needs no dependency on cerebro-connections.

import { useQuery } from "@tanstack/react-query";
import { api } from "@multica/core/api";

// The registry's scoping axis. The gate threads `data_source_id` (and resolves
// its `folder_id`) into the request context, so these are the two arg names a
// registry scope rule may use. Folder mode is what makes "Finance · all"
// auto-cover a source added to the folder later.
export const DATA_SOURCE_ARG = "data_source_id";
export const FOLDER_ARG = "folder_id";

export interface ScopeOption {
  id: string;
  name: string;
  folder?: string;
  folderId?: string;
  tags?: string[];
}

// ScopeConfig is the cheap, always-available binding derived from the workspace
// connections: which connection + tool fills the picker for a scoping arg.
export interface ScopeConfig {
  connectionId: string;
  optionsSourceTool: string;
  label: string;
}

// useDataSourceScopeConfig finds the connection that declares `data_source_id`
// scoping (the registry, in v1) so the When popover knows where to fetch options.
// Cheap: one connections list call, shared/cached across the table.
export function useDataSourceScopeConfig(wsId: string): ScopeConfig | null {
  const { data } = useQuery({
    queryKey: ["tool-policy", "scope-config", wsId],
    queryFn: async (): Promise<ScopeConfig | null> => {
      const raw = await api.listCerebroConnections<{ connections?: unknown[] }>(
        wsId,
      );
      const conns = Array.isArray(raw?.connections) ? raw.connections : [];
      for (const c of conns as Array<Record<string, unknown>>) {
        if (c?.enabled === false) continue;
        const args = Array.isArray(c?.scopable_args) ? c.scopable_args : [];
        for (const a of args as Array<Record<string, unknown>>) {
          if (a?.arg === DATA_SOURCE_ARG && typeof a?.options_source_tool === "string" && a.options_source_tool) {
            return {
              connectionId: String(c.id ?? ""),
              optionsSourceTool: a.options_source_tool,
              label: typeof a.label === "string" && a.label ? a.label : "Data sources",
            };
          }
        }
      }
      return null;
    },
    enabled: !!wsId,
    staleTime: 60_000,
  });
  return data ?? null;
}

// useScopeOptions lazily fetches the picker choices for a scope config. Enabled
// is flipped on when the popover opens, so the registry round-trip happens only
// when an admin actually edits a scope rule.
export function useScopeOptions(
  wsId: string,
  config: ScopeConfig | null,
  enabled: boolean,
): { options: ScopeOption[]; loading: boolean; error: boolean } {
  const { data, isLoading, isError } = useQuery({
    queryKey: ["tool-policy", "scope-options", wsId, config?.connectionId, config?.optionsSourceTool],
    queryFn: async (): Promise<ScopeOption[]> => {
      const raw = await api.getCerebroConnectionOptions<{ options?: unknown[] }>(
        wsId,
        config!.connectionId,
        config!.optionsSourceTool,
      );
      const list = Array.isArray(raw?.options) ? raw.options : [];
      return (list as Array<Record<string, unknown>>)
        .map((o) => ({
          id: String(o.id ?? ""),
          name: String(o.name ?? o.id ?? ""),
          folder: typeof o.folder === "string" ? o.folder : undefined,
          folderId: typeof o.folder_id === "string" ? o.folder_id : undefined,
          tags: Array.isArray(o.tags) ? (o.tags as unknown[]).filter((t): t is string => typeof t === "string") : undefined,
        }))
        .filter((o) => o.id);
    },
    enabled: !!wsId && !!config?.connectionId && !!config?.optionsSourceTool && enabled,
    staleTime: 60_000,
  });
  return { options: data ?? [], loading: isLoading, error: isError };
}

// FolderGroup groups scope options by their folder for the grouped picker. The
// "(no folder)" bucket has an empty folderId, which has no auto-cover meaning, so
// the UI offers folder-level "select all" only on real folders.
export interface FolderGroup {
  folderId: string;
  folder: string;
  options: ScopeOption[];
}

export function groupByFolder(options: ScopeOption[]): FolderGroup[] {
  const byId = new Map<string, FolderGroup>();
  for (const o of options) {
    const folderId = o.folderId ?? "";
    const folder = o.folder ?? "Ungrouped";
    const key = folderId || folder;
    const group = byId.get(key) ?? { folderId, folder, options: [] };
    group.options.push(o);
    byId.set(key, group);
  }
  return [...byId.values()].sort((a, b) => a.folder.localeCompare(b.folder));
}

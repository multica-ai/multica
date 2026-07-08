/**
 * Mobile-owned WS cache patchers for the project domain. Pure functions over
 * `QueryClient` — no React, no WS plumbing. Hooks in `use-projects-realtime.ts`
 * and `use-project-realtime.ts` translate WS events into calls into this module.
 *
 * Why mobile-owned (and not importing from packages/core/projects):
 *   - Web doesn't have project ws-updaters yet — it invalidates via the
 *     query cache mutation surface. Mobile must patch (cellular-data rule
 *     in apps/mobile/CLAUDE.md realtime § "Patch over invalidate").
 *   - Even when web adds them, mobile keys come from its own
 *     `data/queries/projects.ts` factory; binding to a foreign factory
 *     would silently drift on key-shape changes.
 *
 * Cache shapes:
 *   - Project list    (projectKeys.list)         → `Project[]`
 *   - Project detail  (projectKeys.detail)       → `Project`
 *   - Resources       (projectKeys.resources)    → `ProjectResource[]`
 */
import type { QueryClient } from "@tanstack/react-query";
import type { Project } from "@multica/core/types";
import { projectKeys } from "@/data/queries/projects";

export function patchProjectsList(
  qc: QueryClient,
  wsId: string,
  partial: Partial<Project> & { id: string },
) {
  qc.setQueryData<Project[]>(projectKeys.list(wsId), (old) =>
    old
      ? old.map((p) => (p.id === partial.id ? { ...p, ...partial } : p))
      : old,
  );
}

/** Prepend if not present, replace in place if it is. List ordering is
 *  server-driven; on `project:created` the list will resync to the
 *  authoritative order via the next refetch / reconnect. */
export function upsertIntoProjectsList(
  qc: QueryClient,
  wsId: string,
  project: Project,
) {
  qc.setQueryData<Project[]>(projectKeys.list(wsId), (old) => {
    if (!old) return [project];
    const idx = old.findIndex((p) => p.id === project.id);
    if (idx === -1) return [project, ...old];
    const copy = old.slice();
    copy[idx] = project;
    return copy;
  });
}

export function removeFromProjectsList(
  qc: QueryClient,
  wsId: string,
  projectId: string,
) {
  qc.setQueryData<Project[]>(projectKeys.list(wsId), (old) =>
    old ? old.filter((p) => p.id !== projectId) : old,
  );
}

export function patchProjectDetail(
  qc: QueryClient,
  wsId: string,
  project: Project,
) {
  // Full replace — payload carries the authoritative Project. We don't merge
  // because server can clear nullable fields (description / lead) which a
  // partial spread would erase silently if the payload omitted the key.
  qc.setQueryData<Project>(projectKeys.detail(wsId, project.id), project);
}

export function clearProjectDetail(
  qc: QueryClient,
  wsId: string,
  projectId: string,
) {
  qc.removeQueries({ queryKey: projectKeys.detail(wsId, projectId) });
  qc.removeQueries({ queryKey: projectKeys.resources(wsId, projectId) });
}

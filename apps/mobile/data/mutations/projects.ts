/**
 * Project mutations. Mirrors the optimistic-patch + event-always-wins pattern
 * of `useUpdateIssue` (data/mutations/issues.ts:276): apply the patch to
 * both list and detail caches up-front, server response or WS event later
 * overwrites with authoritative state.
 *
 * Cache shapes touched:
 *   - projectKeys.list(wsId)      → `Project[]`     (patch in place)
 *   - projectKeys.detail(wsId,id) → `Project`       (replace fully)
 *   - projectKeys.resources(...)  → `ProjectResource[]` (append / filter)
 *
 * No realtime-driven `project:*` updaters exist on web yet (see
 * apps/mobile/CLAUDE.md realtime section) so mobile mirrors the design
 * — mobile-owned ws-updaters live in `data/realtime/project-ws-updaters.ts`
 * and are invoked by `use-projects-realtime.ts` + `use-project-realtime.ts`.
 */
import { useMutation, useQueryClient } from "@tanstack/react-query";
import type {
  CreateProjectRequest,
  CreateProjectResourceRequest,
  Project,
  ProjectResource,
  UpdateProjectRequest,
} from "@multica/core/types";
import { api } from "@/data/api";
import { projectKeys } from "@/data/queries/projects";
import { useWorkspaceStore } from "@/data/workspace-store";

export function useCreateProject() {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  return useMutation({
    mutationFn: (body: CreateProjectRequest) => api.createProject(body),
    onSuccess: (project) => {
      // Seed the detail cache so the post-create navigation lands on a
      // populated page (no spinner flash). The list cache gets a prepend
      // — list ordering is server-driven, so a brief out-of-order render
      // is acceptable and corrected by the WS `project:created` event
      // (or the next refetch).
      qc.setQueryData<Project>(projectKeys.detail(wsId, project.id), project);
      qc.setQueryData<Project[]>(projectKeys.list(wsId), (old) =>
        old ? [project, ...old.filter((p) => p.id !== project.id)] : [project],
      );
    },
  });
}

export function useUpdateProject(projectId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  return useMutation({
    mutationKey: ["updateProject", projectId] as const,
    mutationFn: (patch: UpdateProjectRequest) =>
      api.updateProject(projectId, patch),
    onMutate: async (patch) => {
      const detailKey = projectKeys.detail(wsId, projectId);
      const listKey = projectKeys.list(wsId);
      // Cancel both — a concurrent list refetch can race-overwrite the
      // optimistic patch otherwise (brief stale flash on screen).
      await Promise.all([
        qc.cancelQueries({ queryKey: detailKey }),
        qc.cancelQueries({ queryKey: listKey }),
      ]);

      const prevDetail = qc.getQueryData<Project>(detailKey);
      const prevList = qc.getQueryData<Project[]>(listKey);

      if (prevDetail) {
        qc.setQueryData<Project>(detailKey, { ...prevDetail, ...patch });
      }
      qc.setQueryData<Project[]>(listKey, (old) =>
        old
          ? old.map((p) => (p.id === projectId ? { ...p, ...patch } : p))
          : old,
      );

      return { prevDetail, prevList, detailKey, listKey };
    },
    onError: (_err, _vars, ctx) => {
      if (!ctx) return;
      if (ctx.prevDetail !== undefined) {
        qc.setQueryData(ctx.detailKey, ctx.prevDetail);
      }
      if (ctx.prevList !== undefined) {
        qc.setQueryData(ctx.listKey, ctx.prevList);
      }
    },
    onSuccess: (server) => {
      // Server response is authoritative — replace the optimistic merge
      // so any server-side normalisation (e.g. trimmed title) wins.
      qc.setQueryData<Project>(projectKeys.detail(wsId, projectId), server);
      qc.setQueryData<Project[]>(projectKeys.list(wsId), (old) =>
        old
          ? old.map((p) => (p.id === projectId ? server : p))
          : old,
      );
    },
  });
}

export function useDeleteProject(projectId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  return useMutation({
    mutationKey: ["deleteProject", projectId] as const,
    mutationFn: () => api.deleteProject(projectId),
    onMutate: async () => {
      const listKey = projectKeys.list(wsId);
      await qc.cancelQueries({ queryKey: listKey });
      const prevList = qc.getQueryData<Project[]>(listKey);
      qc.setQueryData<Project[]>(listKey, (old) =>
        old ? old.filter((p) => p.id !== projectId) : old,
      );
      return { prevList, listKey };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prevList !== undefined) {
        qc.setQueryData(ctx.listKey, ctx.prevList);
      }
    },
    onSettled: () => {
      qc.removeQueries({ queryKey: projectKeys.detail(wsId, projectId) });
      qc.removeQueries({ queryKey: projectKeys.resources(wsId, projectId) });
    },
  });
}

export function useCreateProjectResource(projectId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  return useMutation({
    mutationKey: ["createProjectResource", projectId] as const,
    mutationFn: (body: CreateProjectResourceRequest) =>
      api.createProjectResource(projectId, body),
    onSuccess: (resource) => {
      qc.setQueryData<ProjectResource[]>(
        projectKeys.resources(wsId, projectId),
        (old) =>
          old
            ? [...old.filter((r) => r.id !== resource.id), resource]
            : [resource],
      );
      // Bump the parent's resource_count so the chip on detail/list
      // increments without a refetch.
      const bumpCount = (p: Project): Project => ({
        ...p,
        resource_count: p.resource_count + 1,
      });
      qc.setQueryData<Project>(
        projectKeys.detail(wsId, projectId),
        (old) => (old ? bumpCount(old) : old),
      );
      qc.setQueryData<Project[]>(projectKeys.list(wsId), (old) =>
        old
          ? old.map((p) => (p.id === projectId ? bumpCount(p) : p))
          : old,
      );
    },
  });
}

export function useDeleteProjectResource(projectId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  return useMutation({
    mutationKey: ["deleteProjectResource", projectId] as const,
    mutationFn: (resourceId: string) =>
      api.deleteProjectResource(projectId, resourceId).then(() => resourceId),
    onMutate: async (resourceId) => {
      const key = projectKeys.resources(wsId, projectId);
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<ProjectResource[]>(key);
      qc.setQueryData<ProjectResource[]>(key, (old) =>
        old ? old.filter((r) => r.id !== resourceId) : old,
      );
      return { prev, key };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev !== undefined) {
        qc.setQueryData(ctx.key, ctx.prev);
      }
    },
    onSuccess: () => {
      const dropCount = (p: Project): Project => ({
        ...p,
        resource_count: Math.max(0, p.resource_count - 1),
      });
      qc.setQueryData<Project>(
        projectKeys.detail(wsId, projectId),
        (old) => (old ? dropCount(old) : old),
      );
      qc.setQueryData<Project[]>(projectKeys.list(wsId), (old) =>
        old
          ? old.map((p) => (p.id === projectId ? dropCount(p) : p))
          : old,
      );
    },
  });
}

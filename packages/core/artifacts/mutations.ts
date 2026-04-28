import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import { artifactKeys } from "./queries";
import type {
  Artifact,
  CreateArtifactRequest,
  UpdateArtifactRequest,
  UpdateArtifactScopeRequest,
} from "../types";

function invalidateArtifactScopes(
  qc: ReturnType<typeof useQueryClient>,
  wsId: string,
  artifact: Pick<Artifact, "id" | "issue_id" | "project_id">,
) {
  qc.invalidateQueries({ queryKey: artifactKeys.detail(wsId, artifact.id) });
  if (artifact.issue_id) {
    qc.invalidateQueries({
      queryKey: artifactKeys.byIssue(wsId, artifact.issue_id),
    });
  }
  if (artifact.project_id) {
    qc.invalidateQueries({
      queryKey: artifactKeys.byProject(wsId, artifact.project_id),
    });
  }
}

export function useCreateArtifact() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: CreateArtifactRequest) => api.createArtifact(data),
    onSuccess: (artifact) => {
      if (artifact.issue_id) {
        qc.setQueryData<Artifact[]>(
          artifactKeys.byIssue(wsId, artifact.issue_id),
          (old) => (old ? [artifact, ...old] : [artifact]),
        );
      }
      if (artifact.project_id) {
        qc.setQueryData<Artifact[]>(
          artifactKeys.byProject(wsId, artifact.project_id),
          (old) => (old ? [artifact, ...old] : [artifact]),
        );
      }
      qc.setQueryData(artifactKeys.detail(wsId, artifact.id), artifact);
    },
    onSettled: (artifact) => {
      if (!artifact) return;
      invalidateArtifactScopes(qc, wsId, artifact);
    },
  });
}

export function useUpdateArtifact() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, data }: { id: string; data: UpdateArtifactRequest }) =>
      api.updateArtifact(id, data),
    onMutate: async ({ id, data }) => {
      await qc.cancelQueries({ queryKey: artifactKeys.detail(wsId, id) });
      const prev = qc.getQueryData<Artifact>(artifactKeys.detail(wsId, id));
      if (prev) {
        const next: Artifact = {
          ...prev,
          title: data.title ?? prev.title,
          body: data.body ?? prev.body,
          metadata: data.metadata ?? prev.metadata,
          updated_at: new Date().toISOString(),
        };
        qc.setQueryData(artifactKeys.detail(wsId, id), next);
      }
      return { prev };
    },
    onError: (_err, { id }, ctx) => {
      if (ctx?.prev) qc.setQueryData(artifactKeys.detail(wsId, id), ctx.prev);
    },
    onSuccess: (artifact) => {
      qc.setQueryData(artifactKeys.detail(wsId, artifact.id), artifact);
      invalidateArtifactScopes(qc, wsId, artifact);
    },
  });
}

export function useMoveArtifact() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({
      artifact,
      data,
    }: {
      artifact: Pick<Artifact, "id" | "issue_id" | "project_id">;
      data: UpdateArtifactScopeRequest;
    }) => api.updateArtifactScope(artifact.id, data),
    onSuccess: (updated, { artifact: previous }) => {
      qc.setQueryData(artifactKeys.detail(wsId, updated.id), updated);
      // Invalidate both old and new scopes so they refetch their lists.
      invalidateArtifactScopes(qc, wsId, previous);
      invalidateArtifactScopes(qc, wsId, updated);
      // Workspace search cache spans all scopes.
      qc.invalidateQueries({ queryKey: artifactKeys.all(wsId) });
    },
  });
}

export function useDeleteArtifact() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (artifact: Pick<Artifact, "id" | "issue_id" | "project_id">) =>
      api.deleteArtifact(artifact.id),
    onMutate: async (artifact) => {
      const filterOut = (list: Artifact[] | undefined) =>
        list ? list.filter((a) => a.id !== artifact.id) : list;
      if (artifact.issue_id) {
        await qc.cancelQueries({
          queryKey: artifactKeys.byIssue(wsId, artifact.issue_id),
        });
        qc.setQueryData<Artifact[]>(
          artifactKeys.byIssue(wsId, artifact.issue_id),
          filterOut,
        );
      }
      if (artifact.project_id) {
        await qc.cancelQueries({
          queryKey: artifactKeys.byProject(wsId, artifact.project_id),
        });
        qc.setQueryData<Artifact[]>(
          artifactKeys.byProject(wsId, artifact.project_id),
          filterOut,
        );
      }
      qc.removeQueries({ queryKey: artifactKeys.detail(wsId, artifact.id) });
    },
    onSettled: (_data, _err, artifact) => {
      invalidateArtifactScopes(qc, wsId, artifact);
    },
  });
}

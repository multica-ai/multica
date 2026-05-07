import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { artifactKeys } from "./queries";
import type {
  Artifact,
  ArtifactFolder,
  CreateArtifactRequest,
  CreateArtifactFolderRequest,
  UpdateArtifactRequest,
  UpdateArtifactScopeRequest,
  UpdateArtifactFolderRequest,
  MoveArtifactToFolderRequest,
} from "@multica/core/types";

function invalidateArtifactScopes(
  qc: ReturnType<typeof useQueryClient>,
  wsId: string,
  // The artifact arg is kept for API symmetry with prior callers but is no
  // longer needed — invalidating the workspace-wide artifact key catches
  // detail, byIssue, byProject, search, and folder-list together.
  _artifact?: Pick<Artifact, "id" | "issue_id" | "project_id">,
) {
  qc.invalidateQueries({ queryKey: artifactKeys.all(wsId) });
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
          file_url: data.file_url === undefined ? prev.file_url : data.file_url,
          file_size_bytes:
            data.file_size_bytes === undefined ? prev.file_size_bytes : data.file_size_bytes,
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

export function useMoveArtifactToFolder() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({
      id,
      data,
    }: {
      id: string;
      data: MoveArtifactToFolderRequest;
    }) => api.moveArtifactToFolder(id, data),
    onSuccess: (artifact) => {
      qc.setQueryData(artifactKeys.detail(wsId, artifact.id), artifact);
      // Folder lists are derived from search results; invalidate the
      // workspace-wide search cache so the new and old folder views refetch.
      qc.invalidateQueries({ queryKey: artifactKeys.all(wsId) });
    },
  });
}

export function useUploadArtifactFile() {
  return useMutation({
    mutationFn: (file: File) => api.uploadArtifactFile(file),
  });
}

// ---------------------------------------------------------------------------
// Folder mutations
// ---------------------------------------------------------------------------

export function useCreateArtifactFolder() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: CreateArtifactFolderRequest) =>
      api.createArtifactFolder(data),
    onSuccess: (folder) => {
      qc.setQueryData<ArtifactFolder[]>(
        artifactKeys.folders(wsId),
        (old) => (old ? [...old, folder] : [folder]),
      );
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: artifactKeys.folders(wsId) });
    },
  });
}

export function useUpdateArtifactFolder() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({
      id,
      data,
    }: {
      id: string;
      data: UpdateArtifactFolderRequest;
    }) => api.updateArtifactFolder(id, data),
    onSuccess: (folder) => {
      qc.setQueryData<ArtifactFolder[]>(artifactKeys.folders(wsId), (old) =>
        old ? old.map((f) => (f.id === folder.id ? folder : f)) : old,
      );
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: artifactKeys.folders(wsId) });
    },
  });
}

export function useDeleteArtifactFolder() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (folder: Pick<ArtifactFolder, "id">) =>
      api.deleteArtifactFolder(folder.id),
    onMutate: async (folder) => {
      await qc.cancelQueries({ queryKey: artifactKeys.folders(wsId) });
      const prev = qc.getQueryData<ArtifactFolder[]>(artifactKeys.folders(wsId));
      qc.setQueryData<ArtifactFolder[]>(artifactKeys.folders(wsId), (old) =>
        old ? old.filter((f) => f.id !== folder.id) : old,
      );
      return { prev };
    },
    onError: (_err, _folder, ctx) => {
      if (ctx?.prev) qc.setQueryData(artifactKeys.folders(wsId), ctx.prev);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: artifactKeys.folders(wsId) });
      // Artifacts inside the folder reset to root via SET NULL; refresh lists.
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

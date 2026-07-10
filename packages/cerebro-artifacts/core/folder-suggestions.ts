// CEREBRO-PATCH(folder-suggestion): FIR-2697 part 2 — folder suggestions with
// human accept. An agent proposes an existing folder for a document/note (via
// the suggest_artifact_folder MCP tool); a person sees the proposal as a banner
// in the editor and accepts or rejects it. The artifact only moves on accept.
// Notes and documents keep separate folder trees (surface), so the banner reads
// the proposal per artifact and the accept path enforces the match server-side.
import {
  queryOptions,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import type { ArtifactFolderSuggestion } from "@multica/core/types";
import { artifactKeys } from "./queries";

export const folderSuggestionKeys = {
  all: (wsId: string) => ["artifact-folder-suggestions", wsId] as const,
  forArtifact: (wsId: string, artifactId: string) =>
    [...folderSuggestionKeys.all(wsId), "for-artifact", artifactId] as const,
  list: (wsId: string, surface?: string) =>
    [...folderSuggestionKeys.all(wsId), "list", surface ?? "all"] as const,
};

// The pending proposal (if any) for a single artifact. Returns null when there
// is none, the feature is off, or the caller cannot see the target folder.
export function folderSuggestionForArtifactOptions(
  wsId: string,
  artifactId: string,
  enabled = true,
) {
  return queryOptions({
    queryKey: folderSuggestionKeys.forArtifact(wsId, artifactId),
    queryFn: async () =>
      (await api.getArtifactFolderSuggestion(artifactId)).suggestion,
    enabled: Boolean(enabled && wsId && artifactId),
  });
}

export function useFolderSuggestionForArtifact(
  artifactId: string,
  enabled = true,
) {
  const wsId = useWorkspaceId();
  return useQuery(folderSuggestionForArtifactOptions(wsId, artifactId, enabled));
}

// The review inbox: every live proposal in the workspace, optionally scoped to
// one surface (document | note).
export function folderSuggestionListOptions(
  wsId: string,
  surface?: string,
  enabled = true,
) {
  return queryOptions({
    queryKey: folderSuggestionKeys.list(wsId, surface),
    queryFn: () => api.listArtifactFolderSuggestions(surface),
    enabled: Boolean(enabled && wsId),
  });
}

// Accept moves the artifact into the proposed folder; reject leaves it in place.
// Both invalidate the artifact caches (the doc's folder changed) and the
// suggestion caches (the banner disappears).
function useResolveFolderSuggestion(
  action: (id: string) => Promise<ArtifactFolderSuggestion>,
) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (suggestion: Pick<ArtifactFolderSuggestion, "id" | "artifact_id">) =>
      action(suggestion.id),
    onSuccess: (resolved) => {
      qc.invalidateQueries({ queryKey: folderSuggestionKeys.all(wsId) });
      // The artifact's folder may have changed — refresh its detail + lists.
      qc.invalidateQueries({
        queryKey: artifactKeys.detail(wsId, resolved.artifact_id),
      });
      qc.invalidateQueries({ queryKey: artifactKeys.all(wsId) });
    },
  });
}

export function useAcceptFolderSuggestion() {
  return useResolveFolderSuggestion((id) =>
    api.acceptArtifactFolderSuggestion(id),
  );
}

export function useRejectFolderSuggestion() {
  return useResolveFolderSuggestion((id) =>
    api.rejectArtifactFolderSuggestion(id),
  );
}

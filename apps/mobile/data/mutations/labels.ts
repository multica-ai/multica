/**
 * Mobile-side label mutations. Mirrors the design of
 * `packages/core/labels/mutations.ts` but binds to mobile's own ApiClient
 * (`@/data/api`) and workspace store — the core hook depends on
 * `useWorkspaceId` from `packages/core/hooks` which mobile does not share.
 */
import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { CreateLabelRequest, Label } from "@multica/core/types";
import { api } from "@/data/api";
import { labelKeys } from "@/data/queries/labels";
import { useWorkspaceStore } from "@/data/workspace-store";

export function useCreateLabel() {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  return useMutation({
    mutationFn: (body: CreateLabelRequest) => api.createLabel(body),
    onSuccess: (label) => {
      // Append to the workspace label list cache so the picker sees the
      // new label without waiting for a refetch. labelListOptions stores
      // a flat Label[] (unwrapped from the API response envelope) at
      // `labelKeys.all(wsId)` — match that shape here.
      qc.setQueryData<Label[]>(labelKeys.all(wsId), (old) =>
        old && !old.some((l) => l.id === label.id) ? [...old, label] : old,
      );
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: labelKeys.all(wsId) });
    },
  });
}

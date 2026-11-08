import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import type { WorkspaceLinkInput } from "../types/workspace-link";
import { linkKeys } from "./queries";

export function useCreateLink() {
  const qc = useQueryClient(); const wsId = useWorkspaceId();
  return useMutation({ mutationFn: (data: WorkspaceLinkInput) => api.createWorkspaceLink(data), onSettled: () => { qc.invalidateQueries({queryKey: linkKeys.all(wsId)}); } });
}
export function useUpdateLink() {
  const qc = useQueryClient(); const wsId = useWorkspaceId();
  return useMutation({ mutationFn: ({id, ...data}: {id: string} & Partial<WorkspaceLinkInput>) => api.updateWorkspaceLink(id, data), onSettled: () => { qc.invalidateQueries({queryKey: linkKeys.all(wsId)}); } });
}
export function useDeleteLink() {
  const qc = useQueryClient(); const wsId = useWorkspaceId();
  return useMutation({ mutationFn: (id: string) => api.deleteWorkspaceLink(id), onSettled: () => { qc.invalidateQueries({queryKey: linkKeys.all(wsId)}); } });
}

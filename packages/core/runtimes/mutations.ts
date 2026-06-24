import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type {
  CreateRuntimePermissionRequest,
  UpdateRuntimePermissionRequest,
} from "../types";
import { runtimeKeys } from "./queries";

export function useDeleteRuntime(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (runtimeId: string) => api.deleteRuntime(runtimeId),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) });
    },
  });
}

// useUpdateRuntime patches editable fields on a runtime (visibility).
// Invalidates the runtime list so the picker disabled-state recomputes.
export function useUpdateRuntime(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      runtimeId,
      patch,
    }: {
      runtimeId: string;
      patch: { visibility?: "private" | "public" };
    }) => api.updateRuntime(runtimeId, patch),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) });
    },
  });
}

export function useCreateRuntimePermission(runtimeId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: CreateRuntimePermissionRequest) =>
      api.createRuntimePermission(runtimeId, req),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: runtimeKeys.permissions(runtimeId) });
      qc.invalidateQueries({ queryKey: runtimeKeys.myPermission(runtimeId) });
    },
  });
}

export function useUpdateRuntimePermission(runtimeId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      userId,
      req,
    }: {
      userId: string;
      req: UpdateRuntimePermissionRequest;
    }) => api.updateRuntimePermission(runtimeId, userId, req),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: runtimeKeys.permissions(runtimeId) });
      qc.invalidateQueries({ queryKey: runtimeKeys.myPermission(runtimeId) });
    },
  });
}

export function useDeleteRuntimePermission(runtimeId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (userId: string) => api.deleteRuntimePermission(runtimeId, userId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: runtimeKeys.permissions(runtimeId) });
      qc.invalidateQueries({ queryKey: runtimeKeys.myPermission(runtimeId) });
    },
  });
}

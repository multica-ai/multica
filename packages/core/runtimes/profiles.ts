import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { ApiError } from "../api";
import type {
  CreateRuntimeProfileRequest,
  RuntimeProfile,
  UpdateRuntimeProfileRequest,
} from "../types/agent";
import { runtimeKeys } from "./queries";

// Query keys for the workspace-scoped custom runtime profile catalog. Kept
// separate from `runtimeKeys` (which key the registered runtime *instances*)
// because the two resources invalidate on different events — but a profile
// delete can archive bound agents and therefore must also invalidate the
// instance list, so the mutations below touch both.
export const runtimeProfileKeys = {
  all: (wsId: string) => ["runtime-profiles", wsId] as const,
  list: (wsId: string) => [...runtimeProfileKeys.all(wsId), "list"] as const,
  detail: (wsId: string, profileId: string) =>
    [...runtimeProfileKeys.all(wsId), "detail", profileId] as const,
};

export function runtimeProfileListOptions(wsId: string) {
  return queryOptions({
    queryKey: runtimeProfileKeys.list(wsId),
    queryFn: () => api.listRuntimeProfiles(wsId),
  });
}

export function useCreateRuntimeProfile(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: CreateRuntimeProfileRequest) =>
      api.createRuntimeProfile(wsId, body),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: runtimeProfileKeys.all(wsId) });
    },
  });
}

export function useUpdateRuntimeProfile(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      profileId,
      patch,
    }: {
      profileId: string;
      patch: UpdateRuntimeProfileRequest;
    }) => api.updateRuntimeProfile(wsId, profileId, patch),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: runtimeProfileKeys.all(wsId) });
      // A rename / visibility change can affect how the runtime list
      // labels bound instances; refresh that too.
      qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) });
    },
  });
}

export function useDeleteRuntimeProfile(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (profileId: string) => api.deleteRuntimeProfile(wsId, profileId),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: runtimeProfileKeys.all(wsId) });
      // The strict DELETE refuses (409) while agents are bound, but once it
      // succeeds the bound-instance picture may change — keep the runtime
      // list in sync.
      qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) });
    },
  });
}

// The server returns a 409 with a machine-readable code when a delete is
// refused because active agents are still bound to the profile. We surface
// the server's human-readable message verbatim so the confirm dialog can
// explain the refusal without re-deriving it. Non-409s and unrelated codes
// collapse to `null` so callers fall through to the generic error path.
export interface RuntimeProfileBoundConflict {
  message: string;
}

export function parseRuntimeProfileBoundConflict(
  err: unknown,
): RuntimeProfileBoundConflict | null {
  if (!(err instanceof ApiError)) return null;
  if (err.status !== 409) return null;
  const body = err.body;
  const fallback = err.message;
  if (body && typeof body === "object") {
    const record = body as Record<string, unknown>;
    const message =
      typeof record.message === "string" && record.message.trim()
        ? record.message
        : typeof record.error === "string" && record.error.trim()
          ? record.error
          : fallback;
    return { message };
  }
  return { message: fallback };
}

export type { RuntimeProfile };

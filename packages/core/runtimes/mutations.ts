import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type {
  UpdateRuntimeModelConnectionRequest,
  ValidateModelConnectionRequest,
} from "../types";
import { runtimeModelsKeys } from "./models";
import { runtimeKeys } from "./queries";
import { workspaceKeys } from "../workspace/queries";
import { agentTaskSnapshotKeys } from "../agents/queries";

export function useDeleteRuntime(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (runtimeId: string) => api.deleteRuntime(runtimeId),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) });
    },
  });
}

// Confirmed-delete counterpart to useDeleteRuntime. The dialog routes here when
// the strict DELETE refused with `runtime_has_active_agents` (or when the
// caller already knows the runtime has active agents and wants to skip the
// pre-flight refusal). Mutation fn returns the server-reported counts so
// the caller can render a richer success toast.
//
// Invalidates runtimes (the list / detail), workspace agents (they are unbound,
// so their runtime column and readiness change) and the agent presence snapshot
// (the delete also cancels queued/running tasks). Without the agent-side
// invalidation the Agents page would keep showing them as runnable.
export function useUnbindAgentsAndDeleteRuntime(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      runtimeId,
      expectedActiveAgentIds,
    }: {
      runtimeId: string;
      expectedActiveAgentIds: string[];
    }) => api.unbindAgentsAndDeleteRuntime(runtimeId, expectedActiveAgentIds),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) });
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
      qc.invalidateQueries({ queryKey: agentTaskSnapshotKeys.all(wsId) });
    },
  });
}

// useUpdateRuntime patches editable fields on a runtime (visibility, custom
// name). Invalidates the runtime list so the picker disabled-state and
// display names recompute.
export function useUpdateRuntime(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      runtimeId,
      patch,
    }: {
      runtimeId: string;
      patch: {
        visibility?: "private" | "public";
        // Empty string clears the custom name; omit to leave unchanged.
        custom_name?: string;
        apply_to_machine?: boolean;
      };
    }) => api.updateRuntime(runtimeId, patch),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) });
    },
  });
}

export function useUpdateRuntimeModelConnection(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      runtimeId,
      connection,
    }: {
      runtimeId: string;
      connection: UpdateRuntimeModelConnectionRequest;
    }) => api.updateRuntimeModelConnection(runtimeId, connection),
    onSettled: (_data, _error, variables) => {
      qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) });
      qc.invalidateQueries({
        queryKey: runtimeModelsKeys.forRuntime(variables.runtimeId),
      });
    },
  });
}

/**
 * Verifies a candidate model connection before it is saved.
 *
 * Not cached: the answer depends on provider-side state (key revoked, balance
 * spent) that can change between attempts, and a stale "valid" is exactly the
 * wrong thing to reuse.
 */
export function useValidateModelConnection() {
  return useMutation({
    mutationFn: (request: ValidateModelConnectionRequest) =>
      api.validateModelConnection(request),
  });
}

export function useDeleteRuntimeModelConnection(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (runtimeId: string) =>
      api.deleteRuntimeModelConnection(runtimeId),
    onSettled: (_data, _error, runtimeId) => {
      qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) });
      qc.invalidateQueries({ queryKey: runtimeModelsKeys.forRuntime(runtimeId) });
    },
  });
}

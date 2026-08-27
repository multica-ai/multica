import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { runtimeKeys } from "./queries";
import { workspaceKeys } from "../workspace/queries";
import {
  agentTaskSnapshotKeys,
  agentTasksKeys,
  workspaceWorkingAgentsKeys,
} from "../agents/queries";
import { autopilotKeys } from "../autopilots/queries";

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
// name).
//
// Invalidation is deliberately wider than "the runtime list" (MUL-6704). A
// visibility flip is not only a display change: making a machine private
// unbinds the agents it may no longer run, cancels their tasks and pauses their
// Autopilots, so every surface that renders those states is now stale. Before
// this, only runtimeKeys was invalidated and the Agents / queue / Autopilot
// pages kept showing agents as runnable and work as pending.
//
// It runs on the plain PATCH too: a private → public flip changes who may pick
// the machine, and the visibility revoke can also arrive as a 409 that the
// dialog completes through useRevokeRuntimeAndMakePrivate.
function invalidateRuntimeAccessSurfaces(
  qc: ReturnType<typeof useQueryClient>,
  wsId: string,
) {
  qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) });
  qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
  qc.invalidateQueries({ queryKey: agentTaskSnapshotKeys.all(wsId) });
  qc.invalidateQueries({ queryKey: agentTasksKeys.all(wsId) });
  qc.invalidateQueries({ queryKey: workspaceWorkingAgentsKeys.all(wsId) });
  qc.invalidateQueries({ queryKey: autopilotKeys.all(wsId) });
}

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
      invalidateRuntimeAccessSurfaces(qc, wsId);
    },
  });
}

// Confirmed public → private revoke (MUL-6704). The plain PATCH refuses with
// `runtime_visibility_has_foreign_agents` and the impact plan; the dialog shows
// it and lands here with the exact set the user confirmed. A
// `runtime_visibility_plan_changed` refusal means the set moved and the user has
// to confirm again — the caller re-renders the fresh plan from the error body.
export function useRevokeRuntimeAndMakePrivate(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      runtimeId,
      ...confirmed
    }: {
      runtimeId: string;
      expectedActiveAgentIds: string[];
      expectedArchivedAgentCount: number;
      expectedRetainedAgentCount: number;
    }) => api.revokeRuntimeAndMakePrivate(runtimeId, confirmed),
    onSettled: () => {
      invalidateRuntimeAccessSurfaces(qc, wsId);
    },
  });
}

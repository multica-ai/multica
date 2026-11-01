import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import type { Agent } from "../types";
import { workspaceKeys } from "../workspace/queries";
import { officeKeys } from "./queries";
import type {
  CreateOfficeFloorRequest,
  OfficeFloor,
  OfficeSeatAssignment,
  UpdateOfficeFloorRequest,
} from "./types";

/*
 * Creates, deletes and seat moves wait for the server: they can be refused
 * (floor cap, occupied floor, a taken seat) and their result is not something
 * the client can predict. Only a rename and a home-squad change, each a single
 * predictable field with a trivial rollback, patch the cache first.
 */

export function useCreateOfficeFloor() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation<OfficeFloor, Error, CreateOfficeFloorRequest>({
    mutationFn: (data) => api.createOfficeFloor(data),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: officeKeys.floors(wsId) });
    },
  });
}

export function useUpdateOfficeFloor() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation<
    OfficeFloor,
    Error,
    { id: string } & UpdateOfficeFloorRequest,
    { previous?: OfficeFloor[] }
  >({
    mutationFn: ({ id, ...data }) => api.updateOfficeFloor(id, data),
    onMutate: async ({ id, name, position }) => {
      // A move renumbers the other floors, so only a pure rename is patched.
      if (name === undefined || position !== undefined) return {};
      const queryKey = officeKeys.floors(wsId);
      await qc.cancelQueries({ queryKey });
      const previous = qc.getQueryData<OfficeFloor[]>(queryKey);
      qc.setQueryData<OfficeFloor[]>(queryKey, (old) =>
        old?.map((floor) => (floor.id === id ? { ...floor, name } : floor)),
      );
      return { previous };
    },
    onError: (_error, _vars, context) => {
      if (context?.previous) qc.setQueryData(officeKeys.floors(wsId), context.previous);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: officeKeys.floors(wsId) });
    },
  });
}

/** Empty floors only; a floor that still holds a squad is refused (409 `floor_not_empty`). */
export function useDeleteOfficeFloor() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation<void, Error, string>({
    mutationFn: (id) => api.deleteOfficeFloor(id),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: officeKeys.floors(wsId) });
    },
  });
}

/**
 * Applies seat moves atomically. A refused request (409 with the holder, see
 * `parseOfficeSeatConflict`) changed nothing on the server, so the squads cache
 * is refreshed either way.
 */
export function useSetOfficeSeats() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (seats: OfficeSeatAssignment[]) => api.setOfficeSeats(seats),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: workspaceKeys.squads(wsId) });
    },
  });
}

/**
 * Sets (or with `null` clears) an agent's home squad through
 * `PUT /api/agents/:id`. Single field, predictable result: patches the agent
 * list first and rolls back if the server refuses (for example when the agent
 * is not a member of that squad).
 */
export function useSetAgentHomeSquad() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation<
    Agent,
    Error,
    { agentId: string; squadId: string | null },
    { previous?: Agent[] }
  >({
    mutationFn: ({ agentId, squadId }) => api.updateAgent(agentId, { home_squad_id: squadId }),
    onMutate: async ({ agentId, squadId }) => {
      const queryKey = workspaceKeys.agents(wsId);
      await qc.cancelQueries({ queryKey });
      const previous = qc.getQueryData<Agent[]>(queryKey);
      qc.setQueryData<Agent[]>(queryKey, (old) =>
        old?.map((agent) => (agent.id === agentId ? { ...agent, home_squad_id: squadId } : agent)),
      );
      return { previous };
    },
    onError: (_error, _vars, context) => {
      if (context?.previous) qc.setQueryData(workspaceKeys.agents(wsId), context.previous);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
    },
  });
}

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import type { Agent } from "../types";
import { workspaceKeys } from "../workspace/queries";
import { projectKeys } from "../projects/queries";
import { issueKeys } from "../issues/queries";
import { companyKeys } from "./queries";
import type {
  AssignSquadProjectRequest,
  AssignSquadProjectResponse,
  CreateCompanyFloorRequest,
  CompanyFloor,
  CompanySeatAssignment,
  UpdateCompanyFloorRequest,
} from "./types";

/*
 * Creates, deletes and seat moves wait for the server: they can be refused
 * (floor cap, occupied floor, a taken seat) and their result is not something
 * the client can predict. Only a rename and a home-squad change, each a single
 * predictable field with a trivial rollback, patch the cache first.
 */

export function useCreateCompanyFloor() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation<CompanyFloor, Error, CreateCompanyFloorRequest>({
    mutationFn: (data) => api.createCompanyFloor(data),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: companyKeys.floors(wsId) });
    },
  });
}

export function useUpdateCompanyFloor() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation<
    CompanyFloor,
    Error,
    { id: string } & UpdateCompanyFloorRequest,
    { previous?: CompanyFloor[] }
  >({
    mutationFn: ({ id, ...data }) => api.updateCompanyFloor(id, data),
    onMutate: async ({ id, name, position }) => {
      // A move renumbers the other floors, so only a pure rename is patched.
      if (name === undefined || position !== undefined) return {};
      const queryKey = companyKeys.floors(wsId);
      await qc.cancelQueries({ queryKey });
      const previous = qc.getQueryData<CompanyFloor[]>(queryKey);
      qc.setQueryData<CompanyFloor[]>(queryKey, (old) =>
        old?.map((floor) => (floor.id === id ? { ...floor, name } : floor)),
      );
      return { previous };
    },
    onError: (_error, _vars, context) => {
      if (context?.previous) qc.setQueryData(companyKeys.floors(wsId), context.previous);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: companyKeys.floors(wsId) });
    },
  });
}

/** Empty floors only; a floor that still holds a squad is refused (409 `floor_not_empty`). */
export function useDeleteCompanyFloor() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation<void, Error, string>({
    mutationFn: (id) => api.deleteCompanyFloor(id),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: companyKeys.floors(wsId) });
    },
  });
}

/**
 * Applies seat moves atomically. A refused request (409 with the holder, see
 * `parseCompanySeatConflict`) changed nothing on the server, so the squads cache
 * is refreshed either way.
 */
export function useSetCompanySeats() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (seats: CompanySeatAssignment[]) => api.setCompanySeats(seats),
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

/**
 * Assigns a project to a squad. The server moves issues and sets the workflow in one
 * transaction, so the result is not predictable on the client: nothing is patched,
 * and the project list and the issues are refetched when the request settles.
 */
export function useAssignSquadProject(squadId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation<AssignSquadProjectResponse, Error, AssignSquadProjectRequest>({
    mutationFn: (data) => api.assignSquadProject(squadId, data),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: projectKeys.list(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
    },
  });
}

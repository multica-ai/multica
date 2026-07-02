import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import { issueKeys } from "../issues/queries";
import { projectKeys } from "../projects/queries";
import { autopilotKeys } from "../autopilots/queries";
import { teamKeys } from "./queries";
import type { CreateTeamRequest, ListTeamsResponse, UpdateTeamRequest } from "../types";

export function useCreateTeam() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: CreateTeamRequest) => api.createTeam(data),
    // Create stays onSuccess-append: there's no stable local id to seed an
    // optimistic row, so we wait for the server-assigned team.
    onSuccess: (team) => {
      qc.setQueryData<ListTeamsResponse>(teamKeys.list(wsId), (old) =>
        old && !old.teams.some((t) => t.id === team.id)
          ? { ...old, teams: [...old.teams, team], total: old.total + 1 }
          : old,
      );
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: teamKeys.all(wsId) });
    },
  });
}

export function useUpdateTeam() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, ...data }: { id: string } & UpdateTeamRequest) =>
      api.updateTeam(id, data),
    onMutate: async ({ id, ...data }) => {
      await qc.cancelQueries({ queryKey: teamKeys.list(wsId) });
      const prevList = qc.getQueryData<ListTeamsResponse>(teamKeys.list(wsId));
      qc.setQueryData<ListTeamsResponse>(teamKeys.list(wsId), (old) =>
        old
          ? { ...old, teams: old.teams.map((t) => (t.id === id ? { ...t, ...data } : t)) }
          : old,
      );
      return { prevList };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prevList) qc.setQueryData(teamKeys.list(wsId), ctx.prevList);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: teamKeys.all(wsId) });
      // A key change re-derives every issue identifier under the team.
      qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
    },
  });
}

export function useArchiveTeam() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.archiveTeam(id),
    onMutate: async (id) => {
      await qc.cancelQueries({ queryKey: teamKeys.list(wsId) });
      const prevList = qc.getQueryData<ListTeamsResponse>(teamKeys.list(wsId));
      const archivedAt = new Date().toISOString();
      qc.setQueryData<ListTeamsResponse>(teamKeys.list(wsId), (old) =>
        old
          ? {
              ...old,
              teams: old.teams.map((t) =>
                t.id === id ? { ...t, archived_at: archivedAt } : t,
              ),
            }
          : old,
      );
      return { prevList };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prevList) qc.setQueryData(teamKeys.list(wsId), ctx.prevList);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: teamKeys.all(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
      qc.invalidateQueries({ queryKey: projectKeys.all(wsId) });
      qc.invalidateQueries({ queryKey: autopilotKeys.all(wsId) });
    },
  });
}

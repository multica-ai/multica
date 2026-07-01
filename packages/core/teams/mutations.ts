import { useMutation, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import { issueKeys } from "../issues/queries";
import { projectKeys } from "../projects/queries";
import { autopilotKeys } from "../autopilots/queries";
import { teamKeys } from "./queries";
import type { CreateTeamRequest, ListTeamsResponse, Team, UpdateTeamRequest } from "../types";

export function useCreateTeam() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: CreateTeamRequest) => api.createTeam(data),
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
    onSuccess: (team) => {
      patchTeam(qc, wsId, team);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: teamKeys.all(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
    },
  });
}

export function useArchiveTeam() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.archiveTeam(id),
    onSuccess: (team) => {
      patchTeam(qc, wsId, team);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: teamKeys.all(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
      qc.invalidateQueries({ queryKey: projectKeys.all(wsId) });
      qc.invalidateQueries({ queryKey: autopilotKeys.all(wsId) });
    },
  });
}

function patchTeam(qc: QueryClient, wsId: string, team: Team) {
  qc.setQueryData<ListTeamsResponse>(teamKeys.list(wsId), (old) =>
    old
      ? {
          ...old,
          teams: old.teams.map((t) => (t.id === team.id ? team : t)),
        }
      : old,
  );
}

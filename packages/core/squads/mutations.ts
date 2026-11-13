import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import { workspaceKeys } from "../workspace/queries";
import type { Squad, SquadMemberType, UpdateSquadRequest } from "../types";
import { squadKeys } from "./queries";

export interface NewSquadMember {
  member_type: SquadMemberType;
  member_id: string;
  role?: string;
}

export interface CreateSquadInput {
  name: string;
  description?: string;
  leader_id: string;
  avatar_url?: string;
  members?: NewSquadMember[];
}

export interface CreateSquadResult {
  squad: Squad;
  /** Members the server refused; the squad exists without them. */
  failedMembers: NewSquadMember[];
}

/** Creates the squad, then adds its members one by one; a refused member does not undo the squad. */
export function useCreateSquad() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: async ({ members = [], ...data }: CreateSquadInput): Promise<CreateSquadResult> => {
      const squad = await api.createSquad(data);
      const results = await Promise.allSettled(members.map((member) => api.addSquadMember(squad.id, member)));
      return { squad, failedMembers: members.filter((_, index) => results[index]?.status === "rejected") };
    },
    onSettled: () => qc.invalidateQueries({ queryKey: workspaceKeys.squads(wsId) }),
  });
}

export function useUpdateSquad(squadId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: UpdateSquadRequest) => api.updateSquad(squadId, data),
    onSuccess: (updated) => qc.setQueryData(squadKeys.detail(wsId, squadId), updated),
    onSettled: () => qc.invalidateQueries({ queryKey: workspaceKeys.squads(wsId) }),
  });
}

export function useAddSquadMember(squadId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (member: NewSquadMember) => api.addSquadMember(squadId, member),
    onSettled: () => qc.invalidateQueries({ queryKey: workspaceKeys.squads(wsId) }),
  });
}

export function useRemoveSquadMember(squadId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (member: { member_type: SquadMemberType; member_id: string }) => api.removeSquadMember(squadId, member),
    onSettled: () => qc.invalidateQueries({ queryKey: workspaceKeys.squads(wsId) }),
  });
}

export function useUpdateSquadMemberRole(squadId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (input: { member_type: SquadMemberType; member_id: string; role: string }) => api.updateSquadMemberRole(squadId, input),
    onSettled: () => qc.invalidateQueries({ queryKey: squadKeys.members(wsId, squadId) }),
  });
}

/** Archives the squad (the server keeps it for history). */
export function useDeleteSquad() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (squadId: string) => api.deleteSquad(squadId),
    onSettled: () => qc.invalidateQueries({ queryKey: workspaceKeys.squads(wsId) }),
  });
}

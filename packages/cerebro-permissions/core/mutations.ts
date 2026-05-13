import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  createPersonaGrant,
  deletePersonaGrant,
  updatePersonaGrant,
} from "./api";
import { permissionsKeys } from "./queries";
import type {
  CreatePersonaGrantRequest,
  UpdatePersonaGrantRequest,
} from "./types";

export function useCreatePersonaGrant() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (body: CreatePersonaGrantRequest) =>
      createPersonaGrant(wsId, body),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: permissionsKeys.all(wsId) });
    },
  });
}

export function useUpdatePersonaGrant() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (vars: { id: string; body: UpdatePersonaGrantRequest }) =>
      updatePersonaGrant(wsId, vars.id, vars.body),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: permissionsKeys.all(wsId) });
    },
  });
}

export function useDeletePersonaGrant() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => deletePersonaGrant(wsId, id),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: permissionsKeys.all(wsId) });
    },
  });
}

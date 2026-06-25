import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { adminKeys } from "./queries";
import type { MemberRole } from "../types";

export function useUpdateUserName() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ userId, name }: { userId: string; name: string }) =>
      api.adminUpdateUser(userId, name),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: adminKeys.users() });
    },
  });
}

export function useAdminCreateInvitations() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: {
      email: string;
      name?: string;
      role?: MemberRole;
      workspaces: string[];
    }) => api.adminCreateInvitations(data),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: adminKeys.users() });
    },
  });
}

export function useAdminAddUserToWorkspaces() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      userId,
      workspaceIds,
      role,
    }: {
      userId: string;
      workspaceIds: string[];
      role?: MemberRole;
    }) => api.adminAddUserToWorkspaces(userId, workspaceIds, role),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: adminKeys.users() });
    },
  });
}

export function useAdminRemoveUserFromWorkspace() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      userId,
      workspaceId,
    }: {
      userId: string;
      workspaceId: string;
    }) => api.adminRemoveUserFromWorkspace(userId, workspaceId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: adminKeys.users() });
    },
  });
}

export function useAdminUpdateUserRole() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      userId,
      workspaceId,
      role,
    }: {
      userId: string;
      workspaceId: string;
      role: MemberRole;
    }) => api.adminUpdateUserRole(userId, workspaceId, role),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: adminKeys.users() });
    },
  });
}

export function useAdminRevokeInvitation() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (invitationId: string) => api.adminRevokeInvitation(invitationId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: adminKeys.invitations() });
    },
  });
}

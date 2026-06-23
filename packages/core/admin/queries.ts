import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const adminKeys = {
  all: ["admin"] as const,
  users: (search?: string, limit?: number, offset?: number) =>
    ["admin", "users", { search, limit, offset }] as const,
  workspaces: () => ["admin", "workspaces"] as const,
  invitations: () => ["admin", "invitations"] as const,
};

export function userListOptions(params?: {
  search?: string;
  limit?: number;
  offset?: number;
}) {
  return queryOptions({
    queryKey: adminKeys.users(params?.search, params?.limit, params?.offset),
    queryFn: () => api.adminListUsers(params),
  });
}

export function workspaceListOptions() {
  return queryOptions({
    queryKey: adminKeys.workspaces(),
    queryFn: () => api.adminListWorkspaces(),
  });
}

export function invitationListOptions() {
  return queryOptions({
    queryKey: adminKeys.invitations(),
    queryFn: () => api.adminListPendingInvitations(),
  });
}

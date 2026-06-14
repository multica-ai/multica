import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const adminKeys = {
  users: (search?: string, limit?: number, offset?: number) =>
    ["admin", "users", { search, limit, offset }] as const,
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

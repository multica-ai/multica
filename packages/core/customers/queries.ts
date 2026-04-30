import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const customerKeys = {
  all: (wsId: string) => ["customers", wsId] as const,
  list: (wsId: string) => [...customerKeys.all(wsId), "list"] as const,
  detail: (wsId: string, id: string) =>
    [...customerKeys.all(wsId), "detail", id] as const,
};

export function customerListOptions(wsId: string) {
  return queryOptions({
    queryKey: customerKeys.list(wsId),
    queryFn: () => api.listCustomers(),
    select: (data) => data.customers,
  });
}

export function customerDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: customerKeys.detail(wsId, id),
    queryFn: () => api.getCustomer(id),
  });
}

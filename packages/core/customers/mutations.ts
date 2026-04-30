import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import { projectKeys } from "../projects/queries";
import type {
  Customer,
  CreateCustomerRequest,
  UpdateCustomerRequest,
  ListCustomersResponse,
} from "../types";
import { customerKeys } from "./queries";

export function useCreateCustomer() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: CreateCustomerRequest) => api.createCustomer(data),
    onSuccess: (newCustomer) => {
      qc.setQueryData<ListCustomersResponse>(customerKeys.list(wsId), (old) =>
        old && !old.customers.some((c) => c.id === newCustomer.id)
          ? { ...old, customers: [...old.customers, newCustomer], total: old.total + 1 }
          : old,
      );
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: customerKeys.list(wsId) });
    },
  });
}

export function useUpdateCustomer() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, ...data }: { id: string } & UpdateCustomerRequest) =>
      api.updateCustomer(id, data),
    onMutate: async ({ id, ...data }) => {
      await qc.cancelQueries({ queryKey: customerKeys.list(wsId) });
      await qc.cancelQueries({ queryKey: customerKeys.detail(wsId, id) });
      const prevList = qc.getQueryData<ListCustomersResponse>(customerKeys.list(wsId));
      const prevDetail = qc.getQueryData<Customer>(customerKeys.detail(wsId, id));
      qc.setQueryData<ListCustomersResponse>(customerKeys.list(wsId), (old) =>
        old ? { ...old, customers: old.customers.map((c) => (c.id === id ? { ...c, ...data } : c)) } : old,
      );
      qc.setQueryData<Customer>(customerKeys.detail(wsId, id), (old) =>
        old ? { ...old, ...data } : old,
      );
      return { prevList, prevDetail, id };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prevList) qc.setQueryData(customerKeys.list(wsId), ctx.prevList);
      if (ctx?.prevDetail) qc.setQueryData(customerKeys.detail(wsId, ctx.id), ctx.prevDetail);
    },
    onSettled: (_data, _err, vars) => {
      qc.invalidateQueries({ queryKey: customerKeys.detail(wsId, vars.id) });
      qc.invalidateQueries({ queryKey: customerKeys.list(wsId) });
      qc.invalidateQueries({ queryKey: projectKeys.all(wsId) });
    },
  });
}

export function useDeleteCustomer() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.deleteCustomer(id),
    onMutate: async (id) => {
      await qc.cancelQueries({ queryKey: customerKeys.list(wsId) });
      const prevList = qc.getQueryData<ListCustomersResponse>(customerKeys.list(wsId));
      qc.setQueryData<ListCustomersResponse>(customerKeys.list(wsId), (old) =>
        old ? { ...old, customers: old.customers.filter((c) => c.id !== id), total: old.total - 1 } : old,
      );
      qc.removeQueries({ queryKey: customerKeys.detail(wsId, id) });
      return { prevList };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prevList) qc.setQueryData(customerKeys.list(wsId), ctx.prevList);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: customerKeys.list(wsId) });
      qc.invalidateQueries({ queryKey: projectKeys.all(wsId) });
    },
  });
}

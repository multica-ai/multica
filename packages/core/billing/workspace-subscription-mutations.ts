import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { CreateWorkspaceSubscriptionCheckoutRequest } from "../types";
import { workspaceSubscriptionKeys } from "./workspace-subscription-queries";

export function useCreateWorkspaceSubscriptionCheckout(wsId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (data: CreateWorkspaceSubscriptionCheckoutRequest) =>
      api.createWorkspaceSubscriptionCheckout(data),
    onSuccess: () => {
      void queryClient.invalidateQueries({
        queryKey: workspaceSubscriptionKeys.entitlements(wsId),
      });
    },
  });
}

export function useReconcileWorkspaceSubscriptionSeats(wsId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => api.reconcileWorkspaceSubscriptionSeats(),
    onSuccess: () => {
      void queryClient.invalidateQueries({
        queryKey: workspaceSubscriptionKeys.entitlements(wsId),
      });
    },
  });
}

export function useCreateWorkspaceSubscriptionPortal(wsId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (idempotencyKey: string) =>
      api.createWorkspaceSubscriptionPortal(idempotencyKey),
    onSuccess: () => {
      void queryClient.invalidateQueries({
        queryKey: workspaceSubscriptionKeys.entitlements(wsId),
      });
    },
  });
}

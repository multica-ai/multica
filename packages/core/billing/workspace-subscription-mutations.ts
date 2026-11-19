import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type {
  ChangeWorkspacePlanRequest,
  CreateWorkspaceSubscriptionCheckoutRequest,
  PreviewWorkspacePlanChangeRequest,
  PreviewWorkspaceSeatPurchaseRequest,
  PurchaseWorkspaceSeatsRequest,
} from "../types";
import { workspaceSubscriptionKeys } from "./workspace-subscription-queries";

export function useCreateWorkspaceSubscriptionCheckout(wsId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (data: CreateWorkspaceSubscriptionCheckoutRequest) =>
      api.createWorkspaceSubscriptionCheckout(data),
    onSuccess: () => {
      void queryClient.invalidateQueries({
        queryKey: workspaceSubscriptionKeys.summary(wsId),
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
        queryKey: workspaceSubscriptionKeys.summary(wsId),
      });
    },
  });
}

export function usePreviewWorkspaceSeatPurchase() {
  return useMutation({
    mutationFn: (data: PreviewWorkspaceSeatPurchaseRequest) =>
      api.previewWorkspaceSeatPurchase(data),
  });
}

export function usePurchaseWorkspaceSeats(wsId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (data: PurchaseWorkspaceSeatsRequest) =>
      api.purchaseWorkspaceSeats(data),
    onSuccess: () => {
      void queryClient.invalidateQueries({
        queryKey: workspaceSubscriptionKeys.summary(wsId),
      });
    },
  });
}

export function usePreviewWorkspacePlanChange() {
  return useMutation({
    mutationFn: (data: PreviewWorkspacePlanChangeRequest) =>
      api.previewWorkspacePlanChange(data),
  });
}

/**
 * Cloud persists the new plan before answering, so refreshing the whole
 * workspace subscription scope (summary and anything keyed under it) shows the
 * switched plan without waiting for Stripe's webhook.
 */
export function useChangeWorkspacePlan(wsId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (data: ChangeWorkspacePlanRequest) =>
      api.changeWorkspacePlan(data),
    onSuccess: () => {
      void queryClient.invalidateQueries({
        queryKey: workspaceSubscriptionKeys.all(wsId),
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
        queryKey: workspaceSubscriptionKeys.summary(wsId),
      });
    },
  });
}

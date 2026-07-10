import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { modelRegistryKeys } from "./queries";
import type {
  CreateModelRegistryChangeRequestRequest,
  ReviewModelRegistryChangeRequestRequest,
  RollbackModelRegistryRequest,
} from "@multica/core/types";

export function useCreateModelRegistryChangeRequest() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: CreateModelRegistryChangeRequestRequest) =>
      api.createModelRegistryChangeRequest(data),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: modelRegistryKeys.changeRequests() });
    },
  });
}

export function useReviewModelRegistryChangeRequest() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      crId,
      data,
    }: {
      crId: string;
      data: ReviewModelRegistryChangeRequestRequest;
    }) => api.reviewModelRegistryChangeRequest(crId, data),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: modelRegistryKeys.changeRequests() });
      qc.invalidateQueries({ queryKey: modelRegistryKeys.versions() });
      qc.invalidateQueries({ queryKey: modelRegistryKeys.live() });
      // The registry pricing sync (packages/core/runtimes) reads through its
      // own query key — invalidate it too so an approved price change is
      // reflected in cost estimation without waiting for its 30s staleTime.
      qc.invalidateQueries({ queryKey: ["cerebro", "model-registry"] });
    },
  });
}

export function useRollbackModelRegistry() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: RollbackModelRegistryRequest) =>
      api.rollbackModelRegistry(data),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: modelRegistryKeys.versions() });
      qc.invalidateQueries({ queryKey: modelRegistryKeys.live() });
      qc.invalidateQueries({ queryKey: ["cerebro", "model-registry"] });
    },
  });
}

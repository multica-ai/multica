import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { governanceKeys } from "./queries";
import type { CreateBudgetRequest, UpdateBudgetRequest } from "./budgets-types";

/*
 * Every budget write waits for the server and then shows what the server kept.
 * A write also adds a governance entry and can release holds, so the whole
 * governance cache is refetched when it settles.
 */

function useBudgetWrite<Vars, Result = unknown>(wsId: string, run: (vars: Vars) => Promise<Result>) {
  const qc = useQueryClient();
  return useMutation<Result, Error, Vars>({
    mutationFn: run,
    onSettled: () => {
      qc.invalidateQueries({ queryKey: governanceKeys.all(wsId) });
    },
  });
}

export function useCreateBudget(wsId: string) {
  return useBudgetWrite<CreateBudgetRequest>(wsId, (data) => api.createBudget(data));
}

export function useUpdateBudget(wsId: string) {
  return useBudgetWrite<{ id: string; patch: UpdateBudgetRequest }>(wsId, ({ id, patch }) => api.updateBudget(id, patch));
}

export function useExtendBudget(wsId: string) {
  return useBudgetWrite<{ id: string; amount_micros: number }>(wsId, ({ id, amount_micros }) =>
    api.extendBudget(id, amount_micros),
  );
}

export function useReleaseBudgetHolds(wsId: string) {
  return useBudgetWrite<{ id: string }, { released: number }>(wsId, ({ id }) => api.releaseBudgetHolds(id));
}

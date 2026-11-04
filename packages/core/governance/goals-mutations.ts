import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { goalKeys } from "./goals-queries";
import type {
  CreateGoalRequest,
  Goal,
  GoalConfidence,
  MetricReading,
  ReportReadingRequest,
  UpdateGoalRequest,
} from "./goals-types";

/*
 * Every write waits for the server and then refetches what it changed: a goal's
 * progress and confidence come from readings the server computes, so nothing is
 * patched ahead of the response. A reading moves every goal that reads its
 * metric, so a reading refetches the whole goal workspace.
 */

function useGoalWrite<Vars, Result>(wsId: string, run: (vars: Vars) => Promise<Result>) {
  const qc = useQueryClient();
  return useMutation<Result, Error, Vars>({
    mutationFn: run,
    onSettled: () => {
      qc.invalidateQueries({ queryKey: goalKeys.all(wsId) });
    },
  });
}

export function useCreateGoal(wsId: string) {
  return useGoalWrite<CreateGoalRequest, Goal>(wsId, (data) => api.createGoal(data));
}

export function useUpdateGoal(wsId: string) {
  return useGoalWrite<{ id: string; data: UpdateGoalRequest }, Goal>(wsId, ({ id, data }) => api.updateGoal(id, data));
}

export function useActivateGoal(wsId: string) {
  return useGoalWrite<string, Goal>(wsId, (id) => api.activateGoal(id));
}

export function useSetGoalStatus(wsId: string) {
  return useGoalWrite<{ id: string; status: "achieved" | "missed" | "dropped" }, Goal>(wsId, ({ id, status }) =>
    api.setGoalStatus(id, status),
  );
}

export function useDeleteGoal(wsId: string) {
  return useGoalWrite<string, void>(wsId, (id) => api.deleteGoal(id));
}

/** Sets or clears the owner's confidence. A value needs a note. */
export function useSetGoalConfidence(wsId: string) {
  return useGoalWrite<{ id: string; confidence: GoalConfidence | null; note: string }, Goal>(wsId, ({ id, confidence, note }) =>
    api.setGoalConfidence(id, confidence, note),
  );
}

/** Adds a reading to a metric at a scope. The first report of a key creates the metric. */
export function useReportMetricReading(wsId: string) {
  return useGoalWrite<{ key: string; data: ReportReadingRequest }, MetricReading>(wsId, ({ key, data }) =>
    api.reportMetricReading(key, data),
  );
}


import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import type { IssueEnvironmentCommand, StartIssueEnvironmentRequest } from "../types";
import { issueKeys } from "./queries";

export const environmentCommandKeys = {
  command: (issueId: string, commandId: string) =>
    ["issues", "environment-commands", issueId, commandId] as const,
};

/** A command the machine has not answered yet; polled until it has. */
export function issueEnvironmentCommandOptions(issueId: string, commandId: string | null) {
  return queryOptions({
    queryKey: environmentCommandKeys.command(issueId, commandId ?? ""),
    queryFn: () => api.getIssueEnvironmentCommand(issueId, commandId!),
    enabled: !!issueId && !!commandId,
    refetchInterval: (query) => (query.state.data?.status === "pending" ? 1_000 : false),
    staleTime: Infinity,
  });
}

export type IssueEnvironmentControl =
  | { action: "stop" | "restart"; environmentId: string }
  | { action: "start"; body: StartIssueEnvironmentRequest };

/** Stops, restarts or starts an environment; the list refetches to show it pending. */
export function useControlIssueEnvironment(issueId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (control: IssueEnvironmentControl): Promise<IssueEnvironmentCommand> => {
      switch (control.action) {
        case "stop":
          return api.stopIssueEnvironment(issueId, control.environmentId);
        case "restart":
          return api.restartIssueEnvironment(issueId, control.environmentId);
        case "start":
          return api.startIssueEnvironment(issueId, control.body);
      }
    },
    onSettled: () => qc.invalidateQueries({ queryKey: issueKeys.environments(wsId, issueId) }),
  });
}

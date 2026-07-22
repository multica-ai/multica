import { queryOptions } from "@tanstack/react-query";
import { fetchCommands } from "./api";

export const commandKeys = {
  all: (workspaceId: string) => ["cerebro", "commands", workspaceId] as const,
  list: (workspaceId: string) => [...commandKeys.all(workspaceId), "list"] as const,
};

export function commandsListOptions(workspaceId: string) {
  return queryOptions({ queryKey: commandKeys.list(workspaceId), queryFn: fetchCommands, enabled: !!workspaceId });
}

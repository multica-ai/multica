import { api, parseWithFallback } from "@multica/core/api";
import { commandSchema, commandsListSchema } from "./schemas";
import type { CerebroCommand, CommandInput } from "./types";

export async function fetchCommands(): Promise<CerebroCommand[]> {
  const raw = await api.cerebroRequest<unknown>("/api/cerebro/commands");
  return parseWithFallback(raw, commandsListSchema, { commands: [] }, { endpoint: "listCerebroCommands" }).commands as CerebroCommand[];
}

export async function createCommand(input: CommandInput): Promise<CerebroCommand> {
  const raw = await api.cerebroRequest<unknown>("/api/cerebro/commands", { method: "POST", body: JSON.stringify(input) });
  const parsed = parseWithFallback(raw, commandSchema, null, { endpoint: "createCerebroCommand" });
  if (!parsed) throw new Error("Invalid create command response");
  return parsed as CerebroCommand;
}

export async function updateCommand(id: string, input: CommandInput): Promise<CerebroCommand> {
  const raw = await api.cerebroRequest<unknown>(`/api/cerebro/commands/${encodeURIComponent(id)}`, { method: "PUT", body: JSON.stringify(input) });
  const parsed = parseWithFallback(raw, commandSchema, null, { endpoint: "updateCerebroCommand" });
  if (!parsed) throw new Error("Invalid update command response");
  return parsed as CerebroCommand;
}

export async function deleteCommand(id: string): Promise<void> {
  await api.cerebroRequest(`/api/cerebro/commands/${encodeURIComponent(id)}`, { method: "DELETE" });
}

import { api, parseWithFallback } from "@multica/core/api";
import { z } from "zod";
import { cycleSchema, memberSchema, roundSchema, roundStatusSchema, type RoundStatus } from "./schemas";

const listSchema = z.object({ rounds: z.array(roundSchema).default([]) }).loose();
export type Round = z.infer<typeof roundSchema>;

export async function listRounds(): Promise<Round[]> {
  const path = "/api/cerebro/rounds";
  const raw = await api.cerebroRequest<unknown>(path);
  return parseWithFallback(raw, listSchema, { rounds: [] }, { endpoint: path }).rounds;
}
export async function getRoundStatus(id: string): Promise<RoundStatus | null> {
  const path = `/api/cerebro/rounds/${id}/status`;
  return parseWithFallback(await api.cerebroRequest<unknown>(path), roundStatusSchema, null, { endpoint: path });
}
export async function addIssueToRound(roundId: string, issueId: string) {
  const path = `/api/cerebro/rounds/${roundId}/members`;
  return parseWithFallback(await api.cerebroRequest<unknown>(path, { method: "POST", body: JSON.stringify({ issue_id: issueId }) }), memberSchema, null, { endpoint: path });
}
export async function startRound(roundId: string) {
  const path = `/api/cerebro/rounds/${roundId}/start`;
  return parseWithFallback(await api.cerebroRequest<unknown>(path, { method: "POST" }), cycleSchema, null, { endpoint: path });
}
export type RoundInput = { name: string };
export async function createRound(input: RoundInput): Promise<Round | null> {
  const path = "/api/cerebro/rounds";
  return parseWithFallback(await api.cerebroRequest<unknown>(path, { method: "POST", body: JSON.stringify(input) }), roundSchema, null, { endpoint: path });
}
export async function updateRound(id: string, input: RoundInput): Promise<Round | null> {
  const path = `/api/cerebro/rounds/${id}`;
  return parseWithFallback(await api.cerebroRequest<unknown>(path, { method: "PATCH", body: JSON.stringify(input) }), roundSchema, null, { endpoint: path });
}
export async function deleteRound(id: string): Promise<void> {
  await api.cerebroRequest(`/api/cerebro/rounds/${id}`, { method: "DELETE" });
}
export async function removeIssueFromRound(roundId: string, issueId: string): Promise<void> {
  await api.cerebroRequest(`/api/cerebro/rounds/${roundId}/members/${issueId}`, { method: "DELETE" });
}

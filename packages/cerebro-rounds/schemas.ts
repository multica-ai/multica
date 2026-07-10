import { z } from "zod";
import { parseWithFallback } from "@multica/core/api";

const roundSchema = z.object({
  id: z.string(), workspace_id: z.string().default(""), owner_id: z.string().default(""),
  name: z.string().default("Round"), schedule_cron: z.string().nullish().default(null),
  timezone: z.string().nullish().default(null), next_run_at: z.string().nullish().default(null),
  created_at: z.string().default(""), updated_at: z.string().default(""),
}).loose();
const memberSchema = z.object({
  round_id: z.string(), issue_id: z.string(), added_by_type: z.string().default(""),
  added_by_id: z.string().default(""), held_trigger_count: z.number().int().nonnegative().default(0),
  created_at: z.string().default(""),
}).loose();
const runSchema = z.object({
  id: z.string(), round_id: z.string(), status: z.string().transform((value): "running" | "ready" | "completed" | "failed" | "unknown" =>
    value === "running" || value === "ready" || value === "completed" || value === "failed" ? value : "unknown"),
  total_count: z.number().int().nonnegative().default(0), responded_count: z.number().int().nonnegative().default(0),
  stalled_count: z.number().int().nonnegative().default(0), nudged_count: z.number().int().nonnegative().default(0),
  started_at: z.string().nullish().default(null), ready_at: z.string().nullish().default(null),
  completed_at: z.string().nullish().default(null), created_at: z.string().default(""),
}).loose();
export const roundStatusSchema = z.object({ round: roundSchema, active_run: runSchema.nullish().default(null), members: z.array(memberSchema).default([]) }).loose();
export type RoundStatus = z.infer<typeof roundStatusSchema>;
const statusesSchema = z.object({ rounds: z.array(roundStatusSchema).default([]) }).loose();
export function parseRoundStatuses(raw: unknown): RoundStatus[] { return parseWithFallback(raw, statusesSchema, { rounds: [] }, { endpoint: "round-statuses" }).rounds; }
export function roundIssueIdsToExclude(statuses: RoundStatus[]): Set<string> {
  return new Set(statuses.flatMap((s) => s.members.map((m) => m.issue_id)));
}
export function roundRunState(statuses: RoundStatus[], issueId: string): "round_running" | "round_queued" | null {
  const status = statuses.find((s) => s.members.some((m) => m.issue_id === issueId));
  if (!status) return null;
  return status.active_run?.status === "running" ? "round_running" : "round_queued";
}
export function roundMembershipLabel(statuses: RoundStatus[], issueId: string): string | null {
  const status = statuses.find((candidate) => candidate.members.some((member) => member.issue_id === issueId));
  if (!status) return null;
  const member = status.members.find((candidate) => candidate.issue_id === issueId);
  const state = status.active_run?.status === "running" ? "running" : status.active_run?.status === "ready" ? "ready" : "queued";
  const held = member?.held_trigger_count ?? 0;
  return `${status.round.name} · ${state}${held > 0 ? ` · ${held} held ${held === 1 ? "response" : "responses"}` : ""}`;
}
export { roundSchema, memberSchema, runSchema };

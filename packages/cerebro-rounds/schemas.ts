import { z } from "zod";
import { parseWithFallback } from "@multica/core/api";

const roundSchema = z.object({
  id: z.string(), workspace_id: z.string().default(""), owner_id: z.string().default(""),
  name: z.string().default("Round"), mode: z.enum(["live", "batch"]).catch("batch").default("batch"), schedule_cron: z.string().nullish().default(null),
  timezone: z.string().nullish().default(null), next_run_at: z.string().nullish().default(null),
  // Non-null while an answer cycle is open: the round is surfaced and its
  // waiting messages need the owner (FIR-3114 review round 3). Older servers
  // omit the field — null degrades to the resting state.
  cycle_opened_at: z.string().nullish().default(null),
  created_at: z.string().default(""), updated_at: z.string().default(""),
}).loose();
const memberSchema = z.object({
  round_id: z.string(), issue_id: z.string(), added_by_type: z.string().default(""),
  added_by_id: z.string().default(""), held_trigger_count: z.number().int().nonnegative().default(0),
  // Owner-perspective state (FIR-3114 review): only 'waiting' members surface
  // in the round list; 'answered' folds away; 'working' and 'queued' hide
  // until the next cycle surfaces their responses. Older servers omit these
  // fields — default to 'planned'/0, which degrades to showing every member
  // (the pre-review behavior).
  waiting_count: z.number().int().nonnegative().default(0),
  queued_count: z.number().int().nonnegative().default(0),
  state: z.enum(["waiting", "answered", "working", "queued", "planned"]).catch("planned").default("planned"),
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
// roundActivity condenses a round's owner-perspective signals (FIR-3114
// review round 3): `cycleOpen` — an answer cycle is open and `waiting`
// messages need the owner; otherwise the round rests in the "Ready to start"
// group while `queued` responses, `held` replies, `working` agents or a
// `running` dispatch accumulate for the next start. A round with none of it
// has 0 messages and disappears from the block entirely (point 4).
export function roundActivity(status: RoundStatus) {
  const waiting = status.members.reduce((total, m) => total + m.waiting_count, 0);
  const queued = status.members.reduce((total, m) => total + m.queued_count, 0);
  const held = status.members.reduce((total, m) => total + m.held_trigger_count, 0);
  const working = status.members.some((m) => m.state === "working");
  const running = status.active_run?.status === "running";
  const cycleOpen = status.round.cycle_opened_at != null;
  const hasContent = status.members.length > 0 && (waiting > 0 || queued > 0 || held > 0 || working || running || cycleOpen);
  return { waiting, queued, held, working, running, cycleOpen, hasContent };
}
export function roundIssueIdsToExclude(statuses: RoundStatus[], blockActive = true): Set<string> {
  if (!blockActive) return new Set();
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

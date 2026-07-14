import { z } from "zod";
import { parseWithFallback } from "@multica/core/api";

export const roundSchema = z.object({
  id: z.string(),
  workspace_id: z.string().default(""),
  owner_id: z.string().default(""),
  name: z.string().default("Round"),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
});

export const memberSchema = z.object({
  round_id: z.string(),
  issue_id: z.string(),
  added_by_type: z.string().default(""),
  added_by_id: z.string().default(""),
  created_at: z.string().default(""),
});

export const cycleItemSchema = z.object({
  issue_id: z.string(),
  handled_at: z.string().nullish().default(null),
});

export const cycleSchema = z.object({
  id: z.string(),
  round_id: z.string(),
  started_at: z.string(),
  items: z.array(cycleItemSchema).default([]),
});

export const roundStatusSchema = z.object({
  round: roundSchema,
  members: z.array(memberSchema).default([]),
  active_cycle: cycleSchema.nullish().default(null),
});

export type RoundStatus = z.infer<typeof roundStatusSchema>;

const statusesSchema = z.object({ rounds: z.array(roundStatusSchema).default([]) });

export function parseRoundStatuses(raw: unknown): RoundStatus[] {
  return parseWithFallback(raw, statusesSchema, { rounds: [] }, { endpoint: "round-statuses" }).rounds;
}

export function roundMembershipLabel(statuses: RoundStatus[], issueId: string): string | null {
  const status = statuses.find((candidate) => candidate.members.some((member) => member.issue_id === issueId));
  return status ? `In ${status.round.name}` : null;
}

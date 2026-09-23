import { z } from "zod";

const snapshot = z.object({ state: z.string(), activity: z.number().int().nonnegative() });
const receipt = z.object({
  outcome: z.string(), snapshot,
  cleared_steering: z.array(z.string()).optional().default([]),
  cleared_follow_up: z.array(z.string()).optional().default([]),
}).transform((value) => ({
  outcome: value.outcome, snapshot: value.snapshot,
  clearedSteering: value.cleared_steering, clearedFollowUp: value.cleared_follow_up,
}));

export const TaskInteractionSchema = z.object({
  opening_input: z.string().optional().default(""),
  owner: z.string(), state: snapshot, deadline: z.string(),
  updated_at: z.string(), finishing: z.boolean().optional().default(false),
  commands: z.array(z.object({
    id: z.string(), kind: z.string(), activity: z.number(), text: z.string().optional().default(""),
    actor_id: z.string(), created_at: z.string(), receipt: receipt.optional().nullable(),
    error: z.string().optional().default(""),
  })).default([]),
}).transform((value) => ({
  owner: value.owner, state: value.state, deadline: value.deadline,
  openingInput: value.opening_input,
  updatedAt: value.updated_at, finishing: value.finishing,
  commands: value.commands.map((command) => ({
    id: command.id, kind: command.kind, activity: command.activity, text: command.text,
    actorId: command.actor_id, createdAt: command.created_at,
    receipt: command.receipt, error: command.error,
  })),
})).nullable();

export type TaskInteraction = NonNullable<z.infer<typeof TaskInteractionSchema>>;
export const TaskInteractionFollowupSchema = z.object({ run_id: z.string().min(1) })
  .transform((value) => ({ runId: value.run_id }));
export interface TaskInteractionCommand {
  owner: string;
  id: string;
  kind: "input" | "interrupt" | "finish";
  activity: number;
  text?: string;
}

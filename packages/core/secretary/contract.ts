import { z } from "zod";

const date = z.string().regex(/^\d{4}-\d{2}-\d{2}$/).nullable().optional();
export const SecretaryItemSchema = z.object({
  key: z.string().min(1), issue_id: z.string().nullable().optional(), action_id: z.string().optional(),
  case_id: z.string(), kind: z.enum(["action", "reference", "operation"]),
  stage: z.enum(["ready", "preparing", "waiting", "later", "history"]),
  owner: z.enum(["chairman", "secretary", "external"]),
  title: z.string(), situation: z.string(), next_step: z.string(),
  recommendation: z.string().optional().default(""), why_now: z.string().optional().default(""),
  completion: z.string().optional().default(""), due_date: date, follow_up_on: date,
  watch_reason: z.string().optional().default(""),
  follow_up_trigger: z.string().optional().default(""), source_updated_at: z.string(),
  scheduled_on: date, estimate_minutes: z.number().nullable().optional(),
  pending_reconciliation: z.boolean().optional(),
  closure_mode: z.enum(["self_report", "verified_result"]).optional().default("verified_result"), reported_at: z.string().datetime({ offset: true }).optional(),
  reported_status: z.enum(["completed", "cancelled"]).nullable().optional(),
  instruction_note: z.string().optional(), linked_issue_ids: z.array(z.string()).optional().default([]),
  business_status: z.string().optional(), stale: z.boolean().optional(),
  links: z.array(z.object({ label: z.string(), url: z.string() })).optional().default([]),
});
export const SecretaryInstructionSchema = z.object({
  sequence: z.number().int(), request_id: z.string(), item_key: z.string(),
  kind: z.enum(["plan", "complete", "cancel", "prepare", "wait", "later", "reopen", "capacity"]),
  payload: z.object({ scheduled_on: date, estimate_minutes: z.number().nullable().optional(),
    capacity_minutes: z.number().nullable().optional(), note: z.string().optional().default("") }),
  created_at: z.string(), canonical_receipt: z.string().nullable(),
});
export const SecretaryResponseSchema = z.object({
  resolved: z.boolean().optional().default(false),
  revision: z.number().int(), unmapped_count: z.number().int(),
  projection: z.object({ version: z.literal(1), state_version: z.number(), source_as_of: z.string(),
    cases: z.array(z.object({ id: z.string(), title: z.string(), area: z.string(), summary: z.string() })),
    items: z.array(SecretaryItemSchema),
  }).nullable(),
  instructions: z.array(SecretaryInstructionSchema),
});
export const SecretaryReceiptSchema = z.object({ sequence: z.number().int().positive(), request_id: z.string() });
export type SecretaryResponse = z.infer<typeof SecretaryResponseSchema>;
export type SecretaryItem = z.infer<typeof SecretaryItemSchema>;
export type SecretaryInstruction = z.infer<typeof SecretaryInstructionSchema>;
export type SecretaryInstructionInput = {
  request_id: string; expected_revision: number; item_key: string;
  kind: SecretaryInstruction["kind"]; scheduled_on?: string | null;
  estimate_minutes?: number | null; capacity_minutes?: number; note?: string;
};

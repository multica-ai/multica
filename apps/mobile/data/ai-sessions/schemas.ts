import { z } from "zod";
import { parseWithFallback } from "@/lib/parse-response";
import { localV2SnapshotSchema } from "@multica/core/local-conversation/v2";

export const devicesSchema = z.object({ devices: z.array(z.object({
  peer_id: z.string().min(1), device_name: z.string().default(""),
  connected: z.boolean().default(false), available: z.boolean().default(false), connection: z.string().default(""),
})).default([]) });
export type AIDevice = z.infer<typeof devicesSchema>["devices"][number];
export const projectsSchema = z.object({ projects: z.array(z.object({ id: z.string(), title: z.string(), path: z.string() })).default([]) });
export const providersSchema = z.object({ providers: z.array(z.object({ id: z.string(), name: z.string(), available: z.boolean().default(false) })).default([]) });
export const modelsSchema = z.object({ supported: z.boolean().default(false), models: z.array(z.object({
  id: z.string(), label: z.string(), isDefault: z.boolean().optional(),
  thinking: z.object({ supported_levels: z.array(z.object({ value: z.string(), label: z.string() })).default([]) }).optional(),
})).nullable().transform((value) => value ?? []) });
export const directoriesSchema = z.object({ path: z.string(), parent: z.string(), entries: z.array(z.object({ name: z.string(), path: z.string() })) });
export const questionsSchema = z.object({ questions: z.array(z.object({
  id: z.string(), question: z.string(), options: z.array(z.object({ label: z.string() })).default([]),
  multi_select: z.boolean().default(false), allow_free_text: z.boolean().default(true),
})) });
export const commandResultSchema = z.object({ turn_id: z.string().optional(), replayed: z.boolean().optional(), status: z.string().optional() });

export const queueSchema = localV2SnapshotSchema.shape.queue;
export type AIQueue = z.infer<typeof queueSchema>;
export const attachmentMetadataSchema = z.object({ name: z.string(), mime_type: z.string(), size: z.number().nonnegative() });
export const queueItemSchema = z.object({ requestId: z.string(), content: z.string(), attachmentIds: z.array(z.string()).default([]) });
export const usageSchema = z.object({
  provider: z.string(),
  context: z.object({ used_tokens: z.number().nonnegative().nullable(), limit_tokens: z.number().positive().nullable(), model: z.string().nullable(), updated_at: z.string().nullable() }),
  account: z.object({ status: z.string(), plan: z.string().nullable(), windows: z.array(z.object({ id: z.string(), label: z.string(), used_percent: z.number().finite(), resets_at: z.string().nullable() })).default([]), updated_at: z.string().nullable(), stale: z.boolean().default(false) }),
});
export const historySchema = z.object({ store_generation: z.string(), turns: localV2SnapshotSchema.shape.turns, has_older_turns: z.boolean() });

export function remainingUsage(used: number, resetsAt: string | null, now = Date.now()): number | null {
  if (resetsAt && (!Number.isFinite(Date.parse(resetsAt)) || Date.parse(resetsAt) <= now)) return null;
  return Math.max(0, Math.min(100, 100 - used));
}

export function readResponse<T>(raw: unknown, schema: z.ZodType<T>, endpoint: string): T {
  const result = parseWithFallback<T | null>(raw, schema, null, { endpoint });
  if (result === null) throw new Error("invalid_response");
  return result;
}

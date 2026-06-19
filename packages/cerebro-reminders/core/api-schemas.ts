import { z } from "zod";

// Response readers route through parseWithFallback so an older/newer backend
// degrades to safe defaults instead of breaking the UI (CLAUDE.md → API
// Response Compatibility). Enums stay lenient (`.catch`) so an unknown status
// from a newer backend still parses instead of dropping the whole row.

// Optional source link: the backend sends `null` (Go pointers) when the source
// message/conversation was deleted. Collapse null → undefined so the runtime
// value matches the `string | undefined` TS type.
const optStr = z
  .string()
  .nullish()
  .transform((v) => v ?? undefined);

export const reminderStatusSchema = z
  .enum(["pending", "snoozed", "fired", "done"])
  .catch("pending");

export const reminderSchema = z.object({
  id: z.string(),
  remind_at: z.string(),
  status: reminderStatusSchema,
  text: z.string().catch(""),
  message_id: optStr,
  conversation_id: optStr,
  conversation_kind: optStr,
  conversation_title: optStr,
  source_preview: optStr,
  fired_at: optStr,
  created_at: z.string(),
  updated_at: z.string(),
});

export const reminderListSchema = z.array(reminderSchema);

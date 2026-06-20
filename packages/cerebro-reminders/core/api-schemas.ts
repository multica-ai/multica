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

// Enum drift downgrades, not crashes: an unknown recipient/anchor from a newer
// backend falls back to the safe default instead of dropping the row.
export const recipientTypeSchema = z.enum(["member", "agent"]).catch("member");
export const anchorTypeSchema = z
  .enum(["comment", "issue", "project", "chat_message", "none"])
  .catch("none");

export const reminderSchema = z.object({
  id: z.string(),
  // Older backends (pre-v2) omit these; default so the row still parses.
  creator_id: z.string().catch(""),
  recipient_type: recipientTypeSchema,
  recipient_id: z.string().catch(""),
  remind_at: z.string(),
  status: reminderStatusSchema,
  text: z.string().catch(""),
  anchor_type: anchorTypeSchema,
  message_id: optStr,
  conversation_id: optStr,
  conversation_kind: optStr,
  conversation_title: optStr,
  project_id: optStr,
  project_title: optStr,
  chat_message_id: optStr,
  chat_session_id: optStr,
  chat_session_title: optStr,
  source_preview: optStr,
  fired_at: optStr,
  created_at: z.string(),
  updated_at: z.string(),
});

export const reminderListSchema = z.array(reminderSchema);

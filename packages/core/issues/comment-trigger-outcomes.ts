import { CommentTriggerOutcomeSchema } from "../api/schemas";
import type { CommentTriggerOutcome } from "../types/comment";

// Validates the `trigger_outcomes` off a create/edit comment response
// (MUL-4525 §2). The create/edit responses are not fully schema-parsed, so the
// one field the UI branches on is validated here: a non-array yields [], and a
// malformed entry is dropped individually rather than failing the whole set.
export function parseCommentTriggerOutcomes(raw: unknown): CommentTriggerOutcome[] {
  if (!Array.isArray(raw)) return [];
  const out: CommentTriggerOutcome[] = [];
  for (const item of raw) {
    const parsed = CommentTriggerOutcomeSchema.safeParse(item);
    if (parsed.success) {
      out.push(parsed.data as CommentTriggerOutcome);
    }
  }
  return out;
}

// The explicit @agent / @squad mentions that did NOT trigger. Coalesced /
// deferred / queued are all success-shaped (the mention was handled), so only
// `blocked` counts toward the "posted, but N not triggered" warning.
export function blockedCommentTriggerOutcomes(raw: unknown): CommentTriggerOutcome[] {
  return parseCommentTriggerOutcomes(raw).filter((o) => o.status === "blocked");
}

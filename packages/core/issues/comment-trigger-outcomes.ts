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

// The only success-shaped outcome statuses: the mention WAS handled (a run was
// queued, coalesced into an existing run, or intentionally deferred). Success is
// a WHITELIST, not "anything that isn't blocked", so an unknown/future status —
// or the empty status the schema defaults for a malformed entry — never passes
// as success (MUL-4525; mirrors the Run now whitelist).
const HANDLED_TRIGGER_STATUSES = new Set(["queued", "coalesced", "deferred"]);

// The explicit @agent / @squad mentions that did NOT clearly trigger, so the
// "posted, but N not triggered" warning must cover them: `blocked` plus any
// unknown/future/empty status. Never assume an unrecognized status succeeded.
export function unhandledCommentTriggerOutcomes(raw: unknown): CommentTriggerOutcome[] {
  return parseCommentTriggerOutcomes(raw).filter((o) => !HANDLED_TRIGGER_STATUSES.has(o.status));
}

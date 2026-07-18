import { CommentTriggerOutcomeSchema } from "../api/schemas";
import type { CommentTriggerOutcome } from "../types/comment";

// Source for a rendered mention in comment markdown, capturing the label the
// user picked, the target type, and the target id: `[@Go](mention://agent/UUID)`.
// Kept as a string so every parse builds its OWN global RegExp — sharing one
// global instance across `matchAll` calls leaks `lastIndex` and drops matches.
const MENTION_MARKUP_SOURCE =
  "\\[@?(.+?)\\]\\(mention:\\/\\/(member|agent|squad|issue|all)\\/([0-9a-fA-F-]+|all)\\)";

export interface ParsedMention {
  label: string;
  type: string;
  id: string;
}

// Every mention in the body, in order. Callers that only care about a subset
// (e.g. skipping `issue` links, or deduping) filter the result.
export function parseMentions(content: string): ParsedMention[] {
  const re = new RegExp(MENTION_MARKUP_SOURCE, "g");
  const mentions: ParsedMention[] = [];
  for (const match of content.matchAll(re)) {
    const label = match[1];
    const type = match[2];
    const id = match[3];
    if (!label || !type || !id) continue;
    mentions.push({ label, type, id });
  }
  return mentions;
}

// A blocked trigger outcome from the server intentionally omits the target's
// name (enumeration-safety: the wire never reveals a private target). But the
// CLIENT already rendered that name in its own draft/comment, so the composer
// and the post-send toast can label a blocked mention from the markup the user
// typed — no new disclosure. Returns a `${target_type}:${target_id}` -> label map.
export function mentionLabelsByTarget(content: string): Map<string, string> {
  const labels = new Map<string, string>();
  for (const { label, type, id } of parseMentions(content)) {
    labels.set(`${type}:${id}`, label);
  }
  return labels;
}

// The label a user typed for one blocked outcome, or undefined when it cannot be
// correlated (e.g. the mention was edited away). Callers fall back to a
// name-free reason so the warning is still shown.
export function blockedTriggerLabel(
  outcome: { target_type: string; target_id: string },
  labels: Map<string, string>,
): string | undefined {
  return labels.get(`${outcome.target_type}:${outcome.target_id}`);
}

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

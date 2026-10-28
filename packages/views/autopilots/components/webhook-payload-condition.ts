import type {
  WebhookPayloadCondition,
  WebhookPayloadConditionValue,
} from "@multica/core/types";

export type ConditionOp = WebhookPayloadCondition["op"];

export const CONDITION_OPS: ConditionOp[] = ["equals", "not_equals", "contains", "in", "exists"];

// Mirrors the server cap so the editor never builds a row the API rejects.
export const MAX_CONDITIONS = 20;

// formatConditionValue shows strings quoted and booleans/numbers bare, so
// the stored type stays visible.
export function formatConditionValue(c: WebhookPayloadCondition): string {
  if (c.op === "in") return (c.values ?? []).map((v) => JSON.stringify(v)).join(", ");
  return JSON.stringify(c.value);
}

// formatCondition is the full API form, used as the row's tooltip.
export function formatCondition(c: WebhookPayloadCondition): string {
  if (c.op === "exists") return `${c.path} exists`;
  const value = c.op === "in" ? `[${formatConditionValue(c)}]` : formatConditionValue(c);
  return `${c.path} ${c.op}${c.ignore_case ? "/i" : ""} ${value}`;
}

// parseConditionValue reads JSON scalars (true, 42, "42") and falls back to
// plain text, so `main` and `"main"` both mean the string main.
export function parseConditionValue(raw: string): WebhookPayloadConditionValue {
  const trimmed = raw.trim();
  try {
    const parsed: unknown = JSON.parse(trimmed);
    if (
      typeof parsed === "string" ||
      typeof parsed === "number" ||
      typeof parsed === "boolean"
    ) {
      return parsed;
    }
  } catch {
    // Not JSON: plain text.
  }
  return trimmed;
}

export interface ConditionDraft {
  path: string;
  op: ConditionOp;
  value: string;
  ignoreCase: boolean;
}

export const EMPTY_CONDITION_DRAFT: ConditionDraft = { path: "", op: "equals", value: "", ignoreCase: false };

// buildCondition returns null while the draft can't produce a row the
// server accepts.
export function buildCondition(draft: ConditionDraft): WebhookPayloadCondition | null {
  const path = draft.path.trim();
  if (!path || path.split(".").some((seg) => seg === "")) return null;
  if (draft.op === "exists") return { path, op: "exists" };
  if (!draft.value.trim()) return null;
  const condition: WebhookPayloadCondition = { path, op: draft.op };
  if (draft.op === "in") {
    const values = draft.value
      .split(",")
      .map((v) => v.trim())
      .filter((v) => v.length > 0)
      .map(parseConditionValue);
    if (values.length === 0) return null;
    condition.values = values;
  } else if (draft.op === "contains") {
    const parsed = parseConditionValue(draft.value);
    condition.value = typeof parsed === "string" ? parsed : draft.value.trim();
  } else {
    condition.value = parseConditionValue(draft.value);
  }
  if (draft.ignoreCase) condition.ignore_case = true;
  return condition;
}

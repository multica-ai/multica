import type {
  WebhookEventFilter,
  WebhookPayloadCondition,
  WebhookPayloadConditionValue,
} from "@multica/core/types";

export type ConditionOp = WebhookPayloadCondition["op"];

export const CONDITION_OPS: ConditionOp[] = ["equals", "not_equals", "contains", "in", "exists"];

// Mirror server limits so the editor never builds a row the API rejects.
export const MAX_CONDITIONS = 20;
export const MAX_CONDITION_PATH_LENGTH = 256;
export const MAX_CONDITION_VALUES = 100;

// formatConditionValue shows strings quoted and booleans/numbers bare, so
// the stored type stays visible.
export function formatConditionValue(c: WebhookPayloadCondition): string {
  if (c.op === "in") return (c.values ?? []).map((v) => JSON.stringify(v)).join(", ");
  return JSON.stringify(c.value);
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

// splitConditionValues splits an "is one of" list at its commas, except for
// commas inside a value written as a quoted string ("fix, docs"), so a value
// that holds a comma survives the trip through the text input. A value that
// opens a quote without closing it is read as plain text up to its comma.
export function splitConditionValues(raw: string): string[] {
  const values: string[] = [];
  let from = 0;
  for (;;) {
    let start = from;
    while (start < raw.length && /\s/.test(raw[start]!)) start++;
    let scanFrom = start;
    if (raw[start] === '"') {
      let i = start + 1;
      while (i < raw.length && raw[i] !== '"') i += raw[i] === "\\" ? 2 : 1;
      if (i < raw.length) scanFrom = i + 1;
    }
    const comma = raw.indexOf(",", scanFrom);
    const value = raw.slice(start, comma === -1 ? raw.length : comma).trim();
    if (value) values.push(value);
    if (comma === -1) return values;
    from = comma + 1;
  }
}

export interface ConditionDraft {
  path: string;
  op: ConditionOp;
  value: string;
  ignoreCase: boolean;
}

// buildCondition returns null while the draft can't produce a row the
// server accepts.
export function buildCondition(draft: ConditionDraft): WebhookPayloadCondition | null {
  const path = draft.path.trim();
  if (!path || path.split(".").some((seg) => seg === "")) return null;
  if (Array.from(path).length > MAX_CONDITION_PATH_LENGTH) return null;
  if (draft.op === "exists") return { path, op: "exists" };
  if (!draft.value.trim()) return null;
  const condition: WebhookPayloadCondition = { path, op: draft.op };
  if (draft.op === "in") {
    const values = splitConditionValues(draft.value);
    if (values.length === 0 || values.length > MAX_CONDITION_VALUES) return null;
    condition.values = values.map(parseConditionValue);
  } else if (draft.op === "contains") {
    const parsed = parseConditionValue(draft.value);
    condition.value = typeof parsed === "string" ? parsed : draft.value.trim();
  } else {
    condition.value = parseConditionValue(draft.value);
  }
  if (draft.ignoreCase) condition.ignore_case = true;
  return condition;
}

// serializeEventFilters returns a stable JSON string so an editor can compare
// the filters it holds against the snapshot taken on open without depending
// on reference equality. Normalizes empty actions and conditions to [] so
// omitted-vs-explicit-empty doesn't show as a phantom change.
export function serializeEventFilters(filters: WebhookEventFilter[]): string {
  return JSON.stringify(
    filters.map((f) => ({
      event: f.event,
      actions: f.actions ?? [],
      conditions: f.conditions ?? [],
    })),
  );
}

import type {
  WebhookEventFilter,
  WebhookPayloadCondition,
  WebhookPayloadConditionValue,
} from "@multica/core/types";
import {
  MAX_CONDITION_PATH_LENGTH,
  MAX_CONDITION_VALUES,
  buildCondition,
  parseConditionValue,
  splitConditionValues,
  type ConditionOp,
} from "./webhook-payload-condition";

// The editor edits rules and conditions in place, so what it holds is text as
// typed, including rows that are not valid yet. Nothing is dropped on the way
// to a save: an incomplete row blocks it instead (resolveEventFilterDrafts).

export interface ConditionRowDraft {
  key: string;
  path: string;
  op: ConditionOp;
  value: string;
  ignoreCase: boolean;
}

export interface EventFilterDraft {
  key: string;
  event: string;
  actions: string;
  conditions: ConditionRowDraft[];
}

export type ConditionFieldError =
  | "path_required"
  | "path_invalid"
  | "value_required"
  | "values_too_many";

export interface EventFilterDraftErrors {
  // Keyed by rule draft key.
  events: Record<string, "event_required">;
  // Keyed by condition draft key; one error per field.
  paths: Record<string, ConditionFieldError>;
  values: Record<string, ConditionFieldError>;
}

let nextDraftKey = 0;
const draftKey = (prefix: string) => `${prefix}${++nextDraftKey}`;

export function newRuleDraft(): EventFilterDraft {
  return { key: draftKey("r"), event: "", actions: "", conditions: [] };
}

export function newConditionDraft(): ConditionRowDraft {
  return { key: draftKey("c"), path: "", op: "equals", value: "", ignoreCase: false };
}

// conditionValueToText writes a stored value back the way it would have to be
// typed to come out the same. Strings stay bare unless bare text would read
// differently: as another type ("false", "42"), trimmed ("  x  "), as nothing
// (""), or, inside an "is one of" list, as two values ("fix, docs"). Those
// keep their quotes. Anything else would come back changed, and a save that
// only renamed the webhook would quietly rewrite its rule.
export function conditionValueToText(value: WebhookPayloadConditionValue, inList = false): string {
  if (typeof value !== "string") return String(value);
  const bareReadsBack =
    value.trim() !== "" && parseConditionValue(value) === value && !(inList && value.includes(","));
  return bareReadsBack ? value : JSON.stringify(value);
}

function conditionToDraft(c: WebhookPayloadCondition): ConditionRowDraft {
  const value =
    c.op === "exists"
      ? ""
      : c.op === "in"
        ? (c.values ?? []).map((v) => conditionValueToText(v, true)).join(", ")
        : c.value === undefined
          ? ""
          : conditionValueToText(c.value);
  return {
    key: draftKey("c"),
    path: c.path,
    op: c.op,
    value,
    ignoreCase: c.ignore_case === true,
  };
}

export function draftsFromEventFilters(filters: WebhookEventFilter[]): EventFilterDraft[] {
  return filters.map((f) => ({
    key: draftKey("r"),
    event: f.event,
    actions: (f.actions ?? []).join(", "),
    conditions: (f.conditions ?? []).map(conditionToDraft),
  }));
}

function conditionErrors(
  row: ConditionRowDraft,
): { path?: ConditionFieldError; value?: ConditionFieldError } {
  const errors: { path?: ConditionFieldError; value?: ConditionFieldError } = {};
  const path = row.path.trim();
  if (!path) errors.path = "path_required";
  else if (
    path.split(".").some((seg) => seg === "") ||
    Array.from(path).length > MAX_CONDITION_PATH_LENGTH
  ) {
    errors.path = "path_invalid";
  }
  if (row.op !== "exists") {
    const values = splitConditionValues(row.value);
    if (row.op === "in" ? values.length === 0 : !row.value.trim()) errors.value = "value_required";
    else if (row.op === "in" && values.length > MAX_CONDITION_VALUES) errors.value = "values_too_many";
  }
  return errors;
}

export interface ResolvedEventFilters {
  filters: WebhookEventFilter[];
  errors: EventFilterDraftErrors;
  valid: boolean;
}

// resolveEventFilterDrafts turns the editor's text into the filters a save
// sends, and reports every field that keeps it from being one.
export function resolveEventFilterDrafts(drafts: EventFilterDraft[]): ResolvedEventFilters {
  const errors: EventFilterDraftErrors = { events: {}, paths: {}, values: {} };
  const filters: WebhookEventFilter[] = [];
  for (const draft of drafts) {
    const event = draft.event.trim();
    if (!event) errors.events[draft.key] = "event_required";
    const actions = draft.actions
      .split(",")
      .map((a) => a.trim())
      .filter((a) => a.length > 0);
    const conditions: WebhookPayloadCondition[] = [];
    for (const row of draft.conditions) {
      const rowErrors = conditionErrors(row);
      if (rowErrors.path) errors.paths[row.key] = rowErrors.path;
      if (rowErrors.value) errors.values[row.key] = rowErrors.value;
      const condition = buildCondition(row);
      if (condition) conditions.push(condition);
    }
    const filter: WebhookEventFilter = { event };
    if (actions.length > 0) filter.actions = actions;
    if (conditions.length > 0) filter.conditions = conditions;
    filters.push(filter);
  }
  const valid =
    Object.keys(errors.events).length === 0 &&
    Object.keys(errors.paths).length === 0 &&
    Object.keys(errors.values).length === 0;
  return { filters, errors, valid };
}

import { isPropertyOperatorFilter, type PropertyFilterOp, type PropertyFilterValue } from "../../types";
import {
  FILTER_NONE_VALUE,
  isPropertyCondition,
  type AdvancedFilterCondition,
  type NegatableFilterDimension,
} from "./filter-tree";

/** The "no value" member of a property condition. */
export const PROPERTY_FILTER_NONE_VALUE = "__none__";

/**
 * What a condition says about its field. Every operator is a reading of the
 * stored `negated` + `values`, so trees, saved views and the server query
 * keep one shape:
 *
 * - `is` / `is_not`: the values, plain or negated.
 * - `is_empty` / `is_not_empty`: only the field's "no value" member.
 * - `contains`, comparisons, `before` / `after`: one operator member;
 *   `not_contains` is a negated `contains`.
 */
export type FilterOperator =
  | "is"
  | "is_not"
  | "contains"
  | "not_contains"
  | "gt"
  | "gte"
  | "lt"
  | "lte"
  | "before"
  | "after"
  | "is_empty"
  | "is_not_empty";

/**
 * How a field holds values, which decides its operators and value editor:
 * `option` is one pick per issue, `multi` a set of picks (labels, multi
 * select), `list` a set of free-form strings, the rest single scalars.
 */
export type FilterFieldKind = "option" | "multi" | "list" | "text" | "number" | "date";

export interface FilterFieldSpec {
  kind: FilterFieldKind;
  /** Whether the field can be unset, so "is empty" means something. */
  nullable: boolean;
}

const DIMENSION_SPECS: Record<NegatableFilterDimension, FilterFieldSpec> = {
  status: { kind: "option", nullable: false },
  priority: { kind: "option", nullable: false },
  assignee: { kind: "option", nullable: true },
  creator: { kind: "option", nullable: false },
  project: { kind: "option", nullable: true },
  projectStatus: { kind: "option", nullable: false },
  label: { kind: "multi", nullable: false },
  relation: { kind: "option", nullable: false },
  dueDate: { kind: "option", nullable: true },
};

const PROPERTY_KINDS: Record<string, FilterFieldKind> = {
  select: "option",
  actor: "option",
  checkbox: "option",
  multi_select: "multi",
  multi_actor: "multi",
  multi_text: "list",
  multi_url: "list",
  text: "text",
  url: "text",
  number: "number",
  date: "date",
};

/** A condition's field spec. A property needs its definition's type; an
 *  unknown or missing one reads as a plain option field. */
export function filterFieldSpec(condition: AdvancedFilterCondition, propertyType?: string): FilterFieldSpec {
  if (!isPropertyCondition(condition)) return DIMENSION_SPECS[condition.field];
  return { kind: (propertyType && PROPERTY_KINDS[propertyType]) || "option", nullable: true };
}

const KIND_OPERATORS: Record<FilterFieldKind, readonly FilterOperator[]> = {
  option: ["is", "is_not"],
  multi: ["is", "is_not"],
  list: ["is", "is_not"],
  text: ["is", "is_not", "contains", "not_contains"],
  number: ["is", "is_not", "gt", "gte", "lt", "lte"],
  date: ["is", "is_not", "before", "after"],
};

/** The operators a field offers, in menu order. */
export function filterOperatorsFor(spec: FilterFieldSpec): FilterOperator[] {
  return [...KIND_OPERATORS[spec.kind], ...(spec.nullable ? (["is_empty", "is_not_empty"] as const) : [])];
}

/** Operators that carry no value. */
export function isEmptinessOperator(op: FilterOperator): boolean {
  return op === "is_empty" || op === "is_not_empty";
}

/** Kinds whose value is typed rather than picked. */
export function isTypedValueKind(kind: FilterFieldKind): boolean {
  return kind === "text" || kind === "number" || kind === "date" || kind === "list";
}

function noneValue(condition: AdvancedFilterCondition): string {
  return isPropertyCondition(condition) ? PROPERTY_FILTER_NONE_VALUE : FILTER_NONE_VALUE;
}

function operatorMember(values: readonly PropertyFilterValue[]) {
  return values.find(isPropertyOperatorFilter);
}

/** The operator a stored condition reads as. */
export function conditionOperator(condition: AdvancedFilterCondition, spec: FilterFieldSpec): FilterOperator {
  const values = condition.values as readonly PropertyFilterValue[];
  const member = operatorMember(values);
  if (member) return member.op === "contains" && condition.negated ? "not_contains" : member.op;
  if (spec.nullable && values.length === 1 && values[0] === noneValue(condition)) {
    return condition.negated ? "is_not_empty" : "is_empty";
  }
  return condition.negated ? "is_not" : "is";
}

/** The typed value of a text, number, date or list condition. */
export function conditionText(condition: AdvancedFilterCondition): string {
  const values = condition.values as readonly PropertyFilterValue[];
  const member = operatorMember(values);
  if (member) return member.value;
  const none = noneValue(condition);
  return values.find((v): v is string => typeof v === "string" && v !== none) ?? "";
}

function storedOp(op: FilterOperator): PropertyFilterOp | null {
  switch (op) {
    case "is":
    case "is_not":
    case "is_empty":
    case "is_not_empty":
      return null;
    case "not_contains":
      return "contains";
    default:
      return op;
  }
}

/**
 * A typed value under an operator. "No value" stays a member alongside it,
 * the way the simple property filter composes the two.
 */
export function withConditionText(
  condition: AdvancedFilterCondition,
  op: FilterOperator,
  text: string,
): AdvancedFilterCondition {
  const value = text.trim();
  const none = noneValue(condition);
  const keepNone = (condition.values as readonly PropertyFilterValue[]).includes(none) && !isEmptinessOperator(op);
  const stored = storedOp(op);
  const member: PropertyFilterValue | null =
    value && value !== none && !isEmptinessOperator(op) ? (stored ? { op: stored, value } : value) : null;
  return {
    ...condition,
    negated: op === "is_not" || op === "not_contains",
    values: [...(member ? [member] : []), ...(keepNone ? [none] : [])],
  } as AdvancedFilterCondition;
}

/**
 * The condition under another operator. Picked values survive a switch
 * between "is" and "is not", a typed value survives any switch between
 * value operators, and "is empty" replaces whatever was there.
 */
export function withConditionOperator(
  condition: AdvancedFilterCondition,
  spec: FilterFieldSpec,
  op: FilterOperator,
): AdvancedFilterCondition {
  const none = noneValue(condition);
  if (isEmptinessOperator(op)) {
    return { ...condition, negated: op === "is_not_empty", values: [none] } as AdvancedFilterCondition;
  }
  const current = conditionOperator(condition, spec);
  if (isTypedValueKind(spec.kind)) {
    return withConditionText(condition, op, isEmptinessOperator(current) ? "" : conditionText(condition));
  }
  return {
    ...condition,
    negated: op === "is_not",
    values: isEmptinessOperator(current) ? [] : condition.values,
  } as AdvancedFilterCondition;
}

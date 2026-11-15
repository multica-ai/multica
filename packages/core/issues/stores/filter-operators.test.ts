import { describe, expect, it } from "vitest";
import { createFilterCondition, propertyFilterField } from "./filter-tree";
import {
  PROPERTY_FILTER_NONE_VALUE,
  conditionOperator,
  conditionText,
  filterFieldSpec,
  filterOperatorsFor,
  withConditionOperator,
  withConditionText,
} from "./filter-operators";

const estimate = propertyFilterField("estimate");

describe("filterOperatorsFor", () => {
  it("offers operators by how the field holds values", () => {
    const status = createFilterCondition("status");
    expect(filterOperatorsFor(filterFieldSpec(status))).toEqual(["is", "is_not"]);
    expect(filterOperatorsFor(filterFieldSpec(createFilterCondition("assignee")))).toEqual([
      "is",
      "is_not",
      "is_empty",
      "is_not_empty",
    ]);
    const number = createFilterCondition(estimate);
    expect(filterOperatorsFor(filterFieldSpec(number, "number"))).toEqual([
      "is", "is_not", "gt", "gte", "lt", "lte", "is_empty", "is_not_empty",
    ]);
    expect(filterOperatorsFor(filterFieldSpec(number, "url"))).toContain("not_contains");
    expect(filterFieldSpec(createFilterCondition("label")).kind).toBe("multi");
    // A type this build doesn't know still gets the basic operators.
    expect(filterFieldSpec(number, "formula")).toEqual({ kind: "option", nullable: true });
  });
});

describe("conditionOperator", () => {
  it("reads operators off the stored values", () => {
    const text = filterFieldSpec(createFilterCondition(estimate), "text");
    expect(conditionOperator(createFilterCondition("status", ["todo"], true), filterFieldSpec(createFilterCondition("status")))).toBe("is_not");
    expect(conditionOperator(createFilterCondition(estimate, [{ op: "contains", value: "a" }], true), text)).toBe("not_contains");
    expect(conditionOperator(createFilterCondition(estimate, [{ op: "gte", value: "3" }]), text)).toBe("gte");
    expect(conditionOperator(createFilterCondition(estimate, [PROPERTY_FILTER_NONE_VALUE], true), text)).toBe("is_not_empty");
    const assignee = createFilterCondition("assignee", ["none"]);
    expect(conditionOperator(assignee, filterFieldSpec(assignee))).toBe("is_empty");
    // "No assignee or Lambda" is a value list, not emptiness.
    const either = createFilterCondition("assignee", ["none", "agent:a1"]);
    expect(conditionOperator(either, filterFieldSpec(either))).toBe("is");
  });
});

describe("withConditionOperator", () => {
  it("keeps picked values between is and is not", () => {
    const label = createFilterCondition("label", ["bug", "ui"]);
    const next = withConditionOperator(label, filterFieldSpec(label), "is_not");
    expect(next).toMatchObject({ negated: true, values: ["bug", "ui"] });
  });

  it("stores emptiness as the field's no-value member", () => {
    const project = createFilterCondition("project", ["p1"]);
    expect(withConditionOperator(project, filterFieldSpec(project), "is_not_empty")).toMatchObject({
      negated: true,
      values: ["none"],
    });
    const empty = createFilterCondition("project", ["none"]);
    expect(withConditionOperator(empty, filterFieldSpec(empty), "is")).toMatchObject({ negated: false, values: [] });
  });

  it("carries a typed value across operators", () => {
    const condition = createFilterCondition(estimate, ["5", PROPERTY_FILTER_NONE_VALUE]);
    const spec = filterFieldSpec(condition, "number");
    const gt = withConditionOperator(condition, spec, "gt");
    expect(gt).toMatchObject({ negated: false, values: [{ op: "gt", value: "5" }, PROPERTY_FILTER_NONE_VALUE] });
    expect(withConditionOperator(gt, spec, "is_not")).toMatchObject({
      negated: true,
      values: ["5", PROPERTY_FILTER_NONE_VALUE],
    });
    const notContains = withConditionOperator(createFilterCondition(estimate, ["api"]), spec, "not_contains");
    expect(notContains).toMatchObject({ negated: true, values: [{ op: "contains", value: "api" }] });
    expect(conditionText(notContains)).toBe("api");
  });
});

describe("withConditionText", () => {
  it("drops an empty value so the condition filters nothing", () => {
    const condition = createFilterCondition(estimate, [{ op: "contains", value: "a" }]);
    expect(withConditionText(condition, "contains", "  ").values).toEqual([]);
    expect(withConditionText(condition, "after", "2026-01-01").values).toEqual([{ op: "after", value: "2026-01-01" }]);
  });
});

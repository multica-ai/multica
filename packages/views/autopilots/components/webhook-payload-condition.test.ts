// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  EMPTY_CONDITION_DRAFT,
  buildCondition,
  formatCondition,
  formatConditionValue,
  parseConditionValue,
} from "./webhook-payload-condition";

describe("parseConditionValue", () => {
  it.each([
    ["main", "main"],
    ['"main"', "main"],
    ["true", true],
    ["false", false],
    ["42", 42],
    ['"42"', "42"],
    ["  spaced  ", "spaced"],
    ["{\"a\":1}", "{\"a\":1}"],
  ])("reads %s", (raw, want) => {
    expect(parseConditionValue(raw)).toEqual(want);
  });
});

describe("buildCondition", () => {
  const draft = (patch: Partial<typeof EMPTY_CONDITION_DRAFT>) => ({
    ...EMPTY_CONDITION_DRAFT,
    ...patch,
  });

  it("needs a path without empty segments", () => {
    expect(buildCondition(draft({ value: "x" }))).toBeNull();
    expect(buildCondition(draft({ path: "a..b", value: "x" }))).toBeNull();
  });

  it("needs a value except for exists", () => {
    expect(buildCondition(draft({ path: "draft" }))).toBeNull();
    expect(buildCondition(draft({ path: "merged_at", op: "exists", value: "ignored", ignoreCase: true }))).toEqual({
      path: "merged_at",
      op: "exists",
    });
  });

  it("types equals values as JSON scalars", () => {
    expect(buildCondition(draft({ path: "pull_request.draft", value: "false" }))).toEqual({
      path: "pull_request.draft",
      op: "equals",
      value: false,
    });
  });

  it("keeps contains values as text", () => {
    expect(buildCondition(draft({ path: "title", op: "contains", value: "42", ignoreCase: true }))).toEqual({
      path: "title",
      op: "contains",
      value: "42",
      ignore_case: true,
    });
  });

  it("splits in values on commas", () => {
    expect(buildCondition(draft({ path: "base.ref", op: "in", value: "main, release, " }))).toEqual({
      path: "base.ref",
      op: "in",
      values: ["main", "release"],
    });
    expect(buildCondition(draft({ path: "base.ref", op: "in", value: " , " }))).toBeNull();
  });
});

describe("formatCondition", () => {
  it("renders each op as code", () => {
    expect(formatCondition({ path: "a", op: "equals", value: false })).toBe("a equals false");
    expect(formatCondition({ path: "a", op: "contains", value: "x", ignore_case: true })).toBe('a contains/i "x"');
    expect(formatCondition({ path: "a", op: "in", values: ["x", 1] })).toBe('a in ["x", 1]');
    expect(formatCondition({ path: "a", op: "exists" })).toBe("a exists");
  });
});

describe("formatConditionValue", () => {
  it("quotes strings and leaves booleans and numbers bare", () => {
    expect(formatConditionValue({ path: "a", op: "equals", value: "x" })).toBe('"x"');
    expect(formatConditionValue({ path: "a", op: "equals", value: false })).toBe("false");
    expect(formatConditionValue({ path: "a", op: "in", values: ["x", 1] })).toBe('"x", 1');
  });
});

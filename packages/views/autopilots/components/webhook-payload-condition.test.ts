// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  buildCondition,
  formatConditionValue,
  parseConditionValue,
  splitConditionValues,
  type ConditionDraft,
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
  const draft = (patch: Partial<ConditionDraft>): ConditionDraft => ({
    path: "",
    op: "equals",
    value: "",
    ignoreCase: false,
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

  it.each(["a", "界", "😀"])("allows 256 path characters and rejects 257 (%s)", (character) => {
    expect(buildCondition(draft({ path: character.repeat(256), op: "exists" }))).not.toBeNull();
    expect(buildCondition(draft({ path: character.repeat(257), op: "exists" }))).toBeNull();
  });

  it("allows 100 in values and rejects 101", () => {
    const values = Array.from({ length: 101 }, (_, i) => String(i));
    expect(buildCondition(draft({ path: "number", op: "in", value: values.slice(0, 100).join(",") }))?.values).toHaveLength(100);
    expect(buildCondition(draft({ path: "number", op: "in", value: values.join(",") }))).toBeNull();
  });
});

describe("formatConditionValue", () => {
  it("quotes strings and leaves booleans and numbers bare", () => {
    expect(formatConditionValue({ path: "a", op: "equals", value: "x" })).toBe('"x"');
    expect(formatConditionValue({ path: "a", op: "equals", value: false })).toBe("false");
    expect(formatConditionValue({ path: "a", op: "in", values: ["x", 1] })).toBe('"x", 1');
  });
});

describe("splitConditionValues", () => {
  it.each([
    ["main, release", ["main", "release"]],
    ['"fix, docs", chore', ['"fix, docs"', "chore"]],
    ['"a\\", b", c', ['"a\\", b"', "c"]],
    ['a"b, c', ['a"b', "c"]],
    ['"open, x', ['"open', "x"]],
    ['"", main', ['""', "main"]],
    [" , main ,", ["main"]],
  ] as const)("splits %s outside quoted values", (raw, want) => {
    expect(splitConditionValues(raw)).toEqual(want);
  });
});

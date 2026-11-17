// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { WebhookEventFilter } from "@multica/core/types";
import {
  conditionValueToText,
  draftsFromEventFilters,
  newConditionDraft,
  newRuleDraft,
  resolveEventFilterDrafts,
} from "./webhook-event-filter-draft";

describe("conditionValueToText", () => {
  it.each([
    ["main", "main"],
    [false, "false"],
    [42, "42"],
    ["false", '"false"'],
    ["", '""'],
    ["42", '"42"'],
    ['"quoted"', '"\\"quoted\\""'],
  ] as const)("writes %j as %s", (value, text) => {
    expect(conditionValueToText(value)).toBe(text);
  });
});

describe("draftsFromEventFilters → resolveEventFilterDrafts", () => {
  it("round-trips every saved shape unchanged", () => {
    const filters: WebhookEventFilter[] = [
      {
        event: "pull_request",
        actions: ["opened", "synchronize"],
        conditions: [
          { path: "pull_request.draft", op: "equals", value: false },
          { path: "pull_request.number", op: "not_equals", value: 7 },
          { path: "pull_request.title", op: "contains", value: "false", ignore_case: true },
          { path: "pull_request.base.ref", op: "in", values: ["main", "release", 3] },
          // Commas and empty strings inside a list, and an empty string on
          // its own, are values the API accepts that bare text can't carry.
          { path: "pull_request.title", op: "in", values: ["fix, docs", "chore", "", '"quoted"'] },
          { path: "pull_request.body", op: "equals", value: "" },
          { path: "pull_request.head.ref", op: "not_equals", value: "  padded  " },
          { path: "pull_request.labels.*.name", op: "exists" },
        ],
      },
      { event: "issues" },
    ];

    const resolved = resolveEventFilterDrafts(draftsFromEventFilters(filters));

    expect(resolved.valid).toBe(true);
    expect(resolved.filters).toEqual(filters);
  });
});

describe("resolveEventFilterDrafts", () => {
  it("reports a rule without an event instead of dropping it", () => {
    const rule = newRuleDraft();
    const resolved = resolveEventFilterDrafts([rule]);

    expect(resolved.valid).toBe(false);
    expect(resolved.errors.events).toEqual({ [rule.key]: "event_required" });
  });

  it("reports each incomplete condition field", () => {
    const empty = newConditionDraft();
    const badPath = { ...newConditionDraft(), path: "a..b", value: "x" };
    const tooMany = {
      ...newConditionDraft(),
      path: "a",
      op: "in" as const,
      value: Array.from({ length: 101 }, (_, i) => `v${i}`).join(", "),
    };
    const exists = { ...newConditionDraft(), path: "a", op: "exists" as const };
    const rule = { ...newRuleDraft(), event: "issues", conditions: [empty, badPath, tooMany, exists] };

    const { errors, valid } = resolveEventFilterDrafts([rule]);

    expect(valid).toBe(false);
    expect(errors.paths).toEqual({ [empty.key]: "path_required", [badPath.key]: "path_invalid" });
    expect(errors.values).toEqual({ [empty.key]: "value_required", [tooMany.key]: "values_too_many" });
  });

  it("resolves no rules to an empty list, which accepts every event", () => {
    expect(resolveEventFilterDrafts([])).toEqual({
      filters: [],
      errors: { events: {}, paths: {}, values: {} },
      valid: true,
    });
  });
});

// @vitest-environment node
import { describe, expect, it } from "vitest";
import { baselineFromQuery } from "./baseline";
import { propertyFilterValueKey } from "../types";

// The property-filter branch of baselineFromQuery: saved-view members must
// survive a round-trip as-is when the client can represent them, and drop
// silently when it cannot (hand-edited blob, operator a future client added).
describe("baselineFromQuery property filters", () => {
  const textId = "prop-note";
  const numId = "prop-estimate";

  it("passes strings and known operator objects through untouched", () => {
    const members = [
      "hello",
      "__none__",
      { op: "contains", value: "foo" },
      { op: "gte", value: "3.5" },
    ];
    const baseline = baselineFromQuery({
      propertyFilters: { [textId]: members },
    });

    expect(baseline.raw.propertyFilters[textId]).toEqual(members);
    // Membership keys: strings are their own key; operators get their
    // canonical key so Set lookups agree with the store.
    expect([...baseline.property.get(textId)!]).toEqual(
      members.map((m) => propertyFilterValueKey(m as never)),
    );
  });

  it("drops members the store cannot represent", () => {
    const baseline = baselineFromQuery({
      propertyFilters: {
        [textId]: [
          "keep",
          { op: "regex", value: "x" }, // unknown op
          { op: 42, value: "x" }, // non-string op
          { op: "contains", value: 7 }, // non-string value
          { nope: true }, // not an operator shape
          7, // not a string
          null,
        ],
        [numId]: [{ op: "regex", value: "x" }], // everything dropped
      },
    });

    expect(baseline.raw.propertyFilters[textId]).toEqual(["keep"]);
    // A definition with no representable members is not a filter at all.
    expect(baseline.raw.propertyFilters[numId]).toBeUndefined();
    expect(baseline.property.has(numId)).toBe(false);
  });

  it("treats a non-array member list as no filter", () => {
    const baseline = baselineFromQuery({
      propertyFilters: { [textId]: "hello" },
    });
    expect(baseline.raw.propertyFilters).toEqual({});
    expect(baseline.property.size).toBe(0);
  });
});

// Saved views predate the project-status dimension, so every read path has to
// treat a missing key as "no filter" rather than as a value.
describe("baselineFromQuery project status filters", () => {
  it("keeps known project statuses", () => {
    const baseline = baselineFromQuery({
      projectStatusFilters: ["in_progress", "completed"],
    });
    expect(baseline.raw.projectStatusFilters).toEqual(["in_progress", "completed"]);
    expect(baseline.projectStatus.has("in_progress")).toBe(true);
  });

  it("treats a view saved before the dimension existed as no filter", () => {
    const baseline = baselineFromQuery({ projectFilters: ["p-1"] });
    expect(baseline.raw.projectStatusFilters).toEqual([]);
    expect(baseline.projectStatus.size).toBe(0);
  });

  it("drops values the store cannot represent", () => {
    const baseline = baselineFromQuery({
      // "backlog" is an issue status; the project lifecycle has no such value.
      projectStatusFilters: ["in_progress", "backlog", 7, null],
    });
    expect(baseline.raw.projectStatusFilters).toEqual(["in_progress"]);
  });
});

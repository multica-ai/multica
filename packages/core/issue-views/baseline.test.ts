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

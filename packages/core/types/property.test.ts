// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  actorRefsFromValue,
  actorRefValuesFromValue,
  hasUnknownActorRef,
  formatActorRef,
  isActorPropertyType,
  isFilterablePropertyType,
  isKnownPropertyType,
  isScalarPropertyType,
  parseActorRef,
} from "./property";

const MEMBER = "11111111-2222-3333-4444-555555555555";
const SECOND_MEMBER = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee";
const AGENT = "66666666-7777-8888-9999-000000000000";

describe("isKnownPropertyType", () => {
  it("accepts the actor types", () => {
    expect(isKnownPropertyType("actor")).toBe(true);
    expect(isKnownPropertyType("multi_actor")).toBe(true);
  });

  it("still rejects unknown types", () => {
    expect(isKnownPropertyType("relation")).toBe(false);
  });
});

describe("isActorPropertyType", () => {
  it("covers both actor types and nothing else", () => {
    expect(isActorPropertyType("actor")).toBe(true);
    expect(isActorPropertyType("multi_actor")).toBe(true);
    expect(isActorPropertyType("select")).toBe(false);
    expect(isActorPropertyType("multi_select")).toBe(false);
  });
});

describe("isFilterablePropertyType", () => {
  it("admits every type the filter menu supports", () => {
    for (const type of [
      "select",
      "multi_select",
      "checkbox",
      "text",
      "number",
      "date",
      "url",
      "actor",
      "multi_actor",
    ]) {
      expect(isFilterablePropertyType(type)).toBe(true);
    }
  });

  it("rejects unknown types", () => {
    expect(isFilterablePropertyType("relation")).toBe(false);
  });
});

describe("isScalarPropertyType", () => {
  it("covers the four single-valued types and nothing else", () => {
    for (const type of ["text", "number", "date", "url"]) {
      expect(isScalarPropertyType(type)).toBe(true);
    }
    expect(isScalarPropertyType("select")).toBe(false);
    expect(isScalarPropertyType("checkbox")).toBe(false);
    expect(isScalarPropertyType("actor")).toBe(false);
  });
});

describe("parseActorRef", () => {
  it("parses member references", () => {
    expect(parseActorRef(`member:${MEMBER}`)).toEqual({ kind: "member", id: MEMBER });
  });

  it("rejects kinds outside the V1 range", () => {
    // Agents and squads are assignable but deliberately not referenceable.
    expect(parseActorRef(`agent:${AGENT}`)).toBeNull();
    expect(parseActorRef(`squad:${AGENT}`)).toBeNull();
    expect(parseActorRef(`user:${MEMBER}`)).toBeNull();
  });

  it("rejects malformed input", () => {
    expect(parseActorRef(MEMBER)).toBeNull();
    expect(parseActorRef("member:")).toBeNull();
    expect(parseActorRef(":" + MEMBER)).toBeNull();
    expect(parseActorRef(42)).toBeNull();
    expect(parseActorRef(undefined)).toBeNull();
  });
});

describe("formatActorRef", () => {
  it("round-trips through parseActorRef", () => {
    const ref = formatActorRef("member", MEMBER);
    expect(ref).toBe(`member:${MEMBER}`);
    expect(parseActorRef(ref)).toEqual({ kind: "member", id: MEMBER });
  });
});

describe("actorRefsFromValue", () => {
  it("reads a single actor value as a one-item list", () => {
    expect(actorRefsFromValue(`member:${MEMBER}`)).toEqual([{ kind: "member", id: MEMBER }]);
  });

  it("preserves multi_actor order rather than sorting", () => {
    expect(actorRefsFromValue([`member:${SECOND_MEMBER}`, `member:${MEMBER}`])).toEqual([
      { kind: "member", id: SECOND_MEMBER },
      { kind: "member", id: MEMBER },
    ]);
  });

  it("drops entries a newer backend may ship instead of throwing", () => {
    // A backend that later widens the kind set must not blank the whole value
    // on an older client — the unknown entries drop, the known ones render.
    expect(actorRefsFromValue([`member:${MEMBER}`, `agent:${AGENT}`, "garbage"])).toEqual([
      { kind: "member", id: MEMBER },
    ]);
  });

  it("returns an empty list for unset or wrongly-typed values", () => {
    expect(actorRefsFromValue(undefined)).toEqual([]);
    expect(actorRefsFromValue(true)).toEqual([]);
    expect(actorRefsFromValue(7)).toEqual([]);
  });
});

describe("actorRefValuesFromValue", () => {
  it("keeps kinds this build does not know", () => {
    // The edit path must round-trip through this: a newer backend widening the
    // kind set must not have its values deleted by an older client.
    expect(actorRefValuesFromValue([`member:${MEMBER}`, `agent:${AGENT}`])).toEqual([
      `member:${MEMBER}`,
      `agent:${AGENT}`,
    ]);
  });

  it("reads a single value as a one-item list and preserves order", () => {
    expect(actorRefValuesFromValue(`member:${MEMBER}`)).toEqual([`member:${MEMBER}`]);
    expect(actorRefValuesFromValue([`member:${SECOND_MEMBER}`, `member:${MEMBER}`])).toEqual([
      `member:${SECOND_MEMBER}`,
      `member:${MEMBER}`,
    ]);
  });

  it("returns an empty list for unset or wrongly-typed values", () => {
    expect(actorRefValuesFromValue(undefined)).toEqual([]);
    expect(actorRefValuesFromValue(true)).toEqual([]);
    expect(actorRefValuesFromValue(7)).toEqual([]);
  });
});

describe("hasUnknownActorRef", () => {
  it("is true for a single value this build cannot parse", () => {
    expect(hasUnknownActorRef(`agent:${AGENT}`)).toBe(true);
    expect(hasUnknownActorRef("garbage")).toBe(true);
  });

  it("is true when a multi value mixes known and unknown kinds", () => {
    expect(hasUnknownActorRef([`member:${MEMBER}`, `agent:${AGENT}`])).toBe(true);
  });

  it("is false for values made entirely of known kinds", () => {
    expect(hasUnknownActorRef(`member:${MEMBER}`)).toBe(false);
    expect(hasUnknownActorRef([`member:${MEMBER}`, `member:${SECOND_MEMBER}`])).toBe(false);
  });

  it("is false for an unset value — nothing to lose", () => {
    expect(hasUnknownActorRef(undefined)).toBe(false);
    expect(hasUnknownActorRef([])).toBe(false);
  });
});

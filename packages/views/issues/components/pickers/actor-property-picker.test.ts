import { describe, it, expect } from "vitest";
import { MAX_ISSUE_PROPERTY_ACTOR_VALUES } from "@multica/core/types";
import { toggleActorRefValue } from "./actor-property-picker";

const A = "member:11111111-1111-4111-8111-111111111111";
const B = "member:22222222-2222-4222-8222-222222222222";
const C = "member:00000000-0000-4000-8000-000000000000";

describe("toggleActorRefValue", () => {
  it("appends in insertion order rather than sorted order", () => {
    // C sorts before A; a canonicalizing sort would reorder this result.
    expect(toggleActorRefValue([A], C)).toEqual([A, C]);
  });

  it("removes an already-selected reference", () => {
    expect(toggleActorRefValue([A, B, C], B)).toEqual([A, C]);
  });

  it("clears the property when the last reference is removed", () => {
    expect(toggleActorRefValue([A], A)).toBeUndefined();
  });

  it("refuses to grow past the server cap", () => {
    const full = Array.from(
      { length: MAX_ISSUE_PROPERTY_ACTOR_VALUES },
      (_, i) => `member:${String(i).padStart(8, "0")}-0000-4000-8000-000000000000`,
    );
    expect(toggleActorRefValue(full, A)).toBe(full);
    // Removal still works at capacity, otherwise the value would be stuck.
    expect(toggleActorRefValue(full, full[0]!)).toHaveLength(
      MAX_ISSUE_PROPERTY_ACTOR_VALUES - 1,
    );
  });

  // The regression Elon flagged: an installed client talking to a newer
  // backend must not silently delete reference kinds it cannot parse.
  describe("unknown kinds from a newer backend", () => {
    const AGENT = "agent:99999999-9999-4999-8999-999999999999";

    it("keeps an unknown kind when adding a member", () => {
      expect(toggleActorRefValue([AGENT], A)).toEqual([AGENT, A]);
    });

    it("keeps an unknown kind when removing a member", () => {
      expect(toggleActorRefValue([A, AGENT, B], A)).toEqual([AGENT, B]);
    });

    it("does not clear the property when the last KNOWN kind is removed", () => {
      // Returning undefined here would wipe the agent reference too.
      expect(toggleActorRefValue([A, AGENT], A)).toEqual([AGENT]);
    });
  });
});

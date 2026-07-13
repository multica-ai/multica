import { describe, it, expect } from "vitest";
import { ALL_MENTION_TYPES } from "../types";
import validMentionTypes from "./valid-mention-types.json";

describe("MentionType sync with Go ValidMentionTypes", () => {
  it("TS ALL_MENTION_TYPES exactly matches the JSON fixture (same elements, same order)", () => {
    expect([...ALL_MENTION_TYPES]).toEqual(validMentionTypes);
  });

  it("contains exactly 7 mention types", () => {
    expect(ALL_MENTION_TYPES).toHaveLength(7);
  });

  it("has no duplicate entries", () => {
    const unique = new Set(ALL_MENTION_TYPES);
    expect(unique.size).toBe(ALL_MENTION_TYPES.length);
  });

  it("JSON fixture is a non-empty array of strings", () => {
    expect(Array.isArray(validMentionTypes)).toBe(true);
    expect(validMentionTypes.length).toBeGreaterThan(0);
    for (const entry of validMentionTypes) {
      expect(typeof entry).toBe("string");
    }
  });
});

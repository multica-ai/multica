// @vitest-environment node
import { describe, expect, it } from "vitest";
import { isMentionAllEnabled } from "./mention-all";

describe("isMentionAllEnabled", () => {
  it("defaults to enabled for missing or non-false settings", () => {
    expect(isMentionAllEnabled(null)).toBe(true);
    expect(isMentionAllEnabled({ settings: {} })).toBe(true);
    expect(isMentionAllEnabled({ settings: { mention_all_enabled: true } })).toBe(true);
    expect(isMentionAllEnabled({ settings: { mention_all_enabled: null } })).toBe(true);
    expect(isMentionAllEnabled({ settings: { mention_all_enabled: "false" } })).toBe(true);
  });

  it("disables all-member mentions only for explicit false", () => {
    expect(isMentionAllEnabled({ settings: { mention_all_enabled: false } })).toBe(false);
  });
});

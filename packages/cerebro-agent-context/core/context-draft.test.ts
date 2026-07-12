import { describe, expect, it } from "vitest";
import { bumpPatch, isContextDraftDirty, canSubmitContextDraft } from "./context-draft";

describe("bumpPatch", () => {
  it("increments a strict semantic patch version", () => {
    expect(bumpPatch("2.4.9")).toBe("2.4.10");
  });

  it("falls back for an invalid version", () => {
    expect(bumpPatch("draft")).toBe("1.0.1");
  });
});

describe("context draft state", () => {
  const current = {
    instructions: "Current",
    model: "gpt-5",
    thinkingLevel: "high",
    personaSandbox: "strict",
  };

  it("detects a changed instruction", () => {
    expect(isContextDraftDirty(current, { ...current, instructions: "Changed" })).toBe(true);
  });

  it("does not mark an identical draft dirty", () => {
    expect(isContextDraftDirty(current, current)).toBe(false);
  });

  it("requires both a change and a title before submission", () => {
    expect(canSubmitContextDraft("Reason", true)).toBe(true);
    expect(canSubmitContextDraft("  ", true)).toBe(false);
    expect(canSubmitContextDraft("Reason", false)).toBe(false);
  });
});

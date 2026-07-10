import { describe, expect, it } from "vitest";
import { shouldStampBlock } from "./author-code-plugin";

describe("shouldStampBlock", () => {
  it("stamps a paragraph that just received text", () => {
    expect(shouldStampBlock("paragraph", "hello there", "JEH", false)).toBe(
      true,
    );
  });

  it("never stamps headings or code blocks", () => {
    expect(shouldStampBlock("heading", "IDS", "JEH", false)).toBe(false);
    expect(shouldStampBlock("codeBlock", "select 1", "JEH", false)).toBe(false);
  });

  it("skips empty blocks", () => {
    expect(shouldStampBlock("paragraph", "", "JEH", false)).toBe(false);
    expect(shouldStampBlock("paragraph", "   ", "JEH", false)).toBe(false);
  });

  it("skips lines already carrying the writer's own code", () => {
    expect(
      shouldStampBlock("paragraph", "JEH already stamped", "JEH", false),
    ).toBe(false);
    expect(shouldStampBlock("paragraph", "JEH", "JEH", false)).toBe(false);
  });

  it("skips lines whose first text is an existing bold stamp", () => {
    expect(
      shouldStampBlock("paragraph", "MOP wrote this", "JEH", true),
    ).toBe(false);
  });

  it("still stamps plain lines starting with acronyms", () => {
    expect(
      shouldStampBlock("paragraph", "AI agent vi kan sætte igang", "JEH", false),
    ).toBe(true);
    expect(
      shouldStampBlock("paragraph", "KPI i month view", "JEH", false),
    ).toBe(true);
  });
});

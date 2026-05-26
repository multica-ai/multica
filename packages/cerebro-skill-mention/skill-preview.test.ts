import { describe, expect, it } from "vitest";
import { skillPreviewText } from "./skill-preview";

describe("skillPreviewText", () => {
  it("returns an empty string for missing content", () => {
    expect(skillPreviewText(null)).toBe("");
    expect(skillPreviewText(undefined)).toBe("");
    expect(skillPreviewText("")).toBe("");
  });

  it("strips a leading YAML frontmatter block", () => {
    const content = "---\nname: Release notes\ndescription: Draft notes\n---\nDo the thing.";
    expect(skillPreviewText(content)).toBe("Do the thing.");
  });

  it("handles CRLF frontmatter delimiters", () => {
    const content = "---\r\nname: X\r\n---\r\nBody text.";
    expect(skillPreviewText(content)).toBe("Body text.");
  });

  it("keeps the whole body when there is no frontmatter", () => {
    expect(skillPreviewText("Just instructions, no frontmatter.")).toBe(
      "Just instructions, no frontmatter.",
    );
  });

  it("falls back to the raw content when stripping leaves nothing", () => {
    // A body that is only frontmatter — show it rather than a blank panel.
    const content = "---\nname: X\n---\n";
    expect(skillPreviewText(content)).toBe(content.trim());
  });

  it("truncates very long bodies and appends an ellipsis", () => {
    const body = "a".repeat(5000);
    const result = skillPreviewText(`---\nname: X\n---\n${body}`);
    expect(result.endsWith("…")).toBe(true);
    expect(result.length).toBeLessThan(body.length);
  });
});

import { describe, expect, it } from "vitest";
import { parseFrontmatter } from "./frontmatter";

describe("parseFrontmatter", () => {
  it("extracts name and description", () => {
    const md = `---\nname: code-reviewer\ndescription: Reviews PRs\n---\n# Body`;
    expect(parseFrontmatter(md)).toEqual({ name: "code-reviewer", description: "Reviews PRs" });
  });
  it("strips surrounding quotes", () => {
    const md = `---\nname: "my skill"\ndescription: 'does X'\n---`;
    expect(parseFrontmatter(md)).toEqual({ name: "my skill", description: "does X" });
  });
  it("returns empty strings when no frontmatter", () => {
    expect(parseFrontmatter("# Just a heading")).toEqual({ name: "", description: "" });
  });
  it("returns empty strings when frontmatter is unterminated", () => {
    expect(parseFrontmatter("---\nname: x\nstill going")).toEqual({ name: "", description: "" });
  });
});

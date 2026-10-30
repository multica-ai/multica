// @vitest-environment node
import { describe, expect, it } from "vitest";
import { buildSkillFileTree } from "./file-tree";

describe("buildSkillFileTree", () => {
  it("puts SKILL.md first, then folders, then files", () => {
    const tree = buildSkillFileTree(["notes.md", "templates/b.md", "SKILL.md", "templates/a.md", "assets/logo.svg"]);
    expect(tree.map((n) => n.path)).toEqual(["SKILL.md", "assets", "templates", "notes.md"]);
    expect(tree[2]!.children.map((n) => n.path)).toEqual(["templates/a.md", "templates/b.md"]);
    expect(tree[2]!.isDirectory).toBe(true);
  });

  it("nests folders to any depth", () => {
    const [docs] = buildSkillFileTree(["docs/api/v1/ref.md"]);
    expect(docs!.children[0]!.children[0]!.children[0]).toMatchObject({ path: "docs/api/v1/ref.md", isDirectory: false });
  });
});

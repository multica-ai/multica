import { describe, expect, it } from "vitest";
import { discoverFolderSkills, type FolderEntry } from "./folder-discovery";

function entry(path: string, content: string): FolderEntry {
  return { relativePath: path, text: async () => content, size: content.length };
}

describe("discoverFolderSkills", () => {
  it("finds each SKILL.md as a skill and groups sibling files", async () => {
    const entries = [
      entry("root/skills/a/SKILL.md", "---\nname: a\ndescription: A\n---"),
      entry("root/skills/a/ref.md", "hello"),
      entry("root/skills/b/SKILL.md", "---\nname: b\n---"),
    ];
    const { candidates, truncated } = await discoverFolderSkills(entries);
    expect(truncated).toBe(false);
    expect(candidates.map((c) => c.name).sort()).toEqual(["a", "b"]);
    const a = candidates.find((c) => c.name === "a")!;
    expect(a.data.files?.map((f) => f.path)).toEqual(["ref.md"]);
    expect(a.data.content).toContain("name: a");
  });

  it("falls back to the directory name when frontmatter has no name", async () => {
    const { candidates } = await discoverFolderSkills([
      entry("x/my-skill/SKILL.md", "# no frontmatter"),
    ]);
    expect(candidates[0]!.name).toBe("my-skill");
  });

  it("skips binary files but keeps the skill", async () => {
    const { candidates } = await discoverFolderSkills([
      entry("s/SKILL.md", "---\nname: s\n---"),
      entry("s/logo.png", "BINARY"),
    ]);
    expect(candidates[0]!.data.files ?? []).toEqual([]);
  });
});

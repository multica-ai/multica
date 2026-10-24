import { describe, expect, it } from "vitest";
import { diffLines, diffSkill } from "./diff";
import { contentHash, initialRevisions } from "./fixtures";

describe("skill version mock diff", () => {
  it("keeps context lines and numbers both sides", () => {
    expect(diffLines("a\nb\nc", "a\nx\nc")).toEqual([
      { kind: "context", text: "a", oldNo: 1, newNo: 1 },
      { kind: "del", text: "b", oldNo: 2, newNo: null },
      { kind: "add", text: "x", oldNo: null, newNo: 2 },
      { kind: "context", text: "c", oldNo: 3, newNo: 3 },
    ]);
  });

  it("reports per-file status with SKILL.md first", () => {
    const diff = diffSkill(
      { "SKILL.md": "a", "b.md": "same", "gone.md": "x" },
      { "SKILL.md": "b", "b.md": "same", "new.md": "y" },
    );
    expect(diff.map((file) => [file.path, file.status])).toEqual([
      ["SKILL.md", "modified"],
      ["b.md", "unchanged"],
      ["gone.md", "removed"],
      ["new.md", "added"],
    ]);
  });

  it("gives every fixture revision distinct content", () => {
    const hashes = initialRevisions.map((revision) =>
      contentHash(revision.files),
    );
    expect(new Set(hashes).size).toBe(hashes.length);
  });
});

import { describe, expect, it } from "vitest";
import { countLineDiff, diffDescriptions, splitLines } from "./description-diff";

describe("splitLines", () => {
  it("treats a trailing newline as a terminator, not an extra line", () => {
    expect(splitLines("a\nb")).toEqual(["a", "b"]);
    expect(splitLines("a\nb\n")).toEqual(["a", "b"]);
  });

  it("returns no lines for empty input", () => {
    expect(splitLines("")).toEqual([]);
  });
});

describe("countLineDiff", () => {
  // These cases mirror server/internal/util/linediff_test.go one-for-one: the
  // timeline badge is rendered from the Go counts and the expanded diff from
  // these, so a divergence would show as a badge that contradicts its own diff.
  it.each([
    { old: "a\nb", next: "a\nb", added: 0, removed: 0 },
    { old: "", next: "a\nb\nc", added: 3, removed: 0 },
    { old: "a\nb", next: "", added: 0, removed: 2 },
    { old: "a\nb", next: "a\nb\nc", added: 1, removed: 0 },
    { old: "a\nb\nc", next: "a\nc", added: 0, removed: 1 },
    { old: "a\nb\nc", next: "a\nB\nc", added: 1, removed: 1 },
    { old: "a\nb\nc", next: "x\ny", added: 2, removed: 3 },
    { old: "a\nb", next: "a\nb\n", added: 0, removed: 0 },
    { old: "a\nb\nc\nd", next: "b\nc\nd\na", added: 1, removed: 1 },
  ])("counts ($old -> $next)", ({ old, next, added, removed }) => {
    expect(countLineDiff(old, next)).toEqual({ added, removed });
  });
});

describe("diffDescriptions", () => {
  it("marks an append as added lines with surrounding context", () => {
    const diff = diffDescriptions("intro", "intro\nnew line");
    expect(diff.added).toBe(1);
    expect(diff.removed).toBe(0);
    expect(diff.isRewrite).toBe(false);
    const types = diff.hunks.flatMap((h) => h.lines.map((l) => l.type));
    expect(types).toEqual(["context", "added"]);
  });

  it("flags a wholesale rewrite so the UI can switch away from a unified diff", () => {
    const before = "the original request\nplease look into this\nthanks";
    const after = "## Plan\n1. investigate\n2. fix\n3. verify";
    const diff = diffDescriptions(before, after);
    expect(diff.isRewrite).toBe(true);
  });

  it("does not call a first write a rewrite — there is nothing being lost", () => {
    const diff = diffDescriptions("", "brand new content\nsecond line");
    expect(diff.isRewrite).toBe(false);
    expect(diff.added).toBe(2);
  });

  it("reports an empty-to-empty change as empty", () => {
    expect(diffDescriptions("", "").isEmpty).toBe(true);
  });

  it("attaches word segments when a line is edited in place", () => {
    const diff = diffDescriptions(
      "the quick brown fox jumps",
      "the quick red fox jumps",
    );
    const changed = diff.hunks.flatMap((h) => h.lines).filter((l) => l.type !== "context");
    expect(changed).toHaveLength(2);
    for (const line of changed) {
      expect(line.segments).toBeDefined();
    }
    const removedChanged = changed
      .find((l) => l.type === "removed")!
      .segments!.filter((s) => s.changed)
      .map((s) => s.text)
      .join("");
    expect(removedChanged).toContain("brown");
    // The unchanged prefix must not be marked, or every line reads as fully new.
    const removedUnchanged = changed
      .find((l) => l.type === "removed")!
      .segments!.filter((s) => !s.changed)
      .map((s) => s.text)
      .join("");
    expect(removedUnchanged).toContain("the quick ");
  });

  it("leaves unrelated replaced lines without word segments", () => {
    const diff = diffDescriptions("alpha beta gamma", "zzz yyy xxx");
    const changed = diff.hunks.flatMap((h) => h.lines).filter((l) => l.type !== "context");
    for (const line of changed) {
      expect(line.segments).toBeUndefined();
    }
  });

  it("collapses long unchanged runs into separate hunks", () => {
    const filler = Array.from({ length: 40 }, (_, i) => `line ${i}`);
    const before = ["head", ...filler, "tail"].join("\n");
    const after = ["HEAD", ...filler, "TAIL"].join("\n");
    const diff = diffDescriptions(before, after);
    expect(diff.hunks.length).toBe(2);
    // The 40 filler lines are not all shipped to the renderer.
    const rendered = diff.hunks.reduce((n, h) => n + h.lines.length, 0);
    expect(rendered).toBeLessThan(20);
    expect(diff.hunks[1]!.skippedBefore).toBeGreaterThan(0);
  });
});

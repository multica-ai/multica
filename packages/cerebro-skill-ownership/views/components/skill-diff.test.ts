import { describe, expect, it } from "vitest";
import { computeDiff, groupForDisplay, DIFF_CONTEXT_LINES } from "./skill-diff";

describe("computeDiff", () => {
  it("marks unchanged lines with matching old/new line numbers", () => {
    const diff = computeDiff("a\nb\nc", "a\nb\nc");
    expect(diff).toEqual([
      { type: "unchanged", text: "a", oldLine: 1, newLine: 1 },
      { type: "unchanged", text: "b", oldLine: 2, newLine: 2 },
      { type: "unchanged", text: "c", oldLine: 3, newLine: 3 },
    ]);
  });

  it("detects an added line", () => {
    const diff = computeDiff("a\nc", "a\nb\nc");
    expect(diff).toEqual([
      { type: "unchanged", text: "a", oldLine: 1, newLine: 1 },
      { type: "added", text: "b", oldLine: null, newLine: 2 },
      { type: "unchanged", text: "c", oldLine: 2, newLine: 3 },
    ]);
  });

  it("detects a removed line", () => {
    const diff = computeDiff("a\nb\nc", "a\nc");
    expect(diff).toEqual([
      { type: "unchanged", text: "a", oldLine: 1, newLine: 1 },
      { type: "removed", text: "b", oldLine: 2, newLine: null },
      { type: "unchanged", text: "c", oldLine: 3, newLine: 2 },
    ]);
  });
});

describe("word-level highlighting on modified lines", () => {
  it("highlights only the changed words when a sentence is lightly edited", () => {
    const diff = computeDiff(
      "The quick brown fox jumps over the lazy dog",
      "The quick brown fox leaps over the lazy dog",
    );
    expect(diff).toHaveLength(2);
    const [removed, added] = diff;
    expect(removed).toMatchObject({ type: "removed" });
    expect(added).toMatchObject({ type: "added" });

    if (removed?.type !== "removed" || added?.type !== "added") throw new Error("unreachable");
    expect(removed.words).toBeDefined();
    expect(added.words).toBeDefined();

    const changedOld = removed.words!.filter((w) => w.changed).map((w) => w.text);
    const changedNew = added.words!.filter((w) => w.changed).map((w) => w.text);
    expect(changedOld).toEqual(["jumps"]);
    expect(changedNew).toEqual(["leaps"]);

    // Most of the sentence is untouched.
    const unchangedCount = removed.words!.filter((w) => !w.changed).length;
    expect(unchangedCount).toBeGreaterThan(0);
  });

  it("does not word-diff two unrelated replaced lines", () => {
    const diff = computeDiff("Alpha bravo charlie delta", "Echo foxtrot golf hotel");
    const [removed, added] = diff;
    if (removed?.type !== "removed" || added?.type !== "added") throw new Error("unreachable");
    // Zero shared words — well below the similarity gate.
    expect(removed.words).toBeUndefined();
    expect(added.words).toBeUndefined();
  });
});

describe("groupForDisplay", () => {
  it("does not collapse anything when every unchanged line is within context distance", () => {
    const base = ["a", "b", "changed", "d", "e"].join("\n");
    const proposed = ["a", "b", "new", "d", "e"].join("\n");
    const diff = computeDiff(base, proposed);
    const groups = groupForDisplay(diff);
    expect(groups.every((g) => g.kind === "line")).toBe(true);
  });

  it("collapses a long run of unchanged lines far from any change", () => {
    const unchangedRun = Array.from({ length: 20 }, (_, i) => `line${i}`);
    const base = ["changed", ...unchangedRun].join("\n");
    const proposed = ["new", ...unchangedRun].join("\n");
    const diff = computeDiff(base, proposed);
    const groups = groupForDisplay(diff);

    const collapsed = groups.filter((g) => g.kind === "collapsed");
    expect(collapsed).toHaveLength(1);
    // 20 unchanged lines minus the DIFF_CONTEXT_LINES kept visible after the change.
    expect(collapsed[0]!.lines).toHaveLength(20 - DIFF_CONTEXT_LINES);
  });

  it("keeps context lines directly around each change visible", () => {
    const before = Array.from({ length: 10 }, (_, i) => `pad${i}`);
    const after = Array.from({ length: 10 }, (_, i) => `pad${i}`);
    const base = [...before, "changed", ...after].join("\n");
    const proposed = [...before, "new", ...after].join("\n");
    const diff = computeDiff(base, proposed);
    const groups = groupForDisplay(diff);

    const collapsed = groups.filter((g) => g.kind === "collapsed");
    // One collapsed run before the change, one after.
    expect(collapsed).toHaveLength(2);
    collapsed.forEach((g) => {
      if (g.kind === "collapsed") {
        expect(g.lines.length).toBe(10 - DIFF_CONTEXT_LINES);
      }
    });
  });
});

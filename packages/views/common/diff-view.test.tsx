// @vitest-environment node
import { describe, expect, it } from "vitest";
import { diffLines } from "./diff-view";

describe("diffLines", () => {
  it("marks an identical text as all same", () => {
    expect(diffLines("a\nb", "a\nb").every((l) => l.kind === "same")).toBe(true);
  });

  it("keeps the order of both texts around a change", () => {
    expect(diffLines("one\ntwo\nthree", "one\nTWO\nthree")).toEqual([
      { kind: "same", text: "one" },
      { kind: "removed", text: "two" },
      { kind: "added", text: "TWO" },
      { kind: "same", text: "three" },
    ]);
  });

  it("treats an empty before as all additions", () => {
    expect(diffLines("", "new")).toEqual([{ kind: "added", text: "new" }]);
  });

  it("treats an empty after as all removals", () => {
    expect(diffLines("old", "")).toEqual([{ kind: "removed", text: "old" }]);
  });
});

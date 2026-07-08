import { describe, it, expect } from "vitest";
import { maxSiblingStage, stageOptions } from "./stage-picker";

describe("maxSiblingStage", () => {
  it("returns 0 when nothing is staged", () => {
    expect(maxSiblingStage([])).toBe(0);
    expect(maxSiblingStage([{ stage: null }, { stage: null }])).toBe(0);
  });

  it("returns the highest assigned stage, ignoring unstaged children", () => {
    expect(
      maxSiblingStage([{ stage: 1 }, { stage: 5 }, { stage: null }, { stage: 3 }]),
    ).toBe(5);
  });
});

describe("stageOptions", () => {
  it("floors at Stage 1–3 when nothing higher exists", () => {
    expect(stageOptions(null, 0)).toEqual([1, 2, 3]);
    expect(stageOptions(1, 0)).toEqual([1, 2, 3]);
  });

  // The regression Elon flagged: a parent already has a Stage 5 child, so the
  // picker must keep Stage 5 selectable and offer a new Stage 6 — not floor at 3.
  it("extends one beyond the highest sibling stage", () => {
    expect(stageOptions(null, 5)).toEqual([1, 2, 3, 4, 5, 6]);
  });

  it("extends one beyond the current stage even without siblings", () => {
    expect(stageOptions(5, 0)).toEqual([1, 2, 3, 4, 5, 6]);
  });
});

import { describe, expect, it } from "vitest";
import {
  PULL_WIDTH,
  isHorizontalPull,
  pullOffset,
} from "./use-line-authors-pull";

describe("pullOffset", () => {
  it("ignores leftward and zero movement", () => {
    expect(pullOffset(-30)).toBe(0);
    expect(pullOffset(0)).toBe(0);
  });

  it("follows the pointer up to the gutter width", () => {
    expect(pullOffset(40)).toBe(40);
    expect(pullOffset(PULL_WIDTH)).toBe(PULL_WIDTH);
  });

  it("rubber-bands past the gutter width", () => {
    expect(pullOffset(PULL_WIDTH + 100)).toBe(PULL_WIDTH + 20);
  });
});

describe("isHorizontalPull", () => {
  it("accepts a clear rightward pull", () => {
    expect(isHorizontalPull(30, 5)).toBe(true);
  });

  it("rejects leftward movement", () => {
    expect(isHorizontalPull(-30, 0)).toBe(false);
  });

  it("rejects mostly-vertical movement (scrolling)", () => {
    expect(isHorizontalPull(12, 40)).toBe(false);
  });

  it("rejects tiny movements", () => {
    expect(isHorizontalPull(5, 1)).toBe(false);
  });
});

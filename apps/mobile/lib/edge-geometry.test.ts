import { describe, expect, it } from "vitest";
import {
  computeAtNewestEdge,
  computeCrossedMarkers,
  isDividerPast,
  shouldMarkViewedOnUnmount,
  type DividerRect,
  type ScrollMetrics,
} from "./edge-geometry";

const m = (
  offsetY: number,
  contentH: number,
  viewportH: number,
): ScrollMetrics => ({ offsetY, contentH, viewportH });

describe("computeAtNewestEdge", () => {
  it("short content is always at edge (nothing to scroll)", () => {
    // contentH < viewportH + slack ⇒ maxScroll <= slack ⇒ true
    expect(computeAtNewestEdge(m(0, 500, 800), "oldest")).toBe(true);
    expect(computeAtNewestEdge(m(0, 500, 800), "newest")).toBe(true);
  });

  it("oldest: at bottom means at the newest edge", () => {
    // contentH 2000, viewport 800 ⇒ maxScroll = 1200
    expect(computeAtNewestEdge(m(1200, 2000, 800), "oldest")).toBe(true);
    // slack of 80 means 1190 still counts
    expect(computeAtNewestEdge(m(1120, 2000, 800), "oldest")).toBe(true);
    // outside the slack band
    expect(computeAtNewestEdge(m(1000, 2000, 800), "oldest")).toBe(false);
  });

  it("newest: at top means at the newest edge", () => {
    expect(computeAtNewestEdge(m(0, 2000, 800), "newest")).toBe(true);
    expect(computeAtNewestEdge(m(80, 2000, 800), "newest")).toBe(true);
    expect(computeAtNewestEdge(m(200, 2000, 800), "newest")).toBe(false);
  });

  it("the two directions disagree about which offset is the edge", () => {
    // Top of list: newest-edge for "newest" but NOT for "oldest" when there
    // is plenty to scroll.
    expect(computeAtNewestEdge(m(0, 5000, 800), "newest")).toBe(true);
    expect(computeAtNewestEdge(m(0, 5000, 800), "oldest")).toBe(false);
    // Bottom of list: inverse.
    expect(computeAtNewestEdge(m(4200, 5000, 800), "oldest")).toBe(true);
    expect(computeAtNewestEdge(m(4200, 5000, 800), "newest")).toBe(false);
  });
});

describe("isDividerPast", () => {
  const rect = (y: number, height: number) => ({ y, height });

  it("returns true once the divider's bottom has risen above viewport top", () => {
    // divider sits 200px down from content top, 2px tall
    const r = rect(200, 2);
    // viewport top at 202 (divider bottom exactly at offsetY)
    expect(isDividerPast(r, m(202, 2000, 800), "oldest")).toBe(true);
    expect(isDividerPast(r, m(300, 2000, 800), "oldest")).toBe(true);
    // viewport top at 100 — divider is still below the top
    expect(isDividerPast(r, m(100, 2000, 800), "oldest")).toBe(false);
  });

  it("is direction-symmetric (same answer regardless of sort direction)", () => {
    const r = rect(500, 1);
    for (const dir of ["oldest", "newest"] as const) {
      expect(isDividerPast(r, m(600, 2000, 800), dir)).toBe(true);
      expect(isDividerPast(r, m(400, 2000, 800), dir)).toBe(false);
    }
  });
});

describe("computeCrossedMarkers", () => {
  const r = (y: number, height = 2): DividerRect => ({ y, height });

  it("returns the set of markers whose divider has scrolled above the viewport top", () => {
    const rects = new Map<string, DividerRect>([
      ["__top_marker__", r(200)],
      ["reply:a", r(500)],
      ["reply:b", r(900)],
    ]);
    // viewport top at 600: top marker (bottom 202) and reply:a (bottom 502)
    // are past; reply:b (bottom 902) is not.
    const crossed = computeCrossedMarkers(rects, m(600, 2000, 800), "oldest");
    expect([...crossed].sort()).toEqual(["__top_marker__", "reply:a"]);
  });

  it("returns an empty set when nothing is crossed", () => {
    const rects = new Map<string, DividerRect>([
      ["__top_marker__", r(1200)],
    ]);
    expect(computeCrossedMarkers(rects, m(0, 2000, 800), "oldest").size).toBe(0);
  });

  it("marks every marker past once the user reaches the bottom", () => {
    const rects = new Map<string, DividerRect>([
      ["__top_marker__", r(200)],
      ["reply:a", r(500)],
      ["reply:b", r(900)],
    ]);
    // At bottom (offset 1200) every divider is above the top.
    const crossed = computeCrossedMarkers(rects, m(1200, 2000, 800), "oldest");
    expect([...crossed].sort()).toEqual([
      "__top_marker__",
      "reply:a",
      "reply:b",
    ]);
  });

  it("ignores direction (geometry is symmetric)", () => {
    const rects = new Map<string, DividerRect>([["x", r(400)]]);
    const oldest = computeCrossedMarkers(rects, m(500, 2000, 800), "oldest");
    const newest = computeCrossedMarkers(rects, m(500, 2000, 800), "newest");
    expect([...oldest]).toEqual([...newest]);
    expect(oldest.has("x")).toBe(true);
  });

  it("returns an empty set for an empty rect map", () => {
    expect(
      computeCrossedMarkers(new Map(), m(0, 2000, 800), "oldest").size,
    ).toBe(0);
  });
});

describe("shouldMarkViewedOnUnmount", () => {
  it("marks viewed when there are no unread dividers (nothing to catch up on)", () => {
    expect(
      shouldMarkViewedOnUnmount({
        hasDividers: false,
        unseenMarkerCount: 0,
        userReachedEdge: false,
      }),
    ).toBe(true);
  });

  it("marks viewed when every marker was crossed", () => {
    expect(
      shouldMarkViewedOnUnmount({
        hasDividers: true,
        unseenMarkerCount: 0,
        userReachedEdge: false,
      }),
    ).toBe(true);
  });

  it("marks viewed when the user actively reached the newest edge", () => {
    expect(
      shouldMarkViewedOnUnmount({
        hasDividers: true,
        unseenMarkerCount: 2,
        userReachedEdge: true,
      }),
    ).toBe(true);
  });

  it("does NOT mark viewed on a cold load with unread dividers and no interaction", () => {
    // Regression: first render has no entries (noDividers true), but entries
    // arrive before unmount. The LIVE hasDividers is true, user never
    // scrolled/reached edge, markers remain unseen → preserve the snapshot.
    expect(
      shouldMarkViewedOnUnmount({
        hasDividers: true,
        unseenMarkerCount: 1,
        userReachedEdge: false,
      }),
    ).toBe(false);
  });

  it("does NOT mark viewed when markers remain uncrossed and the edge wasn't reached", () => {
    expect(
      shouldMarkViewedOnUnmount({
        hasDividers: true,
        unseenMarkerCount: 3,
        userReachedEdge: false,
      }),
    ).toBe(false);
  });
});

// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  bottomPinTarget,
  distanceFromBottom,
  isAtLiveEnd,
  type ScrollMetrics,
} from "./stick-to-bottom";
import { FOLLOW_EDGE_THRESHOLD } from "../../common/task-transcript/transcript-follow";

const VIEWPORT = 600;

function at(content: number, fromBottom: number): ScrollMetrics {
  return {
    clientHeight: VIEWPORT,
    scrollHeight: content,
    scrollTop: Math.max(0, content - VIEWPORT - fromBottom),
  };
}

describe("distanceFromBottom", () => {
  it("measures the content left below the fold", () => {
    expect(distanceFromBottom(at(2000, 340))).toBe(340);
  });

  it("floors at zero when the content is shorter than the viewport", () => {
    expect(distanceFromBottom({ clientHeight: 600, scrollHeight: 200, scrollTop: 0 })).toBe(0);
  });
});

describe("isAtLiveEnd", () => {
  it("keeps following inside the edge threshold and releases past it", () => {
    expect(isAtLiveEnd(at(4000, FOLLOW_EDGE_THRESHOLD))).toBe(true);
    expect(isAtLiveEnd(at(4000, FOLLOW_EDGE_THRESHOLD + 1))).toBe(false);
  });
});

describe("bottomPinTarget", () => {
  it("pins a grown row back to the bottom", () => {
    expect(bottomPinTarget({ clientHeight: VIEWPORT, scrollHeight: 920, scrollTop: 200 })).toBe(320);
  });

  it("pins past a reply taller than the viewport in one step", () => {
    expect(bottomPinTarget({ clientHeight: VIEWPORT, scrollHeight: 50_000, scrollTop: 0 })).toBe(
      50_000 - VIEWPORT,
    );
  });

  it("re-pins when the composer grows and shrinks the viewport", () => {
    const shrunk = { clientHeight: VIEWPORT - 72, scrollHeight: 2000, scrollTop: 2000 - VIEWPORT };
    expect(distanceFromBottom(shrunk)).toBe(72);
    expect(bottomPinTarget(shrunk)).toBe(2000 - (VIEWPORT - 72));
  });

  it("never pins upward when content shrinks under a pinned viewport", () => {
    expect(bottomPinTarget({ clientHeight: VIEWPORT, scrollHeight: 1000, scrollTop: 900 })).toBeNull();
  });

  it("reports no work when the viewport is already at the bottom", () => {
    expect(bottomPinTarget({ clientHeight: VIEWPORT, scrollHeight: 2000, scrollTop: 1400 })).toBeNull();
  });
});

// @vitest-environment node
import { describe, expect, it } from "vitest";
import { distanceFromBottom, isAtLiveEnd, type ScrollMetrics } from "./stick-to-bottom";
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

  it("sees the live end slide away when the composer shrinks the viewport", () => {
    expect(
      distanceFromBottom({
        clientHeight: VIEWPORT - 72,
        scrollHeight: 2000,
        scrollTop: 2000 - VIEWPORT,
      }),
    ).toBe(72);
  });
});

describe("isAtLiveEnd", () => {
  it("keeps following inside the edge threshold and releases past it", () => {
    expect(isAtLiveEnd(at(4000, FOLLOW_EDGE_THRESHOLD))).toBe(true);
    expect(isAtLiveEnd(at(4000, FOLLOW_EDGE_THRESHOLD + 1))).toBe(false);
  });
});

import { describe, expect, it } from "vitest";
import {
  isNavigationGesture,
  navigationGestureFromSwipe,
} from "./navigation-gestures";

describe("navigationGestureFromSwipe", () => {
  it("maps horizontal macOS swipe directions to browser-style history", () => {
    expect(navigationGestureFromSwipe("right")).toBe("back");
    expect(navigationGestureFromSwipe("left")).toBe("forward");
  });

  it("ignores vertical and unknown directions", () => {
    expect(navigationGestureFromSwipe("up")).toBeNull();
    expect(navigationGestureFromSwipe("down")).toBeNull();
    expect(navigationGestureFromSwipe("sideways")).toBeNull();
  });
});

describe("isNavigationGesture", () => {
  it("accepts only the renderer navigation gestures", () => {
    expect(isNavigationGesture("back")).toBe(true);
    expect(isNavigationGesture("forward")).toBe(true);
    expect(isNavigationGesture("right")).toBe(false);
    expect(isNavigationGesture(null)).toBe(false);
  });
});

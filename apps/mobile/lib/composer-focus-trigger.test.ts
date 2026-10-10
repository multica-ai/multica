// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  shouldCollapseAfterBlur,
  shouldWaitForInputLayout,
} from "./composer-focus-trigger";

describe("composer expand trigger focus", () => {
  it("waits for native input layout when the first trigger expands the composer", () => {
    expect(shouldWaitForInputLayout(false)).toBe(true);
  });

  it("requests focus immediately for another starter selection while expanded", () => {
    expect(shouldWaitForInputLayout(true)).toBe(false);
  });

  it("requests focus immediately when the reply target changes while expanded", () => {
    expect(shouldWaitForInputLayout(true)).toBe(false);
  });

  it("does not let a delayed blur clear a reply target selected afterward", () => {
    expect(
      shouldCollapseAfterBlur({
        isEmpty: true,
        isFocused: false,
        triggerRevisionAtBlur: 3,
        currentTriggerRevision: 4,
      }),
    ).toBe(false);
  });

  it("collapses after blur only while the target is unchanged and empty", () => {
    expect(
      shouldCollapseAfterBlur({
        isEmpty: true,
        isFocused: false,
        triggerRevisionAtBlur: 3,
        currentTriggerRevision: 3,
      }),
    ).toBe(true);
    expect(
      shouldCollapseAfterBlur({
        isEmpty: false,
        isFocused: false,
        triggerRevisionAtBlur: 3,
        currentTriggerRevision: 3,
      }),
    ).toBe(false);
  });
});

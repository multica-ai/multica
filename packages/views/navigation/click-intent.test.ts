import { describe, expect, it } from "vitest";
import { navigatesInPlace } from "./click-intent";

const click = { button: 0, metaKey: false, ctrlKey: false, shiftKey: false };

describe("navigatesInPlace", () => {
  it("is true only for a plain primary click", () => {
    expect(navigatesInPlace(click)).toBe(true);
    expect(navigatesInPlace({ ...click, metaKey: true })).toBe(false);
    expect(navigatesInPlace({ ...click, ctrlKey: true })).toBe(false);
    expect(navigatesInPlace({ ...click, shiftKey: true })).toBe(false);
    expect(navigatesInPlace({ ...click, button: 1 })).toBe(false);
  });
});

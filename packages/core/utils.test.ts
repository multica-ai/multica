import { afterEach, describe, expect, it, vi } from "vitest";
import { createRequestId, createSafeId, generateUUID, isImeComposing } from "./utils";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("utils id helpers", () => {
  it("generateUUID returns a valid UUID v4", () => {
    const id = generateUUID();
    expect(id).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i);
  });

  it("createSafeId falls back when crypto.randomUUID is unavailable", () => {
    vi.stubGlobal("crypto", {
      getRandomValues: (arr: Uint8Array) => {
        for (let i = 0; i < arr.length; i++) arr[i] = i;
        return arr;
      },
    });

    const id = createSafeId();
    expect(id).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i);
  });

  it("createRequestId defaults to length 8 and respects custom length", () => {
    vi.spyOn(globalThis.crypto, "randomUUID").mockReturnValue("12345678-1234-4abc-8def-1234567890ab");

    expect(createRequestId()).toBe("12345678");
    expect(createRequestId(12)).toBe("123456781234");
  });
});

describe("isImeComposing", () => {
  it("returns true when nativeEvent.isComposing is set (React synthetic event)", () => {
    expect(isImeComposing({ nativeEvent: { isComposing: true, keyCode: 13 } })).toBe(true);
  });

  it("returns true when nativeEvent.keyCode is 229 (Safari edge case)", () => {
    // Safari clears isComposing on the keydown that ends composition; keyCode
    // stays 229 throughout, which is the only reliable signal in that browser.
    expect(isImeComposing({ nativeEvent: { isComposing: false, keyCode: 229 } })).toBe(true);
  });

  it("returns true for native KeyboardEvent without nativeEvent wrapper", () => {
    expect(isImeComposing({ isComposing: true, keyCode: 13 })).toBe(true);
    expect(isImeComposing({ isComposing: false, keyCode: 229 })).toBe(true);
  });

  it("returns false when not composing", () => {
    expect(isImeComposing({ nativeEvent: { isComposing: false, keyCode: 13 } })).toBe(false);
    expect(isImeComposing({ isComposing: false, keyCode: 13 })).toBe(false);
  });
});

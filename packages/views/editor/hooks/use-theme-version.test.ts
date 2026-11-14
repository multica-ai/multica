import { afterEach, describe, expect, it } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { EMBED_TOKEN_INPUTS } from "@multica/core/appearance";
import { useThemeVersion } from "./use-theme-version";

const root = document.documentElement;
const settle = () => act(() => Promise.resolve());

afterEach(() => {
  root.removeAttribute("style");
});

describe("useThemeVersion", () => {
  it("watches the variables the radius tokens are resolved from", () => {
    expect(EMBED_TOKEN_INPUTS).toEqual(
      expect.arrayContaining(["--radius-card", "--card-radius", "--button-radius", "--input-radius", "--radius"]),
    );
  });

  it("bumps when a theme option changes only a token's source", async () => {
    root.style.setProperty("--card-radius", "8px");
    const { result } = renderHook(() => useThemeVersion());
    await settle();
    root.style.setProperty("--card-radius", "16px");
    await settle();
    expect(result.current).toBe(1);
  });

  it("ignores a dialog's scroll lock on the same style attribute", async () => {
    const { result } = renderHook(() => useThemeVersion());
    await settle();
    root.style.overflow = "hidden";
    await settle();
    root.style.removeProperty("overflow");
    await settle();
    expect(result.current).toBe(0);
  });
});

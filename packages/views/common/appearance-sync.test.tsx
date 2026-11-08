import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { act, cleanup, render } from "@testing-library/react";
import { APPEARANCE_STORAGE_KEY, useAppearanceStore } from "@multica/core/appearance";
import { AppearanceSync } from "./appearance-sync";

const html = () => document.documentElement;

describe("AppearanceSync", () => {
  beforeEach(() => {
    localStorage.clear();
    useAppearanceStore.setState({ style: "default" });
  });
  afterEach(cleanup);

  it("mirrors the style onto <html> and leaves the default unset", () => {
    render(<AppearanceSync />);
    expect(html().hasAttribute("data-style")).toBe(false);

    act(() => useAppearanceStore.getState().setStyle("pop"));
    expect(html().getAttribute("data-style")).toBe("pop");

    act(() => useAppearanceStore.getState().setStyle("default"));
    expect(html().hasAttribute("data-style")).toBe(false);
  });

  it("clears the attribute when the product UI unmounts", () => {
    useAppearanceStore.setState({ style: "pop" });
    const { unmount } = render(<AppearanceSync />);
    expect(html().getAttribute("data-style")).toBe("pop");
    unmount();
    expect(html().hasAttribute("data-style")).toBe(false);
  });

  it("follows a change saved by another window", async () => {
    render(<AppearanceSync />);
    localStorage.setItem(
      APPEARANCE_STORAGE_KEY,
      JSON.stringify({ state: { style: "pop" }, version: 0 }),
    );
    await act(async () => {
      window.dispatchEvent(new StorageEvent("storage", { key: APPEARANCE_STORAGE_KEY }));
    });
    expect(html().getAttribute("data-style")).toBe("pop");
  });
});

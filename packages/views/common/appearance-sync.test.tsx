import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { act, cleanup, render } from "@testing-library/react";
import { APPEARANCE_STORAGE_KEY, useAppearanceStore } from "@multica/core/appearance";
import { AppearanceSync } from "./appearance-sync";

const html = () => document.documentElement;

describe("AppearanceSync", () => {
  beforeEach(() => {
    localStorage.clear();
    useAppearanceStore.setState({ style: "default", frame: "neutral" });
  });
  afterEach(cleanup);

  it("mirrors the preference onto <html> and leaves defaults unset", () => {
    render(<AppearanceSync />);
    expect(html().hasAttribute("data-style")).toBe(false);
    expect(html().hasAttribute("data-frame")).toBe(false);

    act(() => useAppearanceStore.getState().setFrame("sand"));
    expect(html().getAttribute("data-frame")).toBe("sand");

    act(() => useAppearanceStore.getState().setStyle("pop"));
    expect(html().getAttribute("data-style")).toBe("pop");

    act(() => useAppearanceStore.getState().setFrame("neutral"));
    expect(html().hasAttribute("data-frame")).toBe(false);
  });

  it("clears the attributes when the product UI unmounts", () => {
    useAppearanceStore.setState({ style: "pop", frame: "mist" });
    const { unmount } = render(<AppearanceSync />);
    expect(html().getAttribute("data-style")).toBe("pop");
    unmount();
    expect(html().hasAttribute("data-style")).toBe(false);
    expect(html().hasAttribute("data-frame")).toBe(false);
  });

  it("follows a change saved by another window", async () => {
    render(<AppearanceSync />);
    localStorage.setItem(
      APPEARANCE_STORAGE_KEY,
      JSON.stringify({ state: { style: "pop", frame: "neutral" }, version: 0 }),
    );
    await act(async () => {
      window.dispatchEvent(new StorageEvent("storage", { key: APPEARANCE_STORAGE_KEY }));
    });
    expect(html().getAttribute("data-style")).toBe("pop");
  });
});

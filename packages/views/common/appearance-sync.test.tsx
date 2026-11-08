import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { act, cleanup, render } from "@testing-library/react";
import {
  APPEARANCE_STORAGE_KEY,
  DEFAULT_APPEARANCE,
  useAppearanceStore,
} from "@multica/core/appearance";
import { AppearanceSync } from "./appearance-sync";

const html = () => document.documentElement;
const sheet = () => document.getElementById("multica-appearance");

describe("AppearanceSync", () => {
  beforeEach(() => {
    localStorage.clear();
    useAppearanceStore.setState(DEFAULT_APPEARANCE);
  });
  afterEach(cleanup);

  it("leaves <html> untouched for the defaults", () => {
    render(<AppearanceSync />);
    expect(html().hasAttribute("data-theme")).toBe(false);
    expect(html().getAttribute("style") ?? "").toBe("");
    expect(sheet()).toBeNull();
  });

  it("mirrors the theme, fonts, size and density onto <html>", () => {
    render(<AppearanceSync />);
    act(() =>
      useAppearanceStore
        .getState()
        .set({ theme: "pop", uiFont: "serif", fontSize: 15, density: "comfortable" }),
    );
    expect(html().getAttribute("data-theme")).toBe("pop");
    expect(html().getAttribute("data-theme-traits")).toContain("status-blocks");
    expect(html().style.getPropertyValue("--font-ui")).toBe("var(--font-ui-serif)");
    expect(html().style.getPropertyValue("--font-scale")).toBe(String(15 / 14));
    expect(html().style.getPropertyValue("--issue-row-height")).toBe("42px");
    expect(sheet()?.textContent).toContain("--button-primary:");

    act(() => useAppearanceStore.getState().reset());
    expect(html().hasAttribute("data-theme")).toBe(false);
    expect(html().style.getPropertyValue("--font-ui")).toBe("");
    expect(sheet()).toBeNull();
  });

  it("clears everything when the product UI unmounts", () => {
    useAppearanceStore.setState({ theme: "github", accent: "green", codeFont: "system" });
    const { unmount } = render(<AppearanceSync />);
    expect(html().getAttribute("data-theme")).toBe("github");
    expect(sheet()).not.toBeNull();
    unmount();
    expect(html().hasAttribute("data-theme")).toBe(false);
    expect(html().style.getPropertyValue("--font-mono")).toBe("");
    expect(sheet()).toBeNull();
  });

  it("follows a change saved by another window", async () => {
    render(<AppearanceSync />);
    localStorage.setItem(
      APPEARANCE_STORAGE_KEY,
      JSON.stringify({ state: { theme: "notion" }, version: 0 }),
    );
    await act(async () => {
      window.dispatchEvent(new StorageEvent("storage", { key: APPEARANCE_STORAGE_KEY }));
    });
    expect(html().getAttribute("data-theme")).toBe("notion");
  });
});

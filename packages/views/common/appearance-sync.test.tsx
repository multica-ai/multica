import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { act, cleanup, render } from "@testing-library/react";
import {
  APPEARANCE_STORAGE_KEY,
  DEFAULT_APPEARANCE,
  INSTALLED_THEMES_STORAGE_KEY,
  useAppearanceStore,
  useInstalledThemesStore,
  useThemeSafeMode,
} from "@multica/core/appearance";
import { AppearanceSync } from "./appearance-sync";

const html = () => document.documentElement;
const sheet = () => document.getElementById("multica-appearance");
const themeCss = () => document.getElementById("multica-theme-css");

const PAPER = {
  apiVersion: 1,
  id: "paper",
  name: "Paper",
  tokens: { light: { "page-canvas": "#f6efdf" }, dark: {} },
  css: '[data-mc="content"] p { hyphens: auto; }',
} as const;

describe("AppearanceSync", () => {
  beforeEach(() => {
    localStorage.clear();
    sessionStorage.clear();
    useAppearanceStore.setState(DEFAULT_APPEARANCE);
    useInstalledThemesStore.setState({ themes: [] });
    useThemeSafeMode.setState({ active: false });
  });
  afterEach(cleanup);

  it("leaves <html> untouched for the defaults", () => {
    render(<AppearanceSync />);
    expect(html().hasAttribute("data-theme")).toBe(false);
    expect(html().getAttribute("style") ?? "").toBe("");
    expect(sheet()).toBeNull();
    expect(themeCss()).toBeNull();
  });

  it("mirrors the theme, its options, type and density onto <html>", () => {
    render(<AppearanceSync />);
    act(() => {
      const { set, setThemeOption } = useAppearanceStore.getState();
      set({ theme: "pop", uiFont: "serif", fontSize: 15, contentCjkFont: "serif", density: "comfortable" });
      setThemeOption("pop", "status-blocks", false);
    });
    expect(html().getAttribute("data-theme")).toBe("pop");
    expect(html().getAttribute("data-theme-option-signal")).toBe("yellow");
    expect(html().hasAttribute("data-theme-option-status-blocks")).toBe(false);
    expect(html().getAttribute("data-theme-option-outlined-chips")).toBe("on");
    expect(html().style.getPropertyValue("--font-ui")).toBe("var(--font-ui-serif)");
    expect(html().style.getPropertyValue("--font-content-cjk")).toBe("var(--font-cjk-serif)");
    expect(html().style.getPropertyValue("--font-scale")).toBe(String(15 / 14));
    expect(html().style.getPropertyValue("--issue-row-height")).toBe("42px");
    expect(sheet()?.textContent).toContain("--button-primary:");
    expect(themeCss()?.textContent).toContain('[data-mc="issue-group-header"]');
    expect(sheet()!.compareDocumentPosition(themeCss()!) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();

    act(() => useAppearanceStore.getState().set({ theme: "github" }));
    expect(html().hasAttribute("data-theme-option-signal")).toBe(false);
    expect(themeCss()).toBeNull();

    act(() => useAppearanceStore.getState().reset());
    expect(html().hasAttribute("data-theme")).toBe(false);
    expect(html().style.getPropertyValue("--font-ui")).toBe("");
    expect(sheet()).toBeNull();
  });

  it("applies an installed theme's CSS, and nothing of it in safe mode", () => {
    useInstalledThemesStore.setState({ themes: [PAPER] });
    useAppearanceStore.setState({ theme: "paper" });
    render(<AppearanceSync />);
    expect(html().getAttribute("data-theme")).toBe("paper");
    expect(themeCss()?.textContent).toContain("hyphens: auto");

    act(() => useThemeSafeMode.getState().enter());
    expect(html().hasAttribute("data-theme")).toBe(false);
    expect(themeCss()).toBeNull();
    expect(useAppearanceStore.getState().theme).toBe("paper");

    act(() => useThemeSafeMode.getState().exit());
    expect(html().getAttribute("data-theme")).toBe("paper");
  });

  it("clears everything when the product UI unmounts", () => {
    useAppearanceStore.setState({ theme: "pop", accent: "green", codeFont: "system" });
    const { unmount } = render(<AppearanceSync />);
    expect(html().getAttribute("data-theme")).toBe("pop");
    expect(sheet()).not.toBeNull();
    unmount();
    expect(html().hasAttribute("data-theme")).toBe(false);
    expect(html().hasAttribute("data-theme-option-signal")).toBe(false);
    expect(html().style.getPropertyValue("--font-mono")).toBe("");
    expect(sheet()).toBeNull();
    expect(themeCss()).toBeNull();
  });

  it("follows changes saved by another window", async () => {
    render(<AppearanceSync />);
    localStorage.setItem(
      INSTALLED_THEMES_STORAGE_KEY,
      JSON.stringify({ state: { themes: [PAPER] }, version: 0 }),
    );
    localStorage.setItem(
      APPEARANCE_STORAGE_KEY,
      JSON.stringify({ state: { theme: "paper" }, version: 0 }),
    );
    await act(async () => {
      window.dispatchEvent(new StorageEvent("storage", { key: INSTALLED_THEMES_STORAGE_KEY }));
      window.dispatchEvent(new StorageEvent("storage", { key: APPEARANCE_STORAGE_KEY }));
    });
    expect(html().getAttribute("data-theme")).toBe("paper");
  });
});

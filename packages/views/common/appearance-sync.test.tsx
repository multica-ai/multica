import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render } from "@testing-library/react";
import {
  APPEARANCE_STORAGE_KEY,
  DEFAULT_APPEARANCE,
  INSTALLED_THEMES_STORAGE_KEY,
  useAppearanceStore,
  useInstalledThemesStore,
  useThemeFolderStatus,
  useThemeSafeMode,
} from "@multica/core/appearance";
import { setCurrentWorkspace } from "@multica/core/platform";
import { AppearanceSync } from "./appearance-sync";

const html = () => document.documentElement;
const sheet = () => document.getElementById("multica-appearance");
const themeCss = () => document.getElementById("multica-theme-css");

const PAPER = {
  apiVersion: 1,
  id: "paper",
  name: "Paper",
  css: '.theme-light { --page-canvas: #f6efdf; }\n[data-mc="content"] p { hyphens: auto; }',
} as const;

describe("AppearanceSync", () => {
  beforeEach(() => {
    localStorage.clear();
    sessionStorage.clear();
    useAppearanceStore.setState(DEFAULT_APPEARANCE);
    useInstalledThemesStore.setState({ themes: [] });
    useThemeSafeMode.setState({ active: false });
  });
  afterEach(() => {
    cleanup();
    html().classList.remove("dark");
    setCurrentWorkspace(null, null);
  });

  it("leaves <html> untouched for the defaults", () => {
    render(<AppearanceSync />);
    expect(html().hasAttribute("data-theme")).toBe(false);
    expect(html().getAttribute("style") ?? "").toBe("");
    expect(sheet()).toBeNull();
    expect(themeCss()).toBeNull();
  });

  it("marks the mode on <html> for theme CSS, following the dark class", async () => {
    const { unmount } = render(<AppearanceSync />);
    expect(html().classList.contains("theme-light")).toBe(true);
    html().classList.add("dark");
    await act(async () => {});
    expect(html().classList.contains("theme-dark")).toBe(true);
    expect(html().classList.contains("theme-light")).toBe(false);
    html().classList.remove("dark");
    await act(async () => {});
    expect(html().classList.contains("theme-light")).toBe(true);
    unmount();
    expect(html().classList.contains("theme-light")).toBe(false);
  });

  it("marks the platform and window focus on <html>", () => {
    const { unmount } = render(<AppearanceSync />);
    expect(html().classList.contains("is-web")).toBe(true);
    act(() => {
      vi.spyOn(document, "hasFocus").mockReturnValue(false);
      window.dispatchEvent(new Event("blur"));
    });
    expect(html().classList.contains("is-focused")).toBe(false);
    act(() => {
      vi.spyOn(document, "hasFocus").mockReturnValue(true);
      window.dispatchEvent(new Event("focus"));
    });
    expect(html().classList.contains("is-focused")).toBe(true);
    unmount();
    expect(html().classList.contains("is-web")).toBe(false);
    expect(html().classList.contains("is-focused")).toBe(false);
  });

  it("mirrors the theme, its options, type and density onto <html>", () => {
    render(<AppearanceSync />);
    act(() => {
      const { set, setThemeOption } = useAppearanceStore.getState();
      set({ lightTheme: "pop", darkTheme: "pop", highContrast: true, uiFont: "serif", fontSize: 15, contentCjkFont: "serif", density: "comfortable" });
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
    expect(themeCss()?.textContent).toContain("--button-primary:");
    expect(themeCss()?.textContent).toContain('[data-mc="issue-group-header"]');
    expect(sheet()?.textContent).toContain("--muted-foreground: color-mix(");

    act(() => useAppearanceStore.getState().set({ lightTheme: "github", darkTheme: "github" }));
    expect(html().hasAttribute("data-theme-option-signal")).toBe(false);
    expect(themeCss()?.textContent).not.toContain('[data-mc="issue-group-header"]');
    expect(themeCss()!.compareDocumentPosition(sheet()!) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();

    act(() => useAppearanceStore.getState().reset());
    expect(html().hasAttribute("data-theme")).toBe(false);
    expect(html().style.getPropertyValue("--font-ui")).toBe("");
    expect(sheet()).toBeNull();
    expect(themeCss()).toBeNull();
  });

  it("keeps the adjustments after a theme stylesheet created later", () => {
    useAppearanceStore.setState({ highContrast: true });
    render(<AppearanceSync />);
    act(() => useAppearanceStore.getState().set({ lightTheme: "notion", darkTheme: "notion" }));
    expect(themeCss()!.compareDocumentPosition(sheet()!) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it("applies an installed theme's CSS, and nothing of it in safe mode", () => {
    useInstalledThemesStore.setState({ themes: [PAPER] });
    useAppearanceStore.setState({ lightTheme: "paper", darkTheme: "paper" });
    render(<AppearanceSync />);
    expect(html().getAttribute("data-theme")).toBe("paper");
    expect(themeCss()?.textContent).toContain("hyphens: auto");

    act(() => useThemeSafeMode.getState().enter());
    expect(html().hasAttribute("data-theme")).toBe(false);
    expect(themeCss()).toBeNull();
    expect(useAppearanceStore.getState()).toMatchObject({ lightTheme: "paper", darkTheme: "paper" });

    act(() => useThemeSafeMode.getState().exit());
    expect(html().getAttribute("data-theme")).toBe("paper");
  });

  it("swaps to the dark theme with the mode", async () => {
    useAppearanceStore.setState({ lightTheme: "notion", darkTheme: "github" });
    render(<AppearanceSync />);
    expect(html().getAttribute("data-theme")).toBe("notion");

    await act(async () => html().classList.add("dark"));
    expect(html().getAttribute("data-theme")).toBe("github");
    expect(html().classList.contains("theme-dark")).toBe(true);

    await act(async () => html().classList.remove("dark"));
    expect(html().getAttribute("data-theme")).toBe("notion");
  });

  it("draws the current workspace's theme and accent, and follows a workspace switch", async () => {
    useAppearanceStore.setState({
      lightTheme: "notion",
      darkTheme: "notion",
      workspaces: {
        "ws-1": { lightTheme: "pop", darkTheme: "pop" },
        "ws-2": { accent: { light: "#2563eb", dark: "#93c5fd" } },
      },
    });
    await act(async () => setCurrentWorkspace("acme", "ws-1"));
    render(<AppearanceSync />);
    expect(html().getAttribute("data-theme")).toBe("pop");
    expect(sheet()?.textContent ?? "").not.toContain("--brand:");

    await act(async () => setCurrentWorkspace("beta", "ws-2"));
    expect(html().getAttribute("data-theme")).toBe("notion");
    expect(sheet()?.textContent).toContain(":root.theme-light { --brand: #2563eb;");
    expect(sheet()?.textContent).toContain(":root.theme-dark { --brand: #93c5fd;");

    act(() => useThemeSafeMode.getState().enter());
    expect(html().hasAttribute("data-theme")).toBe(false);
    expect(sheet()).toBeNull();

    act(() => useThemeSafeMode.getState().exit());
    await act(async () => setCurrentWorkspace(null, null));
    expect(html().getAttribute("data-theme")).toBe("notion");
    expect(sheet()?.textContent ?? "").not.toContain("--brand:");
  });

  it("clears everything when the product UI unmounts", () => {
    useAppearanceStore.setState({ lightTheme: "pop", darkTheme: "pop", highContrast: true, codeFont: "system" });
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
      JSON.stringify({ state: { lightTheme: "paper", darkTheme: "paper" }, version: 0 }),
    );
    await act(async () => {
      window.dispatchEvent(new StorageEvent("storage", { key: INSTALLED_THEMES_STORAGE_KEY }));
      window.dispatchEvent(new StorageEvent("storage", { key: APPEARANCE_STORAGE_KEY }));
    });
    expect(html().getAttribute("data-theme")).toBe("paper");
  });

  describe("on Desktop", () => {
    const encode = (text: string) => new TextEncoder().encode(text);
    const folder = (css: string) => [
      { path: "manifest.json", data: encode(JSON.stringify({ apiVersion: 1, id: "paper", name: "Paper" })) },
      { path: "theme.css", data: encode(css) },
    ];
    let onChange: () => void = () => {};
    const desktop = {
      readThemeFolder: vi.fn(),
      onThemeFolderChange: vi.fn((callback: () => void) => {
        onChange = callback;
        return () => {};
      }),
      openThemeFolder: vi.fn(),
      writeThemeFolder: vi.fn(),
    };
    beforeEach(() => {
      useInstalledThemesStore.setState({ folders: {} });
      useThemeFolderStatus.setState({ path: null, problems: [] });
      (window as unknown as { desktopAPI: unknown }).desktopAPI = desktop;
    });
    afterEach(() => {
      delete (window as unknown as { desktopAPI?: unknown }).desktopAPI;
    });

    it("applies themes from the themes folder and re-reads it when it changes", async () => {
      desktop.readThemeFolder.mockResolvedValue({
        path: "/data/themes",
        entries: [
          { folder: "paper", files: folder(".theme-light { --page-canvas: #fffdf8; }") },
          { folder: "huge", error: "A theme can be at most 1024 KB and 64 files" },
        ],
      });
      useAppearanceStore.setState({ lightTheme: "paper", darkTheme: "paper" });
      render(<AppearanceSync />);
      await vi.waitFor(() => expect(html().getAttribute("data-theme")).toBe("paper"));
      expect(themeCss()?.textContent).toContain("#fffdf8");
      expect(useInstalledThemesStore.getState().folders).toEqual({ paper: "paper" });
      expect(useThemeFolderStatus.getState()).toEqual({
        path: "/data/themes",
        problems: [{ folder: "huge", errors: ["A theme can be at most 1024 KB and 64 files"] }],
      });

      desktop.readThemeFolder.mockResolvedValue({
        path: "/data/themes",
        entries: [{ folder: "paper", files: folder(".theme-light { --page-canvas: #f0f0ff; }") }],
      });
      await act(async () => onChange());
      await vi.waitFor(() => expect(themeCss()?.textContent).toContain("#f0f0ff"));
      expect(useThemeFolderStatus.getState().problems).toEqual([]);

      desktop.readThemeFolder.mockResolvedValue({ path: "/data/themes", entries: [] });
      await act(async () => onChange());
      await vi.waitFor(() => expect(html().hasAttribute("data-theme")).toBe(false));
    });
  });
});

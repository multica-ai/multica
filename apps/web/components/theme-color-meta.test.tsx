/**
 * TIM-10 — the status bar has to follow the *app* theme, not the OS. The
 * resolved theme reaches the DOM as a class on <html>, so that mutation is what
 * this component has to react to.
 */
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { render, cleanup, waitFor } from "@testing-library/react";

const readPaintedThemeColor = vi.hoisted(() => vi.fn<() => string | null>());

vi.mock("@/lib/theme-color", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/theme-color")>()),
  readPaintedThemeColor,
}));

import { ThemeColorMeta } from "./theme-color-meta";

const themeColor = () =>
  document.head.querySelector<HTMLMetaElement>("meta[name='theme-color']")?.content ?? null;

describe("ThemeColorMeta", () => {
  beforeEach(() => {
    document.head.innerHTML = "";
    document.documentElement.className = "";
    // Stand in for the computed --background, which jsdom cannot resolve.
    readPaintedThemeColor.mockImplementation(() =>
      document.documentElement.classList.contains("dark") ? "#111114" : "#fbfbfb",
    );
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it("publishes the painted colour on mount", () => {
    render(<ThemeColorMeta />);

    expect(themeColor()).toBe("#fbfbfb");
  });

  it("retints when the app theme changes, with no reload", async () => {
    render(<ThemeColorMeta />);

    document.documentElement.classList.add("dark");

    await waitFor(() => expect(themeColor()).toBe("#111114"));
  });

  it("leaves the server's fallback alone when no colour can be read", () => {
    document.head.innerHTML =
      '<meta name="theme-color" media="(prefers-color-scheme: light)" content="#fbfbfb">';
    readPaintedThemeColor.mockReturnValue(null);

    render(<ThemeColorMeta />);

    expect(document.head.querySelectorAll("meta[name='theme-color']")).toHaveLength(1);
    expect(themeColor()).toBe("#fbfbfb");
  });

  it("stops watching once unmounted", async () => {
    const { unmount } = render(<ThemeColorMeta />);
    unmount();

    document.documentElement.classList.add("dark");
    await new Promise((resolve) => setTimeout(resolve, 0));

    expect(themeColor()).toBe("#fbfbfb");
  });
});

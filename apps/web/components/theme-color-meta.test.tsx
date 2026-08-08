import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { render, cleanup, waitFor } from "@testing-library/react";

import { ThemeColorMeta } from "./theme-color-meta";

const themeColor = () =>
  document.head.querySelector<HTMLMetaElement>("meta[name='theme-color']")?.content ?? null;

const LIGHT_TOKEN = "oklch(0.988087 0 0)";
const LIGHT_PAINTED = "rgb(251, 251, 251)";
const DARK_PAINTED = "rgb(17, 17, 20)";

describe("ThemeColorMeta", () => {
  beforeEach(() => {
    document.head.innerHTML = "";
    document.documentElement.className = "";
    document.documentElement.style.setProperty("--background", LIGHT_TOKEN);
    document.body.style.backgroundColor = LIGHT_PAINTED;
  });

  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
    document.documentElement.style.removeProperty("--background");
    document.body.style.removeProperty("background-color");
  });

  it("publishes the painted colour on mount", () => {
    render(<ThemeColorMeta />);

    expect(themeColor()).toBe(LIGHT_PAINTED);
  });

  it("retints when the app theme changes, with no reload", async () => {
    render(<ThemeColorMeta />);

    document.documentElement.style.setProperty("--background", "oklch(0.18 0.005 285.823)");
    document.body.style.backgroundColor = DARK_PAINTED;
    document.documentElement.classList.add("dark");

    await waitFor(() => expect(themeColor()).toBe(DARK_PAINTED));
  });

  it("leaves the server's fallback alone when no colour can be read", () => {
    document.head.innerHTML =
      '<meta name="theme-color" media="(prefers-color-scheme: light)" content="#fbfbfb">';
    document.documentElement.style.removeProperty("--background");
    vi.spyOn(window, "getComputedStyle").mockReturnValue({
      backgroundColor: "",
    } as CSSStyleDeclaration);

    render(<ThemeColorMeta />);

    expect(document.head.querySelectorAll("meta[name='theme-color']")).toHaveLength(1);
    expect(themeColor()).toBe("#fbfbfb");
  });

  it("stops watching once unmounted", async () => {
    const { unmount } = render(<ThemeColorMeta />);
    unmount();

    document.documentElement.style.setProperty("--background", "oklch(0.18 0.005 285.823)");
    document.body.style.backgroundColor = DARK_PAINTED;
    document.documentElement.classList.add("dark");
    await new Promise((resolve) => setTimeout(resolve, 0));

    expect(themeColor()).toBe(LIGHT_PAINTED);
  });
});

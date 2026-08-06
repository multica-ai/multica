import { describe, expect, it, beforeEach, afterEach } from "vitest";
import { render, cleanup, waitFor } from "@testing-library/react";

import { ThemeColorMeta } from "./theme-color-meta";

const themeColor = () =>
  document.head.querySelector<HTMLMetaElement>("meta[name='theme-color']")?.content ?? null;

describe("ThemeColorMeta", () => {
  beforeEach(() => {
    document.head.innerHTML = "";
    document.documentElement.className = "";
    document.documentElement.style.setProperty("--background", "#fbfbfb");
  });

  afterEach(() => {
    cleanup();
    document.documentElement.style.removeProperty("--background");
  });

  it("publishes the painted colour on mount", () => {
    render(<ThemeColorMeta />);

    expect(themeColor()).toBe("#fbfbfb");
  });

  it("retints when the app theme changes, with no reload", async () => {
    render(<ThemeColorMeta />);

    document.documentElement.style.setProperty("--background", "#111114");
    document.documentElement.classList.add("dark");

    await waitFor(() => expect(themeColor()).toBe("#111114"));
  });

  it("leaves the server's fallback alone when no colour can be read", () => {
    document.head.innerHTML =
      '<meta name="theme-color" media="(prefers-color-scheme: light)" content="#fbfbfb">';
    document.documentElement.style.removeProperty("--background");

    render(<ThemeColorMeta />);

    expect(document.head.querySelectorAll("meta[name='theme-color']")).toHaveLength(1);
    expect(themeColor()).toBe("#fbfbfb");
  });

  it("stops watching once unmounted", async () => {
    const { unmount } = render(<ThemeColorMeta />);
    unmount();

    document.documentElement.style.setProperty("--background", "#111114");
    document.documentElement.classList.add("dark");
    await new Promise((resolve) => setTimeout(resolve, 0));

    expect(themeColor()).toBe("#fbfbfb");
  });
});

// @vitest-environment jsdom

import { afterEach, describe, expect, it, vi } from "vitest";
import { readEmbedTheme } from "./embed-theme";

/** A browser's resolution of `var()` against the host's variables, enough for these tests. */
function resolve(value: string, vars: Record<string, string>, inherited: string): string {
  if (value === "inherit") return inherited;
  const match = /^var\((--[\w-]+)(?:,\s*(.*))?\)$/.exec(value.trim());
  if (!match) return value;
  const own = vars[match[1]!];
  if (own !== undefined) return resolve(own, vars, inherited);
  return match[2] === undefined ? "" : resolve(match[2], vars, inherited);
}

function stubHost(vars: Record<string, string>, { colorScheme = "light", fontFamily = "Inter, sans-serif" } = {}) {
  const host = document.createElement("div");
  document.body.appendChild(host);
  vi.spyOn(window, "getComputedStyle").mockImplementation((element: Element) => {
    if (element === host) {
      return { colorScheme, fontFamily, getPropertyValue: (name: string) => vars[name] ?? "" } as CSSStyleDeclaration;
    }
    const style = (element as HTMLElement).style;
    return {
      getPropertyValue: (property: string) => resolve(style.getPropertyValue(property), vars, fontFamily),
    } as CSSStyleDeclaration;
  });
  return { host };
}

afterEach(() => {
  vi.restoreAllMocks();
  document.body.innerHTML = "";
  document.documentElement.classList.remove("dark");
});

describe("readEmbedTheme", () => {
  it("resolves the contract where the host sits and leaves no probe behind", () => {
    const { host } = stubHost({ "--foreground": "rgb(10, 10, 10)", "--info": "rgb(0, 0, 255)", "--radius": "10px" });
    const { tokens, colorScheme } = readEmbedTheme(host);
    expect(colorScheme).toBe("light");
    expect(tokens).toMatchObject({
      "--foreground": "rgb(10, 10, 10)",
      "--status-done": "rgb(0, 0, 255)",
      "--palette-red": "#ef4444",
      "--font-ui": "Inter, sans-serif",
      "--font-sans": "Inter, sans-serif",
      "--radius": "10px",
      "--color-scheme": "light",
    });
    expect(tokens).not.toHaveProperty("--chart-1");
    expect(host.childElementCount).toBe(0);
  });

  it("follows a theme's values and the dark fallbacks", () => {
    document.documentElement.classList.add("dark");
    const { host } = stubHost({ "--palette-red": "rgb(200, 80, 80)" }, { colorScheme: "dark" });
    const { tokens, colorScheme } = readEmbedTheme(host);
    expect(colorScheme).toBe("dark");
    expect(tokens["--palette-red"]).toBe("rgb(200, 80, 80)");
    expect(tokens["--code-keyword"]).toBe("oklch(0.7 0.14 255)");
    expect(tokens["--color-scheme"]).toBe("dark");
  });

  it("lets --background carry another host token", () => {
    const { host } = stubHost({ "--background": "white", "--surface": "rgb(250, 250, 250)" });
    expect(readEmbedTheme(host).tokens["--background"]).toBe("white");
    expect(readEmbedTheme(host, { background: "--surface" }).tokens["--background"]).toBe("rgb(250, 250, 250)");
  });

  it("never passes on a value that is not plain", () => {
    const { host } = stubHost({
      "--foreground": "url(https://evil.test/x.png)",
      "--muted": "var(--elsewhere)",
      "--border": "red; background: blue",
      "--brand": "rgb(1, 2, 3)",
    });
    const { tokens } = readEmbedTheme(host);
    expect(tokens).not.toHaveProperty("--foreground");
    expect(tokens).not.toHaveProperty("--border");
    expect(tokens["--brand"]).toBe("rgb(1, 2, 3)");
    expect(Object.values(tokens).some((value) => /url\(|var\(|;/.test(value))).toBe(false);
  });

  it("reads nothing without a host", () => {
    expect(readEmbedTheme(null)).toEqual({ tokens: {}, colorScheme: "light" });
  });
});

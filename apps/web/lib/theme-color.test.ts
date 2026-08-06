import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it, beforeEach } from "vitest";

import { applyThemeColorMeta, THEME_COLOR_FALLBACK } from "./theme-color";

// Vitest roots itself at apps/web (vitest.config.ts lives there), and
// `import.meta.url` is a Vite module id rather than a file URL under jsdom.
const TOKENS_CSS = readFileSync(
  resolve(process.cwd(), "../../packages/ui/styles/tokens.css"),
  "utf8",
);

/**
 * Reads the `--page-canvas` declaration out of a top-level token block. The
 * light values live under `:root`, the dark ones under `.dark`.
 */
function readPageCanvas(selector: string): [number, number, number] {
  const block = TOKENS_CSS.split(new RegExp(`^${selector} \\{$`, "m"))[1] ?? "";
  const match = block.match(/--page-canvas:\s*oklch\(([^)]+)\)/);
  if (!match?.[1]) {
    throw new Error(`tokens.css should declare --page-canvas as oklch() under ${selector}`);
  }

  const [lightness = 0, chroma = 0, hue = 0] = match[1].trim().split(/\s+/).map(Number);
  return [lightness, chroma, hue];
}

/**
 * oklch → sRGB hex, matching what Chrome renders for the same declaration.
 * Verbatim from the CSS Color 4 conversion: oklch → oklab → LMS → linear sRGB →
 * gamma-encoded sRGB.
 */
function oklchToHex([lightness, chroma, hue]: [number, number, number]): string {
  const radians = (hue * Math.PI) / 180;
  const a = chroma * Math.cos(radians);
  const b = chroma * Math.sin(radians);

  const l = (lightness + 0.3963377774 * a + 0.2158037573 * b) ** 3;
  const m = (lightness - 0.1055613458 * a - 0.0638541728 * b) ** 3;
  const s = (lightness - 0.0894841775 * a - 1.291485548 * b) ** 3;

  const linear = [
    4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s,
    -1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s,
    -0.0041960863 * l - 0.7034186147 * m + 1.707614701 * s,
  ];

  const channels = linear.map((channel) => {
    const encoded =
      channel <= 0.0031308 ? 12.92 * channel : 1.055 * channel ** (1 / 2.4) - 0.055;
    const byte = Math.round(Math.min(1, Math.max(0, encoded)) * 255);
    return byte.toString(16).padStart(2, "0");
  });

  return `#${channels.join("")}`;
}

/**
 * The runtime meta reads the colour the page paints, but the server-rendered
 * first paint has to ship a literal. This is the guard that keeps the literal
 * honest — if `--page-canvas` moves, this fails and names the new hex.
 */
describe("THEME_COLOR_FALLBACK", () => {
  it("matches the light --page-canvas token", () => {
    expect(THEME_COLOR_FALLBACK.light).toBe(oklchToHex(readPageCanvas(":root")));
  });

  it("matches the dark --page-canvas token", () => {
    expect(THEME_COLOR_FALLBACK.dark).toBe(oklchToHex(readPageCanvas("\\.dark")));
  });
});

describe("applyThemeColorMeta", () => {
  beforeEach(() => {
    document.head.innerHTML = "";
  });

  const themeColorMetas = () =>
    [...document.head.querySelectorAll<HTMLMetaElement>("meta[name='theme-color']")];

  it("adds the meta when the document has none", () => {
    applyThemeColorMeta("#fbfbfb");

    expect(themeColorMetas().map((meta) => meta.content)).toEqual(["#fbfbfb"]);
  });

  it("drops the server's prefers-color-scheme variants", () => {
    document.head.innerHTML = `
      <meta name="theme-color" media="(prefers-color-scheme: light)" content="#fbfbfb">
      <meta name="theme-color" media="(prefers-color-scheme: dark)" content="#111114">
    `;

    applyThemeColorMeta("#111114");

    // A surviving media meta would win over ours: the HTML spec takes the first
    // theme-color whose media matches, and both of those precede it.
    const metas = themeColorMetas().map((meta) => ({
      media: meta.getAttribute("media"),
      content: meta.content,
    }));
    expect(metas).toEqual([{ media: null, content: "#111114" }]);
  });

  it("retints in place rather than stacking metas on every theme change", () => {
    applyThemeColorMeta("#fbfbfb");
    const [created] = themeColorMetas();

    applyThemeColorMeta("#111114");

    const [current] = themeColorMetas();
    expect(themeColorMetas()).toHaveLength(1);
    expect(current).toBe(created);
    expect(current?.content).toBe("#111114");
  });
});

// @vitest-environment node
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const css = readFileSync(resolve(__dirname, "../../../packages/ui/styles/tokens.css"), "utf8");

const themeBlock = css.slice(css.indexOf("@theme {\n    /* counters"));
const typeBlock = themeBlock.slice(0, themeBlock.indexOf("\n}"));
const mediaStart = css.indexOf("@media (width < 40rem)");
const mediaBlock = css.slice(mediaStart);

describe("mobile appearance tokens", () => {
  it("scales every size and line-height token and keeps the base values", () => {
    const decls = [...typeBlock.matchAll(/(--text-[a-z-]+): calc\((\d+)px \* var\(--mobile-type-scale\)\);/g)];
    // 10 steps x (size + line-height)
    expect(decls).toHaveLength(20);
    expect(decls.filter((d) => d[1]!.endsWith("--line-height"))).toHaveLength(10);
    expect(typeBlock).not.toMatch(/--text-[a-z-]+: \d+px;/);
    const px = Object.fromEntries(decls.map((d) => [d[1], Number(d[2])]));
    expect(px["--text-body"]).toBe(14);
    expect(px["--text-body--line-height"]).toBe(20);
    expect(px["--text-display"]).toBe(36);
  });

  it("defaults leave the UI unchanged (scale 1, 1rem gutter = px-4)", () => {
    expect(css).toMatch(/:root \{[^}]*--mobile-type-scale: 1;/s);
    expect(css).toMatch(/--page-gutter: 1rem;/);
  });

  it("only overrides below the sm breakpoint (40rem)", () => {
    expect(mediaStart).toBeGreaterThan(-1);
    // every attribute override lives inside the mobile media query
    const outside = css.slice(0, mediaStart);
    expect(outside).not.toContain("data-mobile-");
    expect(mediaBlock).toMatch(/data-mobile-font-size="small"\][^}]*--mobile-type-scale: 0\.9;/s);
    expect(mediaBlock).toMatch(/data-mobile-font-size="large"\][^}]*--mobile-type-scale: 1\.1;/s);
    expect(mediaBlock).not.toMatch(/data-mobile-font-size="default"/);
  });

  it("full width removes the gutter but keeps safe-area insets", () => {
    const full = mediaBlock.match(/data-mobile-content-width="full"\] \{([^}]*)\}/s)![1]!;
    expect(full).toContain("--page-gutter");
    expect(full).toContain("safe-area-inset-left");
    expect(full).toContain("safe-area-inset-right");
    expect(mediaBlock).not.toMatch(/data-mobile-content-width="standard"/);
  });

  it("chrome keeps a small inset in Full Width, equal to the gutter by default", () => {
    expect(css).toMatch(/--page-chrome-gutter: var\(--page-gutter\);/);
    const full = mediaBlock.match(/data-mobile-content-width="full"\] \{([^}]*)\}/s)![1]!;
    expect(full).toMatch(/--page-chrome-gutter: max\(\s*0\.75rem,/);
    expect(full).toContain("safe-area-inset-left");
  });

  it("does not touch PAGE_RAIL, icon, or container sizing", () => {
    expect(mediaBlock).not.toMatch(/max-w|1440|\bwidth:|\bsize:|font-size:/);
  });
});

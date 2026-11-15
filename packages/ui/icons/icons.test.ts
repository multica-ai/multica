import { readFileSync } from "node:fs";
import { join } from "node:path";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { OUTPUTS, exportNames } from "./drawings/generate.mjs";
import * as icons from "./index";

describe("@multica/ui/icons", () => {
  it.each(Object.entries(OUTPUTS))("%s matches drawings/drawings.mjs", (file, render) => {
    expect(readFileSync(join(__dirname, file), "utf8"), "run pnpm --filter @multica/ui icons").toBe(render());
  });

  it("exports every drawn name and nothing borrowed from an icon library", () => {
    const exported = Object.keys(icons).filter((n) => n !== "ICON_STROKE_WIDTH");
    expect(exported.sort()).toEqual(Object.keys(exportNames()).sort());
    expect(readFileSync(join(__dirname, "index.ts"), "utf8")).not.toMatch(/from "(?!\.\/)/);
  });

  it.each(Object.keys(exportNames()))("%s renders a Multica glyph", (name) => {
    const Icon = icons[name as keyof typeof icons] as icons.IconComponent;
    const markup = renderToStaticMarkup(createElement(Icon));
    expect(markup).toMatch(/^<svg [^>]*class="icon icon-[a-z0-9-]+"/);
    expect(markup).toMatch(/<(path|circle|ellipse|rect)\b/);
  });
});

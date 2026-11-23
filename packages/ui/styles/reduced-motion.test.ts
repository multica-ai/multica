import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

const css = readFileSync(join(__dirname, "base.css"), "utf8");

/** The bodies of every `@media (prefers-reduced-motion: reduce)` block. */
function reducedMotionBlocks(source: string): string[] {
  const blocks: string[] = [];
  const opener = /@media\s*\(prefers-reduced-motion:\s*reduce\)\s*\{/g;
  for (let match = opener.exec(source); match; match = opener.exec(source)) {
    let depth = 1;
    let end = opener.lastIndex;
    while (depth > 0 && end < source.length) {
      if (source[end] === "{") depth++;
      else if (source[end] === "}") depth--;
      end++;
    }
    blocks.push(source.slice(opener.lastIndex, end - 1));
  }
  return blocks;
}

/** Rules inside a block as `[selectors, declarations]`. */
function rules(block: string): [string[], string][] {
  return [...block.matchAll(/([^{}]+)\{([^{}]*)\}/g)].map(([, selectors, body]) => [
    selectors!.split(",").map((selector) => selector.trim()),
    body!,
  ]);
}

describe("base.css under the system reduced-motion setting", () => {
  const classes = [...new Set(css.match(/\.animate-inbox-enter-[\w-]+/g))];
  const reduced = reducedMotionBlocks(css).flatMap(rules);

  it("defines the Inbox detail transitions", () => {
    expect(classes.sort()).toEqual([
      ".animate-inbox-enter-fade",
      ".animate-inbox-enter-next",
      ".animate-inbox-enter-prev",
    ]);
  });

  it.each(classes)("%s does not animate, not even as a fade", (name) => {
    const rule = reduced.find(([selectors]) => selectors.includes(name));
    expect(rule, `${name} needs a rule in @media (prefers-reduced-motion: reduce)`).toBeDefined();
    expect(rule![1]).toMatch(/(^|;|\s)animation:\s*none\s*;/);
  });
});

// @vitest-environment node
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

const css = readFileSync(join(__dirname, "base.css"), "utf8");

/**
 * Animated classes that keep something under the system reduced-motion
 * setting. Each still needs a rule in `@media (prefers-reduced-motion: reduce)`;
 * the reason says why that rule is not `animation: none`.
 */
const EXCEPTIONS: Record<string, string> = {
  ".animate-skeleton":
    "Keeps only skeleton-hold, a 200ms visibility step that moves nothing, so data that lands fast still skips the placeholder. The pulse stops.",
};

const REDUCED_MEDIA = /@media\s*\(prefers-reduced-motion:\s*reduce\)\s*\{/g;

/** `[start, end)` of every `@media (prefers-reduced-motion: reduce)` block, braces included. */
function reducedMotionRanges(source: string): [number, number][] {
  const ranges: [number, number][] = [];
  for (const match of source.matchAll(REDUCED_MEDIA)) {
    let depth = 1;
    let end = match.index + match[0].length;
    while (depth > 0 && end < source.length) {
      if (source[end] === "{") depth++;
      else if (source[end] === "}") depth--;
      end++;
    }
    ranges.push([match.index, end]);
  }
  return ranges;
}

/** Innermost rules as `[selectors, declarations]`. */
function rules(source: string): [string[], string][] {
  return [...source.matchAll(/([^{}]+)\{([^{}]*)\}/g)].map(([, selectors, body]) => [
    selectors!.split(",").map((selector) => selector.trim()),
    body!,
  ]);
}

const STARTS_ANIMATION = /(^|;|\s)animation(-name)?:\s*(?!none\s*;)[^;]+;/;
const STOPS_ANIMATION = /(^|;|\s)(animation:\s*none|display:\s*none)\s*;/;

/**
 * Classes that start an animation outside reduced-motion blocks, and those the
 * reduced-motion blocks stop (`animation: none`, or hidden with `display: none`)
 * or otherwise restyle.
 */
function audit(withComments: string) {
  const source = withComments.replace(/\/\*[\s\S]*?\*\//g, "");
  const ranges = reducedMotionRanges(source);
  let outside = "";
  let cursor = 0;
  for (const [start, end] of ranges) {
    outside += source.slice(cursor, start);
    cursor = end;
  }
  outside += source.slice(cursor);
  const reduced = ranges.flatMap(([start, end]) => {
    const block = source.slice(start, end);
    return rules(block.slice(block.indexOf("{") + 1, -1));
  });

  const animated = new Set<string>();
  for (const [selectors, body] of rules(outside)) {
    if (!STARTS_ANIMATION.test(body)) continue;
    for (const selector of selectors) if (selector.startsWith(".")) animated.add(selector);
  }
  for (const name of source.match(/\.animate-[\w-]+/g) ?? []) animated.add(name);

  const stopped = new Set<string>();
  const restyled = new Set<string>();
  for (const [selectors, body] of reduced) {
    for (const selector of selectors) {
      restyled.add(selector);
      if (STOPS_ANIMATION.test(body)) stopped.add(selector);
    }
  }
  return { animated: [...animated].sort(), stopped, restyled };
}

/** Animated classes that would still move under the system reduced-motion setting. */
function stillMoving(source: string, exceptions: Record<string, string> = {}): string[] {
  const { animated, stopped } = audit(source);
  return animated.filter((name) => !stopped.has(name) && !(name in exceptions));
}

describe("base.css under the system reduced-motion setting", () => {
  const { animated, restyled } = audit(css);

  it("finds the animated classes", () => {
    expect(animated).toEqual(
      expect.arrayContaining([
        ".animate-chat-enter-fade",
        ".animate-chat-enter-next",
        ".animate-chat-enter-prev",
        ".animate-chat-impulse",
        ".animate-inbox-enter-fade",
        ".animate-inbox-enter-next",
        ".animate-inbox-enter-prev",
        ".animate-nav-progress-sweep",
        ".animate-onboarding-enter",
        ".animate-skeleton",
        ".border-beam-light",
        ".multica-pop",
        ".shimmer-text-window",
      ]),
    );
  });

  it("stops every animated class, not even leaving a fade", () => {
    expect(stillMoving(css, EXCEPTIONS)).toEqual([]);
  });

  it.each(Object.keys(EXCEPTIONS))("%s is a live exception with its own reduced rule", (name) => {
    expect(animated).toContain(name);
    expect(restyled.has(name), `${name} needs a rule in @media (prefers-reduced-motion: reduce)`).toBe(true);
  });
});

describe("stillMoving", () => {
  const reduced = (body: string) => `@media (prefers-reduced-motion: reduce) {\n${body}\n}`;

  it("flags a new .animate-* class with no reduced rule", () => {
    const source = `${css}\n.animate-new-thing { animation: new-thing 150ms ease-out both; }`;
    expect(stillMoving(source, EXCEPTIONS)).toEqual([".animate-new-thing"]);
  });

  it("flags a class whose reduced rule was deleted", () => {
    const covered = ".animate-chat-enter-prev,\n  .animate-chat-enter-fade {";
    expect(css).toContain(covered);
    const source = css.replace(covered, ".animate-chat-enter-prev {");
    expect(stillMoving(source, EXCEPTIONS)).toEqual([".animate-chat-enter-fade"]);
  });

  it("flags a class whose reduced rule swaps in a shorter fade", () => {
    const source = `.animate-a { animation: a-move 160ms ease-out; }\n${reduced(".animate-a { animation-name: a-fade; animation-duration: 120ms; }")}`;
    expect(stillMoving(source)).toEqual([".animate-a"]);
  });

  it("flags any class that starts an animation, not only .animate-*", () => {
    expect(stillMoving(".brand-spin { animation: spin 1s linear infinite; }")).toEqual([".brand-spin"]);
  });

  it("passes classes stopped with animation: none or hidden with display: none", () => {
    const source = [
      ".animate-a, .animate-b { animation: a 160ms ease-out; }",
      ".beam { animation: beam 3s linear infinite; }",
      reduced(".animate-a,\n  .animate-b { animation: none; }\n  .beam { display: none; }"),
    ].join("\n");
    expect(stillMoving(source)).toEqual([]);
  });

  it("lets a listed exception keep a reduced animation", () => {
    const source = `.animate-a { animation: a 1s infinite; }\n${reduced(".animate-a { animation: a-hold 200ms step-end; }")}`;
    expect(stillMoving(source, { ".animate-a": "test" })).toEqual([]);
  });
});

// @vitest-environment node
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

const ROOT = join(__dirname, "../../..");

/**
 * Where the product apps' stylesheets live. The marketing site, docs and
 * storybook don't mount the Appearance settings, matching the scope of the
 * `NO_SYSTEM_ONLY_REDUCED_MOTION` lint rule.
 */
const SCANNED = ["packages/ui", "packages/views", "apps/desktop/src", "apps/web"];
const SKIPPED = ["apps/web/app/landing.css", "apps/web/app/(landing)", "apps/web/features/landing"];
const BUILD_DIRS = new Set(["node_modules", "dist", "out", "build"]);

function stylesheets(): string[] {
  const found: string[] = [];
  const walk = (dir: string) => {
    for (const entry of readdirSync(join(ROOT, dir), { withFileTypes: true })) {
      const path = `${dir}/${entry.name}`;
      if (SKIPPED.includes(path)) continue;
      if (entry.isDirectory()) {
        if (!entry.name.startsWith(".") && !BUILD_DIRS.has(entry.name)) walk(path);
      } else if (entry.name.endsWith(".css")) {
        found.push(path);
      }
    }
  };
  SCANNED.forEach(walk);
  return found.sort();
}

const sheets = Object.fromEntries(
  stylesheets().map((path) => [path, readFileSync(join(ROOT, path), "utf8")]),
);
const css = sheets["packages/ui/styles/base.css"]!;
const attachmentCss = sheets["packages/views/editor/styles/attachment.css"]!;

/**
 * Animated classes that keep something under the system reduced-motion
 * setting, by stylesheet. Each still needs a rule in
 * `@media (prefers-reduced-motion: reduce)` in the same file; the reason says
 * why that rule is not `animation: none`.
 */
const EXCEPTIONS: Record<string, Record<string, string>> = {
  "packages/ui/styles/base.css": {
    ".animate-skeleton":
      "Keeps only skeleton-hold, a 200ms visibility step that moves nothing, so data that lands fast still skips the placeholder. The pulse stops.",
  },
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

/**
 * `file class` for each animated class that still moves. Each stylesheet is
 * audited on its own: a reduced rule in one file doesn't count for another.
 */
function stillMovingIn(
  files: Record<string, string>,
  exceptions: Record<string, Record<string, string>> = EXCEPTIONS,
): string[] {
  return Object.entries(files).flatMap(([path, source]) =>
    stillMoving(source, exceptions[path]).map((name) => `${path} ${name}`),
  );
}

describe("stylesheets the apps load, under the system reduced-motion setting", () => {
  it("scans the shared, product and desktop stylesheets, not the marketing site", () => {
    const paths = Object.keys(sheets);
    expect(paths).toEqual(
      expect.arrayContaining([
        "apps/desktop/src/renderer/src/globals.css",
        "apps/web/app/globals.css",
        "packages/ui/styles/base.css",
        "packages/views/editor/styles/attachment.css",
      ]),
    );
    expect(paths).not.toContain("apps/web/app/landing.css");
  });

  it("stops every animated class, not even leaving a fade", () => {
    expect(stillMovingIn(sheets)).toEqual([]);
  });

  it.each(
    Object.entries(EXCEPTIONS).flatMap(([path, names]) => Object.keys(names).map((name) => [path, name])),
  )("%s %s is a live exception with its own reduced rule", (path, name) => {
    expect(sheets[path], `${path} is not a scanned stylesheet`).toBeDefined();
    const { animated, restyled } = audit(sheets[path]!);
    expect(animated).toContain(name);
    expect(restyled.has(name), `${name} needs a rule in @media (prefers-reduced-motion: reduce)`).toBe(true);
  });

  it("flags a new animated class in a stylesheet other than base.css", () => {
    const path = "packages/views/editor/styles/code.css";
    const files = { ...sheets, [path]: `${sheets[path]}\n.code-flash { animation: flash 1s infinite; }` };
    expect(stillMovingIn(files)).toEqual([`${path} .code-flash`]);
  });

  it("doesn't let a reduced rule in another file stop a class", () => {
    const files = {
      "a.css": ".animate-a { animation: a 1s infinite; }",
      "b.css": "@media (prefers-reduced-motion: reduce) { .animate-a { animation: none; } }",
    };
    expect(stillMovingIn(files)).toEqual(["a.css .animate-a"]);
  });

  it("holds an exception to the file it is listed for", () => {
    const files = { ...sheets, "other.css": ".animate-skeleton { animation: pulse 2s infinite; }" };
    expect(stillMovingIn(files)).toEqual(["other.css .animate-skeleton"]);
  });
});

describe("attachment.css under the system reduced-motion setting", () => {
  it("stops the upload pulse and keeps the image dimmed", () => {
    const { animated, stopped } = audit(attachmentCss);
    expect(animated).toContain(".image-uploading");
    expect(stopped.has(".image-uploading")).toBe(true);
    expect(attachmentCss).toMatch(/\.image-uploading\s*\{\s*opacity:\s*0\.5;/);
  });

  it("flags the upload pulse without its reduced rule", () => {
    const withoutReduced = attachmentCss.replace(/@media\s*\(prefers-reduced-motion:\s*reduce\)\s*\{[^}]*\}\s*\}/, "");
    expect(withoutReduced).not.toContain("prefers-reduced-motion");
    expect(stillMoving(withoutReduced)).toEqual([".image-uploading"]);
  });
});

describe("base.css under the system reduced-motion setting", () => {
  const { animated } = audit(css);

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
});

describe("stillMoving", () => {
  const reduced = (body: string) => `@media (prefers-reduced-motion: reduce) {\n${body}\n}`;
  const baseExceptions = EXCEPTIONS["packages/ui/styles/base.css"];

  it("flags a new .animate-* class with no reduced rule", () => {
    const source = `${css}\n.animate-new-thing { animation: new-thing 150ms ease-out both; }`;
    expect(stillMoving(source, baseExceptions)).toEqual([".animate-new-thing"]);
  });

  it("flags a class whose reduced rule was deleted", () => {
    const covered = ".animate-chat-enter-prev,\n  .animate-chat-enter-fade {";
    expect(css).toContain(covered);
    const source = css.replace(covered, ".animate-chat-enter-prev {");
    expect(stillMoving(source, baseExceptions)).toEqual([".animate-chat-enter-fade"]);
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

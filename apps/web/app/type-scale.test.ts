// @vitest-environment node
import { readFileSync, readdirSync, statSync } from "node:fs";
import { resolve, join, relative } from "node:path";
import { describe, expect, it } from "vitest";

/**
 * Does the product still have exactly one type scale?
 *
 * Before `--text-*` existed in tokens.css there was no baseline to align to, so
 * font sizes grew wherever they were needed: 51 distinct sizes across web and
 * desktop, 370 written as arbitrary pixel values, six of those at half a pixel
 * (10.5 / 11.5 / 12.5 / 13.5 / 14.5 / 15.5px). A scale only holds if going off
 * it is harder than staying on it, and nothing in a Tailwind build makes an
 * off-scale arbitrary size look wrong — it compiles exactly like a token does.
 *
 * So the guard lives here. This asserts on source text rather than on rendered
 * components because the failure mode is authorial, not behavioural: a
 * component test would prove a class name renders, never that the class was one
 * of the ten the design system actually defines.
 *
 * Any unit counts, not just px. The first sweep of this migration only looked
 * for `px` and so walked straight past four `0.8rem` call sites in
 * packages/ui — an off-scale size is off-scale however it is spelled.
 *
 * Scope is product UI. `apps/mobile` builds through its own NativeWind config
 * and `apps/docs` rides fumadocs' type system; both keep Tailwind's default
 * scale on purpose and are not scanned.
 */

const repoRoot = resolve(process.cwd(), "../..");

/** The scale, as tokens.css defines it. Sizes in px. */
const SCALE = [
  ["micro", 11, 15],
  ["caption", 12, 16],
  ["label", 13, 18],
  ["body", 14, 20],
  ["body-lg", 15, 22],
  ["title-sm", 16, 24],
  ["title", 18, 28],
  ["title-lg", 20, 28],
  ["display-sm", 24, 32],
  ["display", 36, 40],
] as const;

const scanRoots = ["packages/ui", "packages/views", "apps/web", "apps/desktop/src"];
const skipDirs = new Set(["node_modules", ".next", "dist", "out", "build", ".turbo"]);
const sourceExtensions = [".ts", ".tsx", ".css"];

/**
 * Landing pages run a marketing display ramp (rem/clamp, 2.2-6.4rem) that is
 * deliberately not this scale — a hero headline does not belong on the same
 * steps as a table cell. They still may not use pixel sizes, so only the
 * relative-unit rule is lifted for them.
 */
const marketingPaths = ["apps/web/app/(landing)", "apps/web/features/landing"];
const isMarketing = (rel: string) => marketingPaths.some((p) => rel.startsWith(p));

/**
 * Tailwind's default steps that the role scale replaces one-for-one. `text-4xl`
 * and larger are not listed: their only call sites are decorative emoji and the
 * serif onboarding hero, which are not UI text and do not sit on this ramp.
 */
const replacedDefaults = ["xs", "sm", "base", "lg", "xl", "2xl", "3xl"];

const bannedPatterns = [
  {
    label: "arbitrary pixel size",
    regex: /\btext-\[\d+(?:\.\d+)?px\]/g,
    hint: "use a scale step (text-micro … text-display)",
    appliesTo: () => true,
  },
  {
    label: "arbitrary relative size",
    regex: /\btext-\[\d*\.?\d+(?:rem|em)\]/g,
    hint: "use a scale step (text-micro … text-display)",
    appliesTo: (rel: string) => !isMarketing(rel),
  },
  {
    label: "Tailwind default size",
    regex: new RegExp(String.raw`\btext-(?:${replacedDefaults.join("|")})\b`, "g"),
    hint: "use the role-named equivalent: sm -> body, base -> title-sm, 2xl -> display-sm",
    appliesTo: () => true,
  },
  /**
   * Hand-written CSS is the blind spot a class-only guard leaves open, and it
   * is where the worst of the old drift survived longest: the transcript kept
   * a 12.5px body long after every Tailwind call site was on the scale, so the
   * "no half-pixel sizes" claim was true of the classes and false of the
   * product. A literal length here is off-scale by definition — the steps are
   * reachable as `var(--text-*)`.
   */
  {
    // The lookahead sits directly after the colon and swallows the whitespace
    // itself. Written as `\s*(?!var\()` the engine simply backtracks `\s*` to
    // zero width, the lookahead then sees a space rather than `var(`, and every
    // tokenised declaration matches as a violation.
    label: "raw CSS font-size",
    regex: /font-size:(?!\s*var\()\s*[^;}]+/g,
    hint: "use var(--text-micro) … var(--text-display)",
    appliesTo: (rel: string) => rel.endsWith(".css") && !isMarketing(rel),
  },
];

/**
 * `base.css` pins editable text to 16px on coarse pointers because iOS Safari
 * zooms the page when a focused input is below 16px. That is a platform
 * workaround, not typography, and no scale step can express it.
 */
const rawCssExemptions = [/font-size:\s*16px\s*!important/];

/**
 * Tailwind scans this file like any other source, so a banned class written out
 * in full here would compile into the bundle as dead CSS — the exact thing the
 * test exists to prevent. Hints and prose above therefore name the steps
 * without the `text-` prefix, and the patterns build it at runtime.
 */

/**
 * Comments legitimately name the old classes when explaining what a step
 * replaced, so they are stripped before scanning. Over-stripping would only
 * ever hide a violation, never invent one, and a class name never follows a
 * comment marker on the same line in practice.
 */
function stripComments(source: string): string {
  return source
    // Block comments are replaced with their own newlines rather than removed,
    // so a violation's reported line still matches the real file. Collapsing
    // them shifts every line below a file header comment.
    .replace(/\/\*[\s\S]*?\*\//g, (block) => "\n".repeat((block.match(/\n/g) ?? []).length))
    .replace(/(^|[^:])\/\/[^\n]*/g, "$1");
}

function collectSourceFiles(dir: string, found: string[] = []): string[] {
  for (const entry of readdirSync(dir)) {
    if (skipDirs.has(entry)) continue;
    const path = join(dir, entry);
    if (statSync(path).isDirectory()) {
      collectSourceFiles(path, found);
      continue;
    }
    if (!sourceExtensions.some((ext) => entry.endsWith(ext))) continue;
    if (/\.test\.tsx?$/.test(entry)) continue;
    found.push(path);
  }
  return found;
}

describe("type scale", () => {
  const tokens = readFileSync(resolve(repoRoot, "packages/ui/styles/tokens.css"), "utf8");

  it.each(SCALE)("defines --text-%s with a paired line-height", (name, size, lineHeight) => {
    expect(tokens).toContain(`--text-${name}: ${size}px;`);
    expect(tokens).toContain(`--text-${name}--line-height: ${lineHeight}px;`);
  });

  it("steps ascend in size and never tighten line-height going up", () => {
    for (let i = 1; i < SCALE.length; i += 1) {
      const [name, size, lineHeight] = SCALE[i]!;
      const [, previousSize, previousLineHeight] = SCALE[i - 1]!;
      expect(size, `${name} must be larger than the step below it`).toBeGreaterThan(previousSize);
      expect(lineHeight, `${name} line-height must not drop below the step below it`)
        .toBeGreaterThanOrEqual(previousLineHeight);
    }
  });

  /**
   * The steps are role-named, so tailwind-merge cannot infer that they are font
   * sizes and needs them registered explicitly in `packages/ui/lib/utils.ts`.
   * A step missing from that list is dropped whenever it meets a colour class,
   * silently, so the two lists have to agree.
   */
  it("registers every step with tailwind-merge so cn() cannot drop it", () => {
    const utils = readFileSync(resolve(repoRoot, "packages/ui/lib/utils.ts"), "utf8");
    for (const [name] of SCALE) {
      expect(utils, `${name} missing from the tailwind-merge font-size group`)
        .toContain(`"${name}"`);
    }
  });

  it("product UI writes no font size outside the scale", () => {
    const violations: string[] = [];

    for (const root of scanRoots) {
      for (const path of collectSourceFiles(resolve(repoRoot, root))) {
        const rel = relative(repoRoot, path);
        const lines = stripComments(readFileSync(path, "utf8")).split("\n");
        lines.forEach((line, index) => {
          for (const { label, regex, hint, appliesTo } of bannedPatterns) {
            if (!appliesTo(rel)) continue;
            regex.lastIndex = 0;
            for (const match of line.matchAll(regex)) {
              if (rawCssExemptions.some((exempt) => exempt.test(match[0]))) continue;
              violations.push(
                `${rel}:${index + 1}  ${match[0].trim()}  (${label} — ${hint})`,
              );
            }
          }
        });
      }
    }

    expect(violations, `Font sizes off the scale:\n${violations.join("\n")}`).toEqual([]);
  });
});

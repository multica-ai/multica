#!/usr/bin/env node
/**
 * Catches focus rings that `outline-none` erases before they are drawn.
 *
 * In Tailwind v4, `outline-none` and `outline-hidden` set
 * `--tw-outline-style: none`, and every outline width utility (`outline`,
 * `outline-2`, `outline-[3px]`) takes its `outline-style` from that variable.
 * On one element, `outline-none focus-visible:outline-2` therefore draws no
 * ring at all. A style utility under the same variants, such as
 * `focus-visible:outline-solid`, sets the variable back.
 *
 * A width is reported when it sits behind at least one variant, a reset is on
 * the same element, and no `outline-solid`/`dashed`/`dotted`/`double` applies
 * under the same or fewer variants. Rings on a pseudo-element or a child
 * (`after:`, `before:`, `*:`, `[&_svg]:`) are a different element: the
 * variable is registered with `inherits: false`, so the reset never reaches
 * them.
 *
 * Classes that land on one element are grouped as described in
 * `ui-class-groups.mjs`. apps/mobile is skipped: NativeWind runs Tailwind v3,
 * where `outline-none` is a transparent outline and widths do not read the
 * variable.
 *
 * Run: node scripts/check-ui-focus-outlines.mjs
 */

import { readFileSync, realpathSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { classGroups, repoPath, sourceFiles } from "./ui-class-groups.mjs";

const elementVariants = new Set([
  "after",
  "before",
  "backdrop",
  "placeholder",
  "file",
  "marker",
  "selection",
  "first-line",
  "first-letter",
  "details-content",
  "*",
  "**",
]);

/** Splits `focus-visible:after:outline-2!` into its variants and utility. */
function parseToken(token) {
  const variants = [];
  let depth = 0;
  let start = 0;
  for (let index = 0; index < token.length; index++) {
    const char = token[index];
    if (char === "[" || char === "(") depth++;
    else if (char === "]" || char === ")") depth--;
    else if (char === ":" && depth === 0) {
      variants.push(token.slice(start, index));
      start = index + 1;
    }
  }
  const utility = token.slice(start).replace(/^!/, "").replace(/!$/, "");
  const element = variants.filter((variant) => elementVariants.has(variant) || variant.startsWith("["));
  const conditions = variants.filter((variant) => !element.includes(variant));
  return { variants, utility, element: element.join(":"), conditions };
}

function outlineRole(utility) {
  if (utility === "outline-none" || utility === "outline-hidden") return "reset";
  if (/^outline-(?:solid|dashed|dotted|double)$/.test(utility)) return "style";
  if (
    utility === "outline" ||
    /^outline-\d+$/.test(utility) ||
    /^outline-\[(?:length:[^\]]+|\d*\.?\d+[a-z%]*)\]$/.test(utility) ||
    /^outline-\(length:[^)]+\)$/.test(utility)
  ) {
    return "width";
  }
  return null;
}

/** Checks class strings that land on one element; each finding keeps its source string. */
export function classGroupViolations(strings) {
  const resets = new Set();
  const styles = [];
  const widths = [];

  for (const string of strings) {
    for (const token of string.text.split(/\s+/)) {
      if (!token) continue;
      const parsed = parseToken(token);
      const role = outlineRole(parsed.utility);
      if (role === "reset") resets.add(parsed.element);
      else if (role === "style") styles.push(parsed);
      else if (role === "width" && parsed.conditions.length > 0) widths.push({ string, token, ...parsed });
    }
  }

  return widths
    .filter(
      (width) =>
        resets.has(width.element) &&
        !styles.some(
          (style) =>
            style.element === width.element &&
            style.conditions.every((condition) => width.conditions.includes(condition)),
        ),
    )
    .map(({ string, token, variants }) => ({
      string,
      token,
      fix: [...variants, "outline-solid"].join(":"),
    }));
}

export function focusOutlineViolations(sourceText, filePath) {
  const file = repoPath(filePath);
  return classGroups(sourceText, filePath).flatMap((strings) =>
    classGroupViolations(strings).map(({ string, token, fix }) => ({
      file,
      line: string.line,
      column: string.column,
      token,
      fix,
    })),
  );
}

function main() {
  const files = sourceFiles();
  const violations = files.flatMap((file) =>
    focusOutlineViolations(readFileSync(file, "utf8"), file),
  );

  if (violations.length > 0) {
    console.error(`Focus outlines that outline-none cancels (${violations.length})`);
    for (const violation of violations) {
      console.error(
        `  ${violation.file}:${violation.line}:${violation.column}  ${violation.token} draws nothing; add \`${violation.fix}\``,
      );
    }
    console.error(
      "\nIn Tailwind v4, `outline-none` and `outline-hidden` set `--tw-outline-style: none`, and `outline-<n>` takes its style from that variable, so on the same element the ring is never drawn. Add `focus-visible:outline-solid` (under the same variants as the width) in the same place.",
    );
    process.exit(1);
  }

  console.log(`UI focus outlines clean (${files.length} source files checked).`);
}

if (process.argv[1] && realpathSync(process.argv[1]) === realpathSync(fileURLToPath(import.meta.url))) main();

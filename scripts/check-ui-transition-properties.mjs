#!/usr/bin/env node
/**
 * Catches hand-written `transition-[…]` lists that mix up `transform` and
 * the standalone transform properties.
 *
 * In Tailwind v4, `translate-*`, `scale-*` and `rotate-*` write the standalone
 * CSS `translate`, `scale` and `rotate` properties, not `transform`. A list
 * such as `transition-[transform,opacity]` therefore snaps those utilities
 * instead of animating them. The 3D `rotate-x/y/z-*` utilities are the
 * reverse case: they still compose `transform`, so `transition-[rotate]`
 * misses them. `transition` and `transition-transform` already include all
 * four properties; only arbitrary lists can drift.
 *
 * Only that mix-up is reported. A list that names neither property, such as
 * `transition-[width]` next to a static `-translate-x-1/2`, is left alone.
 *
 * Classes that land on one element are grouped as described in
 * `ui-class-groups.mjs`. apps/mobile is skipped: NativeWind runs Tailwind v3,
 * where these utilities do go through `transform`.
 *
 * Run: node scripts/check-ui-transition-properties.mjs
 */

import { readFileSync, realpathSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { classGroups, repoPath, sourceFiles } from "./ui-class-groups.mjs";

function utilityName(token) {
  const withoutVariant = token.slice(token.lastIndexOf(":") + 1);
  return withoutVariant.replace(/^!/, "").replace(/!$/, "");
}

/** The CSS property a transform utility writes, or null for other classes. */
function transformProperty(utility) {
  const match = utility.match(/^-?(translate|scale|rotate)-(?:([xyz])-)?/);
  if (!match) return null;
  if (match[1] === "rotate" && match[2]) return "transform";
  return match[1];
}

function isMismatch(listed, property) {
  if (property === "transform") return listed.has("rotate") && !listed.has("transform");
  return listed.has("transform") && !listed.has(property);
}

/** Checks class strings that land on one element; each finding keeps its source string. */
export function classGroupViolations(strings) {
  const listed = new Set();
  const movers = [];

  for (const string of strings) {
    for (const token of string.text.split(/\s+/)) {
      if (!token) continue;
      const utility = utilityName(token);
      const transition = utility.match(/^transition-\[([^\]]+)\]$/);
      if (transition) {
        for (const property of transition[1].split(",")) {
          listed.add(property.replace(/_/g, " ").trim());
        }
        continue;
      }
      const property = transformProperty(utility);
      if (property) movers.push({ string, token, property });
    }
  }

  return movers.filter((mover) => isMismatch(listed, mover.property));
}

export function transitionViolations(sourceText, filePath) {
  const file = repoPath(filePath);
  return classGroups(sourceText, filePath).flatMap((strings) =>
    classGroupViolations(strings).map(({ string, token, property }) => ({
      file,
      line: string.line,
      column: string.column,
      token,
      property,
    })),
  );
}

function main() {
  const files = sourceFiles();
  const violations = files.flatMap((file) =>
    transitionViolations(readFileSync(file, "utf8"), file),
  );

  if (violations.length > 0) {
    console.error(`transition-[…] lists that confuse transform with translate/scale/rotate (${violations.length})`);
    for (const violation of violations) {
      console.error(
        `  ${violation.file}:${violation.line}:${violation.column}  ${violation.token} needs \`${violation.property}\``,
      );
    }
    console.error(
      "\nTailwind v4 `translate-*`, `scale-*` and `rotate-*` write the standalone `translate`, `scale` and `rotate` properties, while `rotate-x/y/z-*` compose `transform`. Name the property the class writes, e.g. `transition-[translate,opacity]`, or use `transition-transform`.",
    );
    process.exit(1);
  }

  console.log(`UI transition properties clean (${files.length} source files checked).`);
}

if (process.argv[1] && realpathSync(process.argv[1]) === realpathSync(fileURLToPath(import.meta.url))) main();

#!/usr/bin/env node
/**
 * Catches hand-written `transition-[…]` lists that cannot animate the
 * transform utilities sitting next to them.
 *
 * In Tailwind v4, `translate-*`, `scale-*` and `rotate-*` write the standalone
 * CSS `translate`, `scale` and `rotate` properties, not `transform`. A list
 * such as `transition-[transform,opacity]` therefore snaps those utilities
 * instead of animating them. `transition` and `transition-transform` already
 * include all four properties; only arbitrary lists can drift. The 3D
 * `rotate-x/y/z-*` utilities still compose `transform`.
 *
 * Matching is per string literal (a template literal's static text counts as
 * one string). Classes split across `cn()` arguments are not followed.
 * apps/mobile is skipped: NativeWind runs Tailwind v3, where these utilities
 * do go through `transform`.
 *
 * Run: node scripts/check-ui-transition-properties.mjs
 */

import { readdirSync, readFileSync, realpathSync } from "node:fs";
import { extname, join, relative, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";
import ts from "typescript";

const repoRoot = resolve(import.meta.dirname, "..");
const roots = [
  "apps/web",
  "apps/desktop",
  "apps/docs",
  "apps/storybook",
  "packages/ui",
  "packages/views",
  "packages/ee",
];
const extensions = new Set([".ts", ".tsx", ".js", ".jsx", ".mjs", ".mdx"]);
const skippedDirectories = new Set(["node_modules", ".next", ".turbo", "dist", "build", "out"]);

function walk(directory, files = []) {
  for (const entry of readdirSync(directory, { withFileTypes: true })) {
    if (entry.name.startsWith(".") || skippedDirectories.has(entry.name)) continue;
    const path = join(directory, entry.name);
    if (entry.isDirectory()) walk(path, files);
    else if (extensions.has(extname(entry.name))) files.push(path);
  }
  return files;
}

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

export function classStringViolations(text) {
  const listed = new Set();
  let hasArbitraryTransition = false;
  const movers = [];

  for (const token of text.split(/\s+/)) {
    if (!token) continue;
    const utility = utilityName(token);
    const transition = utility.match(/^transition-\[([^\]]+)\]$/);
    if (transition) {
      hasArbitraryTransition = true;
      for (const property of transition[1].split(",")) listed.add(property.trim());
      continue;
    }
    const property = transformProperty(utility);
    if (property) movers.push({ token, property });
  }

  if (!hasArbitraryTransition) return [];
  if (listed.has("all") || [...listed].some((property) => property.startsWith("var("))) return [];
  return movers.filter((mover) => !listed.has(mover.property));
}

function classStrings(node) {
  if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) return node.text;
  if (ts.isTemplateExpression(node)) {
    return [node.head.text, ...node.templateSpans.map((span) => span.literal.text)].join(" ");
  }
  return null;
}

export function transitionViolations(sourceText, filePath) {
  const source = ts.createSourceFile(filePath, sourceText, ts.ScriptTarget.Latest, true);
  const relativePath = relative(repoRoot, filePath).split(sep).join("/");
  const violations = [];

  const visit = (node) => {
    const text = classStrings(node);
    if (text !== null) {
      for (const { token, property } of classStringViolations(text)) {
        const { line, character } = source.getLineAndCharacterOfPosition(node.getStart(source));
        violations.push({
          file: relativePath,
          line: line + 1,
          column: character + 1,
          token,
          property,
        });
      }
    }
    ts.forEachChild(node, visit);
  };
  visit(source);
  return violations;
}

function main() {
  const files = roots.flatMap((root) => walk(join(repoRoot, root)));
  const violations = files.flatMap((file) =>
    transitionViolations(readFileSync(file, "utf8"), file),
  );

  if (violations.length > 0) {
    console.error(`transition-[…] lists missing a transform property (${violations.length})`);
    for (const violation of violations) {
      console.error(
        `  ${violation.file}:${violation.line}:${violation.column}  ${violation.token} needs \`${violation.property}\``,
      );
    }
    console.error(
      "\nTailwind v4 `translate-*`, `scale-*` and `rotate-*` write the standalone `translate`, `scale` and `rotate` properties. Name them in the list, e.g. `transition-[translate,opacity]`, or use `transition-transform`.",
    );
    process.exit(1);
  }

  console.log(`UI transition properties clean (${files.length} source files checked).`);
}

if (process.argv[1] && realpathSync(process.argv[1]) === realpathSync(fileURLToPath(import.meta.url))) main();

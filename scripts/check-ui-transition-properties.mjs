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
 * Classes are grouped per string literal (a template literal's static text
 * counts as one string), and the string arguments of one direct `cn()` or
 * `clsx()` call — including ternary, `&&`/`||`/`??` and array branches — are
 * grouped together. Classes passed through variables are not followed.
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

function classStrings(node) {
  if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) return node.text;
  if (ts.isTemplateExpression(node)) {
    return [node.head.text, ...node.templateSpans.map((span) => span.literal.text)].join(" ");
  }
  return null;
}

const classMergers = new Set(["cn", "clsx"]);
const branchOperators = new Set([
  ts.SyntaxKind.AmpersandAmpersandToken,
  ts.SyntaxKind.BarBarToken,
  ts.SyntaxKind.QuestionQuestionToken,
]);

/** String nodes an argument of `cn()` can resolve to without following variables. */
function mergedStringNodes(node, nodes = []) {
  if (classStrings(node) !== null) nodes.push(node);
  else if (ts.isParenthesizedExpression(node)) mergedStringNodes(node.expression, nodes);
  else if (ts.isConditionalExpression(node)) {
    mergedStringNodes(node.whenTrue, nodes);
    mergedStringNodes(node.whenFalse, nodes);
  } else if (ts.isBinaryExpression(node) && branchOperators.has(node.operatorToken.kind)) {
    mergedStringNodes(node.left, nodes);
    mergedStringNodes(node.right, nodes);
  } else if (ts.isArrayLiteralExpression(node)) {
    for (const element of node.elements) mergedStringNodes(element, nodes);
  }
  return nodes;
}

export function transitionViolations(sourceText, filePath) {
  const source = ts.createSourceFile(filePath, sourceText, ts.ScriptTarget.Latest, true);
  const relativePath = relative(repoRoot, filePath).split(sep).join("/");
  const violations = [];
  const grouped = new Set();

  const check = (nodes) => {
    const strings = nodes.map((node) => ({ text: classStrings(node), node }));
    for (const { string, token, property } of classGroupViolations(strings)) {
      const { line, character } = source.getLineAndCharacterOfPosition(string.node.getStart(source));
      violations.push({
        file: relativePath,
        line: line + 1,
        column: character + 1,
        token,
        property,
      });
    }
  };

  const visit = (node) => {
    if (
      ts.isCallExpression(node) &&
      ts.isIdentifier(node.expression) &&
      classMergers.has(node.expression.text)
    ) {
      const nodes = node.arguments.flatMap((argument) => mergedStringNodes(argument));
      for (const string of nodes) grouped.add(string);
      check(nodes);
    } else if (classStrings(node) !== null && !grouped.has(node)) {
      check([node]);
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

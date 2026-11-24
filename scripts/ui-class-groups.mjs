/**
 * Source scanning shared by the UI class checks that need to know which
 * classes land on one element.
 *
 * Classes are grouped per string literal (a template literal's static text
 * counts as one string), and the string arguments of one direct `cn()` or
 * `clsx()` call — including ternary, `&&`/`||`/`??` and array branches — are
 * grouped together. Classes passed through variables are not followed.
 */

import { readdirSync } from "node:fs";
import { extname, join, relative, resolve, sep } from "node:path";
import ts from "typescript";

export const repoRoot = resolve(import.meta.dirname, "..");

/** Workspaces styled by Tailwind v4. apps/mobile runs NativeWind on Tailwind v3. */
export const tailwindV4Roots = [
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

function walk(directory, files) {
  for (const entry of readdirSync(directory, { withFileTypes: true })) {
    if (entry.name.startsWith(".") || skippedDirectories.has(entry.name)) continue;
    const path = join(directory, entry.name);
    if (entry.isDirectory()) walk(path, files);
    else if (extensions.has(extname(entry.name))) files.push(path);
  }
  return files;
}

export function sourceFiles(roots = tailwindV4Roots) {
  return roots.flatMap((root) => walk(join(repoRoot, root), []));
}

export function repoPath(filePath) {
  return relative(repoRoot, filePath).split(sep).join("/");
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

/**
 * Every group of class strings in a file. Each string carries its text and a
 * 1-based `line` and `column`.
 */
export function classGroups(sourceText, filePath) {
  const source = ts.createSourceFile(filePath, sourceText, ts.ScriptTarget.Latest, true);
  const groups = [];
  const grouped = new Set();

  const add = (nodes) => {
    groups.push(
      nodes.map((node) => {
        const { line, character } = source.getLineAndCharacterOfPosition(node.getStart(source));
        return { text: classStrings(node), line: line + 1, column: character + 1 };
      }),
    );
  };

  const visit = (node) => {
    if (
      ts.isCallExpression(node) &&
      ts.isIdentifier(node.expression) &&
      classMergers.has(node.expression.text)
    ) {
      const nodes = node.arguments.flatMap((argument) => mergedStringNodes(argument));
      for (const string of nodes) grouped.add(string);
      add(nodes);
    } else if (classStrings(node) !== null && !grouped.has(node)) {
      add([node]);
    }
    ts.forEachChild(node, visit);
  };
  visit(source);
  return groups;
}

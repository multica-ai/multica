#!/usr/bin/env node
/**
 * Keeps product radii on the shared taxonomy.
 *
 * Bare Tailwind `rounded` is a hidden 4px constant in Tailwind v4, bypassing
 * our `--radius-*` scale. Pixel-arbitrary radii create the same drift. The few
 * 2–3px data-visualization marks below are intentional micro-geometry, not
 * product surfaces, and are allowlisted by exact file so the exception cannot
 * spread silently.
 *
 * Run: node scripts/check-ui-radius-tokens.mjs
 */

import { readdirSync, readFileSync } from "node:fs";
import { extname, join, relative, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";
import ts from "typescript";

const repoRoot = resolve(import.meta.dirname, "..");
const roots = ["apps/web", "apps/desktop", "apps/mobile", "packages/ui", "packages/views"];
const extensions = new Set([".ts", ".tsx", ".js", ".jsx", ".mjs", ".mdx"]);
const skippedDirectories = new Set(["node_modules", ".next", ".turbo", "dist", "build", "out"]);

const microGeometryAllowlist = new Set([
  "apps/web/features/landing/components/features-section.tsx:rounded-[2px]",
  "packages/ui/components/ui/chart.tsx:rounded-[2px]",
  "packages/views/common/task-transcript/run-timeline.tsx:rounded-[3px]",
  "packages/views/dashboard/components/errors-tab.tsx:rounded-[2px]",
  "packages/views/issues/components/gantt-view.tsx:rounded-[2px]",
  "packages/views/runtimes/components/charts/activity-heatmap.tsx:rounded-[2px]",
]);

function walk(directory, files = []) {
  for (const entry of readdirSync(directory, { withFileTypes: true })) {
    if (entry.name.startsWith(".") || skippedDirectories.has(entry.name)) continue;
    const path = join(directory, entry.name);
    if (entry.isDirectory()) walk(path, files);
    else if (extensions.has(extname(entry.name))) files.push(path);
  }
  return files;
}

function isStringSegment(node) {
  return (
    ts.isStringLiteral(node) ||
    ts.isNoSubstitutionTemplateLiteral(node) ||
    node.kind === ts.SyntaxKind.TemplateHead ||
    node.kind === ts.SyntaxKind.TemplateMiddle ||
    node.kind === ts.SyntaxKind.TemplateTail
  );
}

function utilityName(token) {
  const withoutVariant = token.slice(token.lastIndexOf(":") + 1);
  return withoutVariant.replace(/^!/, "").replace(/!$/, "");
}

export function radiusViolations(sourceText, filePath) {
  const source = ts.createSourceFile(filePath, sourceText, ts.ScriptTarget.Latest, true);
  const relativePath = relative(repoRoot, filePath).split(sep).join("/");
  const violations = [];

  const visit = (node) => {
    if (isStringSegment(node)) {
      for (const token of node.text.split(/\s+/)) {
        const utility = utilityName(token);
        const arbitrary = utility.match(
          /^rounded(?:-(?:t|r|b|l|s|e|x|y|tl|tr|br|bl|ss|se|es|ee))?-\[(\d+(?:\.\d+)?)px\]$/,
        );
        const allowKey = `${relativePath}:${utility}`;
        if (utility === "rounded" || (arbitrary && !microGeometryAllowlist.has(allowKey))) {
          const { line, character } = source.getLineAndCharacterOfPosition(node.getStart(source));
          violations.push({
            file: relativePath,
            line: line + 1,
            column: character + 1,
            token,
          });
        }
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
    radiusViolations(readFileSync(file, "utf8"), file),
  );

  if (violations.length > 0) {
    console.error(`Un-tokenized product radius utilities (${violations.length})`);
    for (const violation of violations) {
      console.error(
        `  ${violation.file}:${violation.line}:${violation.column}  ${violation.token}`,
      );
    }
    console.error(
      "\nUse a named `rounded-*` token. Add a narrowly scoped allowlist only for genuine data-visualization micro-geometry.",
    );
    process.exit(1);
  }

  console.log(`UI radius tokens clean (${files.length} source files checked).`);
}

if (resolve(process.argv[1] ?? "") === fileURLToPath(import.meta.url)) main();

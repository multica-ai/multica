#!/usr/bin/env node
/**
 * Keeps product radii on the shared taxonomy.
 *
 * Bare Tailwind radius utilities are hidden constants in Tailwind v4,
 * bypassing our `--radius-*` scale. Fixed arbitrary lengths create the same
 * drift. The few 2–3px data-visualization marks below are intentional
 * micro-geometry, not product surfaces, and are allowlisted by exact file so
 * the exception cannot spread silently.
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
const radiusNames = ["xs", "sm", "md", "lg", "xl", "2xl", "3xl", "4xl"];
const directionalRadius = "(?:t|r|b|l|s|e|x|y|tl|tr|br|bl|ss|se|es|ee)";
const fixedCssLength = /^(?:\d+(?:\.\d+)?|\.\d+)(?:px|rem|em|%|cm|mm|in|pt|pc|q|vh|vw|vmin|vmax|ch|ex|lh|rlh|cap|ic)$/;

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
        const bare = new RegExp(`^rounded(?:-${directionalRadius})?$`).test(utility);
        const arbitrary = utility.match(
          new RegExp(`^rounded(?:-${directionalRadius})?-\\[([^\\]]+)\\]$`),
        );
        const fixedArbitrary = arbitrary && fixedCssLength.test(arbitrary[1]);
        const allowKey = `${relativePath}:${utility}`;
        if (bare || (fixedArbitrary && !microGeometryAllowlist.has(allowKey))) {
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

/** Ensure NativeWind resolves every named radius to the web token value. */
export function radiusScaleViolations(webTokensSource, mobileRadiusTokens) {
  const baseMatch = webTokensSource.match(/--radius:\s*(\d+(?:\.\d+)?)rem\s*;/);
  if (!baseMatch) return ["web base radius is missing"];

  const basePx = Number(baseMatch[1]) * 16;
  const violations = [];
  for (const name of radiusNames) {
    const declaration = webTokensSource.match(
      new RegExp(`--radius-${name}:\\s*([^;]+);`),
    )?.[1].trim();
    if (!declaration) {
      violations.push(`${name}: web token is missing`);
      continue;
    }

    const multiplier = declaration === "var(--radius)"
      ? 1
      : Number(declaration.match(
        /^calc\(var\(--radius\) \* (\d+(?:\.\d+)?)\)$/,
      )?.[1]);
    if (!Number.isFinite(multiplier)) {
      violations.push(`${name}: unsupported web declaration ${declaration}`);
      continue;
    }

    const webValue = basePx * multiplier;
    const mobileValue = mobileRadiusTokens[name];
    if (mobileValue !== webValue) {
      violations.push(`${name}: web=${webValue}px mobile=${String(mobileValue)}px`);
    }
  }
  return violations;
}

function main() {
  const files = roots.flatMap((root) => walk(join(repoRoot, root)));
  const violations = files.flatMap((file) =>
    radiusViolations(readFileSync(file, "utf8"), file),
  );

  const scaleViolations = radiusScaleViolations(
    readFileSync(join(repoRoot, "packages/ui/styles/tokens.css"), "utf8"),
    JSON.parse(readFileSync(join(repoRoot, "apps/mobile/lib/radius-tokens.json"), "utf8")),
  );

  if (scaleViolations.length > 0) {
    console.error(`Web/mobile radius scale drift (${scaleViolations.length})`);
    for (const violation of scaleViolations) console.error(`  ${violation}`);
  }

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
  }

  if (scaleViolations.length > 0 || violations.length > 0) process.exit(1);

  console.log(`UI radius tokens clean (${files.length} source files checked).`);
}

if (resolve(process.argv[1] ?? "") === fileURLToPath(import.meta.url)) main();

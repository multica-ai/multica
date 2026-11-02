import { readFileSync, readdirSync, existsSync } from "node:fs";
import { dirname, relative, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import ts from "typescript";
import policy from "./branding-policy.json" with { type: "json" };

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const retiredBrand = /\b(?:Multica|Mika)\b/;

export function hasRetiredBrand(text) {
  for (const { value } of policy.allowedTokens) text = text.replaceAll(value, "");
  return retiredBrand.test(text);
}

/** Read literal copy, including JSX and template fragments, without matching identifiers/comments. */
export function sourceViolations(path, source) {
  const file = ts.createSourceFile(path, source, ts.ScriptTarget.Latest, true);
  const violations = [];
  function visit(node) {
    if ((ts.isStringLiteralLike(node) || ts.isTemplateLiteralToken(node) || ts.isJsxText(node))
      && hasRetiredBrand(node.text)) {
      const { line } = file.getLineAndCharacterOfPosition(node.getStart(file));
      violations.push(`${path}:${line + 1}: ${node.text.trim().slice(0, 150)}`);
    }
    ts.forEachChild(node, visit);
  }
  visit(file);
  return violations;
}

function* sources(directory) {
  for (const entry of readdirSync(directory, { withFileTypes: true })) {
    if (["node_modules", "dist", "out", ".next", ".source", ".turbo", "ios", "android", "test", "e2e"].includes(entry.name)) continue;
    const path = resolve(directory, entry.name);
    if (entry.isDirectory()) yield* sources(path);
    else if (/\.(?:[cm]?[jt]sx?|html|json)$/.test(entry.name)
      && !/\.(?:test|spec|d)\./.test(entry.name)) yield path;
  }
}

export function checkSources(sourceRoot = root) {
  const violations = [];
  for (const directory of ["packages/core", "packages/ui", "packages/views", "apps/web", "apps/desktop", "apps/mobile"]) {
    for (const path of sources(resolve(sourceRoot, directory))) {
      const name = relative(sourceRoot, path).replaceAll("\\", "/");
      if (policy.sourceExceptions.some((entry) => entry.path === name)) continue;
      // Raw upstream JSON is deliberately unchanged; effective bundles are
      // checked through real i18next in the views/mobile locale tests.
      if (/\/locales\/(?:en|zh-Hans|zh-Hant|ja|ko|fr)\//.test(name)) continue;
      const content = readFileSync(path, "utf8");
      if (name.endsWith("/package.json")) {
        const { author: _author, contributors: _contributors, ...metadata } = JSON.parse(content);
        // Package authors/contributors are upstream attribution, not app names.
        if (hasRetiredBrand(JSON.stringify(metadata))) violations.push(`${name}: retired brand in package display metadata`);
      } else if (/\.(?:json|html)$/.test(path)) {
        if (hasRetiredBrand(content)) violations.push(`${name}: retired brand in static copy`);
      } else {
        violations.push(...sourceViolations(name, content));
      }
    }
  }
  return violations;
}

// Deployment inputs must not activate the dormant upstream analytics code.
export function checkAnalyticsDeployment(sourceRoot = root) {
  const violations = [];
  function inspect(path) {
    if (!existsSync(path)) return;
    for (const entry of readdirSync(path, { withFileTypes: true })) {
      if (["node_modules", ".git"].includes(entry.name)) continue;
      const child = resolve(path, entry.name);
      if (entry.isDirectory()) inspect(child);
      else checkFile(child);
    }
  }
  function checkFile(path) {
    const content = readFileSync(path, "utf8");
    if (/GOOGLE_ANALYTICS_ID|googleAnalyticsId/.test(content)) {
      violations.push(`${relative(sourceRoot, path)}: Google Analytics deployment configuration is forbidden`);
    }
  }
  for (const entry of readdirSync(sourceRoot, { withFileTypes: true })) {
    if (entry.isFile() && /^(?:docker-compose.*\.ya?ml|\.env(?:\..*)?)$/.test(entry.name)) checkFile(resolve(sourceRoot, entry.name));
  }
  for (const name of ["deploy", ".github/workflows", ".github/actions"]) inspect(resolve(sourceRoot, name));
  return violations;
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  const violations = [...checkSources(process.argv[2]), ...checkAnalyticsDeployment(process.argv[2])];
  if (violations.length) {
    console.error(`Retired product branding:\n${violations.join("\n")}`);
    process.exitCode = 1;
  } else console.log("Brand source guard passed.");
}

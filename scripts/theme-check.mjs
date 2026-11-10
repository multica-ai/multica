#!/usr/bin/env node
/**
 * Checks theme folders or .zip files the way the app and the theme store do:
 * the manifest, the CSS sanitizer, public variables, Latin font stacks and
 * WCAG contrast against the app's own palette (packages/ui/styles/tokens.css).
 * The checks live in packages/core/appearance/theme-check.ts.
 *
 * Run: pnpm theme:check <theme folder or .zip>... [--json]
 * Exits 1 when any theme has an error.
 */

import { readFileSync, readdirSync, statSync } from "node:fs";
import { register } from "node:module";
import { basename, join, relative, resolve, sep } from "node:path";
import { pathToFileURL } from "node:url";

// The core sources import each other without extensions; resolve them to .ts
// so Node's type stripping can run them as they are.
register(
  "data:text/javascript," +
    encodeURIComponent(`
      export async function resolve(specifier, context, next) {
        try {
          return await next(specifier, context);
        } catch (error) {
          if (!specifier.startsWith(".") || !context.parentURL?.endsWith(".ts")) throw error;
          return next(specifier + ".ts", context);
        }
      }
    `),
);

const repoRoot = resolve(import.meta.dirname, "..");
const core = (file) => pathToFileURL(join(repoRoot, "packages/core/appearance", file)).href;
const { checkThemeFiles } = await import(core("theme-check.ts"));
const { basePaletteFromTokens } = await import(core("theme-contrast.ts"));
const { readZip } = await import(core("theme-archive.ts"));
const { THEME_PACKAGE_MAX_BYTES } = await import(core("theme-package.ts"));

const SKIPPED = new Set(["node_modules", ".git"]);

function readFolder(root, dir = root, files = []) {
  for (const entry of readdirSync(dir)) {
    if (SKIPPED.has(entry) || entry.startsWith(".")) continue;
    const path = join(dir, entry);
    if (statSync(path).isDirectory()) readFolder(root, path, files);
    else files.push({ path: relative(root, path).split(sep).join("/"), data: new Uint8Array(readFileSync(path)) });
  }
  return files;
}

async function readTheme(path) {
  if (statSync(path).isDirectory()) return { files: readFolder(path) };
  const archive = await readZip(new Uint8Array(readFileSync(path)), THEME_PACKAGE_MAX_BYTES);
  return archive.ok ? { files: archive.files } : { error: archive.error };
}

const args = process.argv.slice(2);
const json = args.includes("--json");
const targets = args.filter((arg) => arg !== "--json");
if (targets.length === 0) {
  console.error("Usage: pnpm theme:check <theme folder or .zip>... [--json]");
  process.exit(2);
}

const base = basePaletteFromTokens(readFileSync(join(repoRoot, "packages/ui/styles/tokens.css"), "utf8"));
const reports = [];
for (const target of targets) {
  const path = resolve(process.env.INIT_CWD ?? process.cwd(), target);
  let read;
  try {
    read = await readTheme(path);
  } catch (error) {
    read = { error: error instanceof Error ? error.message : String(error) };
  }
  const report = read.error
    ? { ok: false, issues: [{ level: "error", message: read.error }], contrast: [] }
    : checkThemeFiles(read.files, base);
  reports.push({ target, path, ...report });
}

if (json) {
  const out = reports.map(({ path, ok, theme, issues, contrast }) => ({
    path,
    ok,
    theme: theme ? { id: theme.id, name: theme.name, version: theme.version ?? null } : null,
    issues,
    contrast,
  }));
  console.log(JSON.stringify(out, null, 2));
} else {
  for (const { target, path, ok, theme, issues, contrast } of reports) {
    const title = theme ? `${theme.name} (${theme.id}${theme.version ? ` ${theme.version}` : ""})` : basename(path);
    console.log(`\n${ok ? "✓" : "✗"} ${title}  ${target}`);
    for (const issue of issues) console.log(`  ${issue.level === "error" ? "error  " : "warning"}  ${issue.message}`);
    if (contrast.length > 0) {
      const failing = contrast.filter((c) => c.ratio < c.required).length;
      const lowest = [...contrast].sort((a, b) => a.ratio / a.required - b.ratio / b.required)[0];
      console.log(
        `  contrast  ${contrast.length} pairs checked, ${failing} below WCAG AA. Lowest: ${lowest.foreground} on ${lowest.background}, ${lowest.mode}${lowest.area === "frame" ? " frame" : ""}, ${lowest.ratio.toFixed(2)}:1 (needs ${lowest.required}:1)`,
      );
    }
  }
  const errors = reports.reduce((n, r) => n + r.issues.filter((i) => i.level === "error").length, 0);
  const warnings = reports.reduce((n, r) => n + r.issues.filter((i) => i.level === "warning").length, 0);
  console.log(`\n${errors === 0 ? "Passed" : "Failed"}: ${errors} error${errors === 1 ? "" : "s"}, ${warnings} warning${warnings === 1 ? "" : "s"}.`);
}

process.exit(reports.every((report) => report.ok) ? 0 : 1);

#!/usr/bin/env node
/**
 * Checks theme folders or .zip files the way the app and the theme store do:
 * the manifest, the CSS sanitizer, public variables, Latin font stacks and
 * WCAG contrast against the app's own palette (packages/ui/styles/tokens.css),
 * and Multica's own rule that a switch's on and off tracks differ by 3:1,
 * at the default options and at each other `select` and `toggle` value.
 * The checks live in packages/core/appearance/theme-check.ts.
 *
 * Run: pnpm theme:check <theme folder or .zip>... [--json]
 * Exits 1 when any theme has an error.
 */

import { readFileSync } from "node:fs";
import { basename, join, resolve } from "node:path";
import { appearance, readTheme, repoRoot } from "./theme-files.mjs";

const { checkThemeFiles } = await appearance("theme-check.ts");
const { basePaletteFromTokens } = await appearance("theme-contrast.ts");

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
    // Switch pairs are Multica's own rule, so they are counted apart from WCAG.
    const wcag = contrast.filter((c) => c.area !== "switch");
    const switches = contrast.filter((c) => c.area === "switch");
    const lowestOf = (checks) => [...checks].sort((a, b) => a.ratio / a.required - b.ratio / b.required)[0];
    const label = (c) => `${c.foreground} on ${c.background}, ${[c.mode, ...(c.area === "page" || c.area === "switch" ? [] : [c.area])].join(" ")}${c.variant ? ` (${c.variant.label})` : ""}, ${c.ratio.toFixed(2)}:1 (needs ${c.required}:1)`;
    if (wcag.length > 0) {
      const failing = wcag.filter((c) => c.ratio < c.required).length;
      const values = new Set(wcag.filter((c) => c.variant).map((c) => c.variant.label)).size;
      console.log(
        `  contrast  ${wcag.length} pairs checked at the default options${values > 0 ? ` and ${values} other option value${values === 1 ? "" : "s"}` : ""}, ${failing} below WCAG AA. Lowest: ${label(lowestOf(wcag))}`,
      );
    }
    if (switches.length > 0) {
      const failing = switches.filter((c) => c.ratio < c.required).length;
      console.log(
        `  switches  ${switches.length} on/off track pairs checked, ${failing} below Multica's 3:1 rule (a product rule, not WCAG). Lowest: ${label(lowestOf(switches))}`,
      );
    }
  }
  const errors = reports.reduce((n, r) => n + r.issues.filter((i) => i.level === "error").length, 0);
  const warnings = reports.reduce((n, r) => n + r.issues.filter((i) => i.level === "warning").length, 0);
  console.log(`\n${errors === 0 ? "Passed" : "Failed"}: ${errors} error${errors === 1 ? "" : "s"}, ${warnings} warning${warnings === 1 ? "" : "s"}.`);
}

// Not process.exit(): it drops stdout still queued for a pipe, which cuts a
// --json report off at 64 KB.
process.exitCode = reports.every((report) => report.ok) ? 0 : 1;

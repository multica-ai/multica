#!/usr/bin/env node
/**
 * Renders a theme in light and dark mode on a fixture page built from the
 * app's own styles (scripts/theme-preview/fixture.html), for the theme
 * store's screenshots and for authors checking their work. The theme goes
 * through the app's parser and CSS sanitizer first; a theme that does not
 * parse is not rendered. The page cannot reach the network.
 *
 * Run: pnpm theme:preview <theme folder or .zip> [--out <dir>] [--modes light,dark]
 * Writes <out>/light.png and <out>/dark.png (default out: ./theme-preview).
 */

import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { createRequire } from "node:module";
import { join, resolve } from "node:path";
import { appearance, readTheme, repoRoot } from "./theme-files.mjs";

const { themeFromFiles, themeCss } = await appearance("theme-package.ts");
const { themeOptionAttributes, themeOptionVariables } = await appearance("themes.ts");

const WIDTH = 1280;
const HEIGHT = 800;

function parseArgs(argv) {
  const args = { out: "theme-preview", modes: ["light", "dark"], targets: [] };
  for (let i = 0; i < argv.length; i += 1) {
    if (argv[i] === "--out") args.out = argv[++i];
    else if (argv[i] === "--modes") args.modes = argv[++i].split(",").filter((m) => m === "light" || m === "dark");
    else args.targets.push(argv[i]);
  }
  return args;
}

async function compileCss() {
  const require = createRequire(join(repoRoot, "package.json"));
  const postcss = require("postcss");
  const tailwind = require("@tailwindcss/postcss");
  const from = join(repoRoot, "scripts/theme-preview/fixture.css");
  const input = `
    @import "tailwindcss" source(none);
    @import "../../packages/ui/styles/tokens.css";
    @import "../../packages/ui/styles/theme-variables.css";
    @import "../../packages/ui/styles/appearance.css";
    @import "../../packages/ui/styles/base.css";
    @custom-variant dark (&:is(.dark *));
    @source "./fixture.html";
    :root {
      --font-sans: var(--font-ui, Inter, -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif);
      --font-mono: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
    }
  `;
  const result = await postcss([tailwind({ base: repoRoot })]).process(input, { from });
  return result.css;
}

const declare = (variables) =>
  Object.entries(variables)
    .map(([name, value]) => `${name}: ${value};`)
    .join(" ");

/** The page as the app would draw it with the theme applied at its option defaults. */
function page(theme, mode, appCss) {
  const variables = themeOptionVariables(theme, undefined);
  const attributes = { "data-theme": theme.id, ...themeOptionAttributes(theme, undefined) };
  const attrs = Object.entries(attributes)
    .filter(([, value]) => value !== null)
    .map(([name, value]) => ` ${name}="${String(value).replace(/"/g, "&quot;")}"`)
    .join("");
  const adjustments = Object.keys(variables.light).length
    ? `:root.theme-light { ${declare(variables.light)} } :root.theme-dark { ${declare(variables.dark)} }`
    : "";
  const fixture = readFileSync(join(repoRoot, "scripts/theme-preview/fixture.html"), "utf8");
  // Inline <style> content cannot close its element: the sanitizer leaves no
  // markup in theme CSS, and "</" is escaped here as well.
  const style = (css) => `<style>${css.replace(/<\//g, "<\\/")}</style>`;
  return `<!doctype html><html lang="en" class="${mode} theme-${mode} is-web is-focused"${attrs}><head><meta charset="utf-8">${style(appCss)}${style(themeCss(theme))}${style(adjustments)}</head><body>${fixture}</body></html>`;
}

const args = parseArgs(process.argv.slice(2));
if (args.targets.length !== 1 || args.modes.length === 0) {
  console.error("Usage: pnpm theme:preview <theme folder or .zip> [--out <dir>] [--modes light,dark]");
  process.exit(2);
}
const target = resolve(process.env.INIT_CWD ?? process.cwd(), args.targets[0]);
const read = await readTheme(target);
const result = read.error ? { ok: false, errors: [read.error] } : themeFromFiles(read.files);
if (!result.ok) {
  console.error(`✗ ${args.targets[0]} does not pass the checks:\n${result.errors.map((e) => `  ${e}`).join("\n")}`);
  process.exit(1);
}

const out = resolve(process.env.INIT_CWD ?? process.cwd(), args.out);
mkdirSync(out, { recursive: true });
const appCss = await compileCss();
const { chromium } = createRequire(join(repoRoot, "package.json"))("@playwright/test");
const browser = await chromium.launch();
try {
  const context = await browser.newContext({
    viewport: { width: WIDTH, height: HEIGHT },
    deviceScaleFactor: 1,
    javaScriptEnabled: false,
  });
  await context.route("**/*", (route) =>
    route.request().url().startsWith("data:") ? route.continue() : route.abort(),
  );
  for (const mode of args.modes) {
    const tab = await context.newPage();
    await tab.setContent(page(result.theme, mode, appCss), { waitUntil: "load" });
    const file = join(out, `${mode}.png`);
    writeFileSync(file, await tab.screenshot({ animations: "disabled" }));
    await tab.close();
    console.log(`✓ ${result.theme.name} ${mode}: ${file}`);
  }
} finally {
  await browser.close();
}

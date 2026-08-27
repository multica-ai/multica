import { readFileSync, writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { deriveTokens, formatTokenBlock, type ThemeVariant } from "./derive";

const here = dirname(fileURLToPath(import.meta.url));
const tokensPath = resolve(here, "../styles/tokens.css");
const landingPath = resolve(here, "../../../apps/web/app/custom.css");

const LIGHT_MARK = {
  start: "    /* generated-light:start */",
  end: "    /* generated-light:end */",
};
const DARK_MARK = {
  start: "    /* generated-dark:start */",
  end: "    /* generated-dark:end */",
};

const LANDING_KEYS = [
  "--app-shell",
  "--page-canvas",
  "--surface",
  "--surface-foreground",
  "--surface-raised",
  "--surface-hover",
  "--surface-selected",
  "--surface-selected-foreground",
  "--surface-border",
  "--surface-shadow",
  "--floating-shadow",
  "--menu-shadow",
  "--background",
  "--foreground",
  "--card",
  "--card-foreground",
  "--popover",
  "--popover-foreground",
  "--primary",
  "--primary-foreground",
  "--primary-hover",
  "--primary-pressed",
  "--secondary",
  "--secondary-foreground",
  "--muted",
  "--muted-foreground",
  "--faint-foreground",
  "--accent",
  "--accent-foreground",
  "--destructive",
  "--border",
  "--input",
  "--ring",
  "--brand",
  "--brand-foreground",
  "--brand-hover",
  "--brand-pressed",
  "--success",
  "--warning",
  "--info",
  "--scrollbar-thumb",
  "--scrollbar-thumb-hover",
  "--scrollbar-track",
] as const;

export function replaceMarkedBlock(
  source: string,
  mark: { start: string; end: string },
  body: string,
): string {
  const start = source.indexOf(mark.start);
  const end = source.indexOf(mark.end);
  if (start < 0 || end < 0 || end < start) {
    throw new Error(`Missing markers ${mark.start}`);
  }
  return `${source.slice(0, start)}${mark.start}\n${body}\n${source.slice(end)}`;
}

export function generatedBlock(variant: ThemeVariant): string {
  return formatTokenBlock(deriveTokens(variant));
}

export function patchTokenFile(source: string): string {
  return replaceMarkedBlock(
    replaceMarkedBlock(source, LIGHT_MARK, generatedBlock("light")),
    DARK_MARK,
    generatedBlock("dark"),
  );
}

export function patchLandingFile(source: string): string {
  const tokens = deriveTokens("light");
  const picked = Object.fromEntries(
    LANDING_KEYS.map((key) => [key, tokens[key]]).filter(([, value]) => value !== undefined),
  );
  const start = source.indexOf(".landing-light {\n    --app-shell:");
  if (start < 0) throw new Error("landing-light color block not found");
  const end = source.indexOf("\n}", start);
  if (end < 0) throw new Error("landing-light color block unterminated");
  const head = source.slice(0, start);
  const tail = source.slice(end);
  return `${head}.landing-light {\n${formatTokenBlock(picked)}\n${tail}`;
}

function writeAll() {
  writeFileSync(tokensPath, patchTokenFile(readFileSync(tokensPath, "utf8")));
  writeFileSync(landingPath, patchLandingFile(readFileSync(landingPath, "utf8")));
}

const isMain = process.argv[1] && fileURLToPath(import.meta.url) === resolve(process.argv[1]);
if (isMain) writeAll();

import { readFileSync, readdirSync, statSync } from "node:fs";
import { dirname, extname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import ts from "typescript";

const MOBILE_ROOT = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const REPO_ROOT = resolve(MOBILE_ROOT, "../..");
const MOBILE_LOCALES = join(MOBILE_ROOT, "i18n/locales");
const SHARED_LOCALES = join(REPO_ROOT, "packages/views/locales");
const SHARED_NAMESPACES = [
  "agents",
  "auth",
  "chat",
  "common",
  "editor",
  "inbox",
  "issues",
  "labels",
  "layout",
  "members",
  "modals",
  "my-issues",
  "projects",
  "runtimes",
  "search",
  "settings",
  "squads",
  "workspace",
];

function collectFiles(directory) {
  return readdirSync(directory).flatMap((name) => {
    const path = join(directory, name);
    const stat = statSync(path);
    if (stat.isDirectory()) return collectFiles(path);
    if (!new Set([".ts", ".tsx"]).has(extname(path))) return [];
    if (path.includes(".test.") || path.includes("/__tests__/")) return [];
    return [path];
  });
}

function flatten(node, prefix = "", output = new Map()) {
  for (const [key, value] of Object.entries(node)) {
    const path = prefix ? `${prefix}.${key}` : key;
    if (typeof value === "string") output.set(path, value);
    else if (value && typeof value === "object") flatten(value, path, output);
  }
  return output;
}

function readJson(path) {
  return JSON.parse(readFileSync(path, "utf8"));
}

function placeholders(value) {
  return [...value.matchAll(/\{\{\s*([a-zA-Z0-9_]+)\s*\}\}/g)]
    .map((match) => match[1])
    .sort();
}

function sharedSourceIndex() {
  const candidates = new Map();
  for (const namespace of SHARED_NAMESPACES) {
    const en = flatten(
      readJson(join(SHARED_LOCALES, "en", `${namespace}.json`)),
    );
    const zh = flatten(
      readJson(join(SHARED_LOCALES, "zh-Hans", `${namespace}.json`)),
    );
    for (const [path, source] of en) {
      const translation = zh.get(path);
      if (!translation) continue;
      const entries = candidates.get(source) ?? [];
      entries.push({ key: `${namespace}:${path}`, translation });
      candidates.set(source, entries);
    }
  }

  const index = new Map();
  for (const [source, entries] of candidates) {
    if (new Set(entries.map((entry) => entry.translation)).size === 1) {
      index.set(source, entries[0].key);
    }
  }
  return index;
}

function translatedSources() {
  const sources = new Map();
  const dynamicCalls = [];
  for (const root of ["app", "components", "lib", "data/mutations"]) {
    for (const file of collectFiles(join(MOBILE_ROOT, root))) {
      const sourceFile = ts.createSourceFile(
        file,
        readFileSync(file, "utf8"),
        ts.ScriptTarget.Latest,
        true,
        file.endsWith(".tsx") ? ts.ScriptKind.TSX : ts.ScriptKind.TS,
      );
      function visit(node) {
        if (
          ts.isCallExpression(node) &&
          ts.isIdentifier(node.expression) &&
          node.expression.text === "translate" &&
          node.arguments[0]
        ) {
          const position = sourceFile.getLineAndCharacterOfPosition(
            node.getStart(sourceFile),
          );
          if (!ts.isStringLiteralLike(node.arguments[0])) {
            dynamicCalls.push(
              `${relative(MOBILE_ROOT, file)}:${position.line + 1}`,
            );
            ts.forEachChild(node, visit);
            return;
          }
          const source = node.arguments[0].text;
          const locations = sources.get(source) ?? [];
          locations.push(`${relative(MOBILE_ROOT, file)}:${position.line + 1}`);
          sources.set(source, locations);
        }
        ts.forEachChild(node, visit);
      }
      visit(sourceFile);
    }
  }
  return { sources, dynamicCalls };
}

const enMobile = flatten(readJson(join(MOBILE_LOCALES, "en.json")));
const zhMobile = flatten(readJson(join(MOBILE_LOCALES, "zh-Hans.json")));
const mobileSources = new Set(enMobile.values());
const sharedSources = sharedSourceIndex();
const { sources: usedSources, dynamicCalls } = translatedSources();
const errors = [];

for (const location of dynamicCalls) {
  errors.push(`translate() requires a static source string (${location})`);
}

for (const key of enMobile.keys()) {
  if (!zhMobile.has(key)) {
    errors.push(`zh-Hans is missing mobile key ${key}`);
    continue;
  }
  const enValue = enMobile.get(key);
  const zhValue = zhMobile.get(key);
  if (!zhValue.trim()) errors.push(`zh-Hans mobile key ${key} is empty`);
  if (placeholders(enValue).join(",") !== placeholders(zhValue).join(",")) {
    errors.push(`Placeholder mismatch for mobile key ${key}`);
  }
}
for (const key of zhMobile.keys()) {
  if (!enMobile.has(key)) errors.push(`English is missing mobile key ${key}`);
}
const mobileSourceKeys = new Map();
for (const [key, source] of enMobile) {
  const existing = mobileSourceKeys.get(source);
  if (existing) {
    errors.push(`Duplicate English mobile source at ${existing} and ${key}`);
  } else {
    mobileSourceKeys.set(source, key);
  }
}
for (const [source, locations] of usedSources) {
  if (!mobileSources.has(source) && !sharedSources.has(source)) {
    errors.push(
      `Missing translation for ${JSON.stringify(source)} (${locations.join(", ")})`,
    );
  }
}

if (process.argv.includes("--json")) {
  console.log(
    JSON.stringify(
      [...usedSources]
        .filter(
          ([source]) =>
            !mobileSources.has(source) && !sharedSources.has(source),
        )
        .map(([source, locations]) => ({ source, locations })),
      null,
      2,
    ),
  );
  if (dynamicCalls.length > 0) {
    console.error(
      `translate() requires a static source string: ${dynamicCalls.join(", ")}`,
    );
    process.exit(1);
  }
  process.exit(0);
}

if (errors.length > 0) {
  console.error("Mobile i18n resource validation failed:");
  for (const error of errors) console.error(`- ${error}`);
  process.exit(1);
}

console.log(`Mobile i18n resources cover ${usedSources.size} source strings.`);

import assert from "node:assert/strict";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import { ESLint, Linter } from "eslint";
import tseslint from "typescript-eslint";
import { NO_SYSTEM_ONLY_REDUCED_MOTION } from "./reduced-motion.js";

const linter = new Linter();

function flagged(code) {
  const messages = linter.verify(
    code,
    [
      {
        files: ["**/*.ts"],
        languageOptions: { parser: tseslint.parser },
        rules: { "no-restricted-syntax": ["error", NO_SYSTEM_ONLY_REDUCED_MOTION] },
      },
    ],
    "fixture.ts",
  );
  const fatal = messages.find((m) => m.fatal);
  if (fatal) throw new Error(`fixture failed to parse: ${fatal.message}`);
  return messages.length > 0;
}

const FLAGGED = [
  ["window.matchMedia", 'window.matchMedia("(prefers-reduced-motion: reduce)").matches;'],
  ["optional window.matchMedia", 'window.matchMedia?.("(prefers-reduced-motion: reduce)").matches;'],
  ["bare matchMedia", 'matchMedia("(prefers-reduced-motion: reduce)").matches;'],
  ["single quotes", "matchMedia('(prefers-reduced-motion: reduce)');"],
  ["no-preference", 'matchMedia("(prefers-reduced-motion: no-preference)");'],
  ["template literal", "matchMedia(`(prefers-reduced-motion: reduce)`);"],
  ["query kept in a constant", 'const QUERY = "(prefers-reduced-motion: reduce)";'],
  ["media-query hook", 'useMediaQuery("(prefers-reduced-motion: reduce)");'],
];

const ALLOWED = [
  ["the core helper", "prefersReducedMotion();"],
  ["another media query", 'window.matchMedia("(prefers-color-scheme: dark)").matches;'],
  ["a Tailwind variant", 'const className = "motion-reduce:transition-none";'],
  ["a sentence naming the setting", 'const hint = "Honours prefers-reduced-motion";'],
  ["a CSS rule string", 'const rule = "@media (prefers-reduced-motion: reduce) { * { transition: none; } }";'],
  ["a template literal that only mentions it", "const hint = `Follows ${name} (prefers-reduced-motion)`;"],
];

for (const [name, code] of FLAGGED) {
  test(`flags ${name}`, () => assert.equal(flagged(code), true));
}

for (const [name, code] of ALLOWED) {
  test(`allows ${name}`, () => assert.equal(flagged(code), false));
}

test("message points to the core helpers", () => {
  assert.match(NO_SYSTEM_ONLY_REDUCED_MOTION.message, /@multica\/core\/appearance/);
});

// The real package configs: views, web and desktop follow the Appearance
// switch; core implements it and the marketing site never mounts it.
const QUERY_SOURCE = 'export const reduced = window.matchMedia("(prefers-reduced-motion: reduce)").matches;\n';
const IMPORT_SOURCE = 'import { useReducedMotion } from "motion/react";\nexport { useReducedMotion };\n';

const workspace = (path) => fileURLToPath(new URL(`../../${path}`, import.meta.url));
const configs = new Map();
function eslintFor(cwd) {
  if (!configs.has(cwd)) configs.set(cwd, new ESLint({ cwd: workspace(cwd) }));
  return configs.get(cwd);
}

async function hits(cwd, filePath, source, ruleId) {
  const [result] = await eslintFor(cwd).lintText(source, { filePath: `${workspace(cwd)}/${filePath}` });
  return result.messages.filter((m) => m.ruleId === ruleId).length;
}

const COVERED = [
  ["packages/views", "issues/components/motion.ts"],
  ["packages/views", "dev-toolbar/motion.tsx"],
  ["apps/web", "components/motion.tsx"],
  ["apps/web", "app/[workspaceSlug]/motion.tsx"],
  ["apps/desktop", "src/renderer/src/components/motion.tsx"],
  ["apps/desktop", "src/renderer/src/components/desktop-dev-toolbar.tsx"],
];

for (const [cwd, file] of COVERED) {
  test(`${cwd}/${file} rejects a bare reduced-motion query`, async () => {
    assert.equal(await hits(cwd, file, QUERY_SOURCE, "no-restricted-syntax"), 1);
  });

  test(`${cwd}/${file} rejects motion's useReducedMotion`, async () => {
    assert.equal(await hits(cwd, file, IMPORT_SOURCE, "no-restricted-imports"), 1);
  });
}

const EXEMPT = [
  ["packages/core", "appearance/motion.ts"],
  ["apps/web", "features/landing/site/motion.tsx"],
  ["apps/web", "app/(landing)/motion.tsx"],
  ["packages/views", "issues/components/motion.test.ts"],
];

for (const [cwd, file] of EXEMPT) {
  test(`${cwd}/${file} may read the system query`, async () => {
    assert.equal(await hits(cwd, file, QUERY_SOURCE, "no-restricted-syntax"), 0);
  });
}

// https://docs.expo.dev/guides/using-eslint/
const { defineConfig } = require('eslint/config');
const expoConfig = require("eslint-config-expo/flat");
const coreExports = require("../../packages/core/package.json").exports;

// Mobile shares only types and pure helpers with core. These barrels re-export
// nothing else; every other core barrel also pulls in hooks, Query factories,
// stores or React context, which must not load under React Native.
const PURE_CORE_BARRELS = new Set(["./types", "./markdown"]);
const coreBarrelImports = Object.entries(coreExports)
  .filter(([subpath, target]) => target.endsWith("/index.ts") && !PURE_CORE_BARRELS.has(subpath))
  .map(([subpath]) => ({
    name: subpath === "." ? "@multica/core" : `@multica/core/${subpath.slice(2)}`,
    allowTypeImports: true,
    message: "This core barrel also loads hooks, queries, stores or React context. Import the pure module through its own subpath (for example @multica/core/agents/derive-presence); type-only imports are fine.",
  }));

module.exports = defineConfig([
  expoConfig,
  {
    ignores: ["dist/*"],
  },
  {
    files: ["**/*.{js,jsx,ts,tsx}"],
    rules: {
      "no-restricted-imports": ["error", {
        patterns: [{
          group: ["@react-navigation/*"],
          message: "Import navigation hooks and themes from expo-router to use its navigation context.",
        }],
      }],
    },
  },
  {
    files: ["**/*.{ts,tsx}"],
    rules: {
      "@typescript-eslint/no-restricted-imports": ["error", { paths: coreBarrelImports }],
    },
  },
]);

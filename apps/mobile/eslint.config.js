// https://docs.expo.dev/guides/using-eslint/
const { defineConfig } = require('eslint/config');
const expoConfig = require("eslint-config-expo/flat");
const coreExports = require("../../packages/core/package.json").exports;

const MOBILE_I18N_ATTRIBUTE_GUARD = {
  selector: [
    "JSXAttribute[name.name=/^(placeholder|title|aria-label|alt|accessibilityLabel)$/] > Literal[value=/[A-Za-z]/]:not([value=/^https?:/])",
    "JSXAttribute[name.name=/^(placeholder|title|aria-label|alt|accessibilityLabel)$/] > JSXExpressionContainer > Literal[value=/[A-Za-z]/]:not([value=/^https?:/])",
    "JSXAttribute[name.name=/^(placeholder|title|aria-label|alt|accessibilityLabel)$/] > JSXExpressionContainer > TemplateLiteral > TemplateElement[value.raw=/[A-Za-z]/]:not([value.raw=/^https?:/])",
  ].join(", "),
  message:
    "User-visible copy in placeholder/title/aria-label/alt/accessibilityLabel must come from useTranslation().",
};

const MOBILE_I18N_JSX_TEXT_GUARD = {
  selector:
    ":not(JSXElement[openingElement.name.name='code']) > JSXText[value=/[A-Za-z]{2,}/]",
  message:
    "User-visible JSX text must come from useT() / useTranslation() rather than a hardcoded string literal.",
};

const MOBILE_I18N_TOAST_GUARD = {
  selector: [
    ":matches(CallExpression[callee.name='toast'], CallExpression[callee.object.name='toast']) > Literal[value=/[A-Za-z]/]:not([value=/^https?:/])",
    ":matches(CallExpression[callee.name='toast'], CallExpression[callee.object.name='toast']) > TemplateLiteral > TemplateElement[value.raw=/[A-Za-z]/]:not([value.raw=/^https?:/])",
  ].join(", "),
  message: "Toast copy must come from useTranslation().",
};

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
  {
    files: ["app/**/*.{ts,tsx}", "components/**/*.{ts,tsx}"],
    ignores: ["**/*.test.{ts,tsx}"],
    rules: {
      "no-restricted-syntax": [
        "error",
        MOBILE_I18N_ATTRIBUTE_GUARD,
        MOBILE_I18N_JSX_TEXT_GUARD,
        MOBILE_I18N_TOAST_GUARD,
      ],
    },
  },
]);

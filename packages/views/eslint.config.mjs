import reactConfig from "@multica/eslint-config/react";
import { ICON_LIBRARY_IMPORT_PATTERNS, REDUCED_MOTION_IMPORT } from "@multica/eslint-config/base";
import { NO_ROUND_CONTROLS } from "@multica/eslint-config/control-shape";
import { NO_FULL_PAGE_NAVIGATION } from "@multica/eslint-config/full-page-navigation";
import i18next from "eslint-plugin-i18next";
import {
  NO_UNTRANSLATED_ATTRIBUTES,
  NO_UNTRANSLATED_TOAST,
} from "./eslint-i18n-guard.mjs";

// Global i18n protection. Every JSX text node in this package must pass
// through useT() — raw strings become a build error. Scope of
// `mode: "jsx-text-only"`: flags raw strings inside JSX children only;
// attribute values and plain TS literals are allowed through — the two doors
// `eslint-i18n-guard.mjs` pins shut.

// Package boundary (AGENTS.md): views consumes stores but never defines them.
// Tests may build throwaway stores as fixtures.
const STORE_FACTORY_MESSAGE =
  "packages/views must not define stores. Define the store in packages/core and import it here.";
const STORE_FACTORY_IMPORTS = ["zustand", "zustand/vanilla"].map((name) => ({
  name,
  importNames: ["create", "createStore"],
  message: STORE_FACTORY_MESSAGE,
}));

export default [
  ...reactConfig,
  {
    files: ["**/*.tsx"],
    ignores: ["**/*.test.tsx", "test/**"],
    plugins: { i18next },
    rules: {
      "i18next/no-literal-string": [
        "error",
        { mode: "jsx-text-only" },
      ],
    },
  },
  {
    // Toasts are fired from hooks (`.ts`) as often as from components.
    files: ["**/*.{ts,tsx}"],
    ignores: ["**/*.test.{ts,tsx}", "test/**"],
    rules: {
      "no-restricted-syntax": [
        "error",
        NO_UNTRANSLATED_ATTRIBUTES,
        NO_UNTRANSLATED_TOAST,
        NO_ROUND_CONTROLS,
        NO_FULL_PAGE_NAVIGATION,
      ],
      "no-restricted-imports": [
        "error",
        {
          paths: [...STORE_FACTORY_IMPORTS, REDUCED_MOTION_IMPORT],
          patterns: ICON_LIBRARY_IMPORT_PATTERNS,
        },
      ],
    },
  },
  {
    // The dev toolbar only exists in development builds and speaks to the
    // people building Multica, so its copy is not translated.
    files: ["dev-toolbar/**/*.{ts,tsx}"],
    rules: {
      "i18next/no-literal-string": "off",
      "no-restricted-syntax": ["error", NO_ROUND_CONTROLS, NO_FULL_PAGE_NAVIGATION],
    },
  },
];

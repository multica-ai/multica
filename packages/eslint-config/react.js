import baseConfig from "./base.js";
import reactPlugin from "eslint-plugin-react";
import reactHooksPlugin from "eslint-plugin-react-hooks";
import a11yPlugin from "./icon-button-name.js";

/** @type {import("eslint").Linter.Config[]} */
export default [
  ...baseConfig,
  // React rules (JSX only)
  {
    files: ["**/*.{jsx,tsx}"],
    plugins: { react: reactPlugin, "multica-a11y": a11yPlugin },
    rules: {
      ...reactPlugin.configs.recommended.rules,
      ...reactPlugin.configs["jsx-runtime"].rules,
      "react/prop-types": "off",
      "react/no-unknown-property": "off",
      "multica-a11y/icon-button-name": "error",
    },
    settings: {
      react: { version: "detect" },
    },
  },
  // React Hooks rules apply to .ts files too — hooks (useEffect, useCallback,
  // useMemo) can live in plain .ts modules and we want exhaustive-deps to
  // run + inline disable comments to resolve.
  {
    files: ["**/*.{ts,tsx,js,jsx}"],
    plugins: { "react-hooks": reactHooksPlugin },
    rules: {
      ...reactHooksPlugin.configs["recommended-latest"].rules,
    },
  },
];

import globals from "globals";
import reactConfig from "@multica/eslint-config/react";

export default [
  {
    ignores: ["plugins/**/*.cjs"],
  },
  ...reactConfig,
  {
    files: ["**/*.cjs"],
    languageOptions: {
      globals: { ...globals.node },
    },
    rules: {
      "@typescript-eslint/no-require-imports": "off",
    },
  },
];

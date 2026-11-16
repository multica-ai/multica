import reactConfig from "@multica/eslint-config/react";
import { NO_ROUND_CONTROLS } from "@multica/eslint-config/control-shape";

export default [
  ...reactConfig,
  {
    files: ["**/*.tsx"],
    ignores: ["**/*.test.tsx"],
    rules: {
      "no-restricted-syntax": ["error", NO_ROUND_CONTROLS],
    },
  },
  {
    files: ["icons/drawings/*.mjs"],
    languageOptions: {
      globals: { process: "readonly", console: "readonly", URL: "readonly" },
    },
  },
];

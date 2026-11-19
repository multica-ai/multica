import reactConfig from "@multica/eslint-config/react";
import { ICON_LIBRARY_IMPORT_PATTERNS, REDUCED_MOTION_IMPORT } from "@multica/eslint-config/base";
import { NO_ROUND_CONTROLS } from "@multica/eslint-config/control-shape";
import i18next from "eslint-plugin-i18next";

// Same guards as packages/views: every JSX text node goes through the
// package's translator, and icons come from @multica/ui.
export default [
  ...reactConfig,
  {
    files: ["**/*.tsx"],
    ignores: ["**/*.test.tsx", "test/**"],
    plugins: { i18next },
    rules: {
      "i18next/no-literal-string": ["error", { mode: "jsx-text-only" }],
    },
  },
  {
    files: ["**/*.{ts,tsx}"],
    ignores: ["**/*.test.{ts,tsx}", "test/**"],
    rules: {
      "no-restricted-syntax": ["error", NO_ROUND_CONTROLS],
      "no-restricted-imports": [
        "error",
        { paths: [REDUCED_MOTION_IMPORT], patterns: ICON_LIBRARY_IMPORT_PATTERNS },
      ],
    },
  },
];

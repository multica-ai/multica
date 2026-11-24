import nextConfig from "@multica/eslint-config/next";
import { NO_SYSTEM_ONLY_REDUCED_MOTION } from "@multica/eslint-config/reduced-motion";
import {
  NO_UNTRANSLATED_ATTRIBUTES,
  NO_UNTRANSLATED_JSX_TEXT,
  NO_UNTRANSLATED_TOAST,
} from "../../packages/views/eslint-i18n-guard.mjs";

const TESTS = ["**/*.test.{ts,tsx}", "**/test/**/*.{ts,tsx}"];

export default [
  ...nextConfig,
  { ignores: [".next/", ".source/"] },
  {
    // The marketing site never mounts the Appearance settings, so the system
    // setting is the only one it can follow.
    files: ["**/*.{ts,tsx}"],
    ignores: ["app/(landing)/**", "features/landing/**", ...TESTS],
    rules: {
      "no-restricted-syntax": ["error", NO_SYSTEM_ONLY_REDUCED_MOTION],
    },
  },
  {
    files: ["app/**/*.{ts,tsx}"],
    ignores: ["app/(landing)/**", ...TESTS],
    rules: {
      "no-restricted-syntax": [
        "error",
        NO_UNTRANSLATED_ATTRIBUTES,
        NO_UNTRANSLATED_JSX_TEXT,
        NO_UNTRANSLATED_TOAST,
        NO_SYSTEM_ONLY_REDUCED_MOTION,
      ],
    },
  },
  {
    files: TESTS,
    rules: {
      "react/display-name": "off",
    },
  },
];

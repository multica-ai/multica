import nextConfig from "@multica/eslint-config/next";
import {
  NO_UNTRANSLATED_ATTRIBUTES,
  NO_UNTRANSLATED_JSX_TEXT,
  NO_UNTRANSLATED_TOAST,
} from "../../packages/views/eslint-i18n-guard.mjs";

export default [
  ...nextConfig,
  { ignores: [".next/", ".source/"] },
  {
    files: ["app/**/*.{ts,tsx}"],
    ignores: [
      "app/(landing)/**",
      "**/*.test.{ts,tsx}",
      "**/test/**/*.{ts,tsx}",
    ],
    rules: {
      "no-restricted-syntax": [
        "error",
        NO_UNTRANSLATED_ATTRIBUTES,
        NO_UNTRANSLATED_JSX_TEXT,
        NO_UNTRANSLATED_TOAST,
      ],
    },
  },
  {
    files: ["**/*.test.{ts,tsx}", "**/test/**/*.{ts,tsx}"],
    rules: {
      "react/display-name": "off",
    },
  },
];

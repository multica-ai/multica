import globals from "globals";
import nextConfig from "@multica/eslint-config/next";

export default [
  ...nextConfig,
  { ignores: [".next/"] },
  {
    files: ["**/*.test.{ts,tsx}", "**/test/**/*.{ts,tsx}"],
    rules: {
      "react/display-name": "off",
    },
  },
  {
    // next.config.mjs is plain JS, not covered by typescript-eslint's
    // no-undef override for .ts/.tsx — declare Node globals explicitly so
    // `process` resolves (mirrors apps/desktop/eslint.config.mjs's
    // scripts/** override).
    files: ["next.config.mjs"],
    languageOptions: {
      globals: { ...globals.node },
    },
  },
];

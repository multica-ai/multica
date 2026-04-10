import eslint from "@eslint/js";
import tseslint from "typescript-eslint";

/** @type {import("eslint").Linter.Config[]} */
export default [
  eslint.configs.recommended,
  ...tseslint.configs.recommended,
  {
    rules: {
      // Already enforced by TypeScript compiler (noUnusedLocals/noUnusedParameters)
      "@typescript-eslint/no-unused-vars": "off",
      // Allow explicit any where needed
      "@typescript-eslint/no-explicit-any": "off",
    },
  },
  {
    ignores: [
      "node_modules/",
      "dist/",
      ".next/",
      "out/",
    ],
  },
];

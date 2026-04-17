import globals from "globals";
import reactConfig from "@multica/eslint-config/react";

export default [
  ...reactConfig,
  { ignores: ["out/", "dist/"] },
  {
    files: ["scripts/**/*.{mjs,js}"],
    languageOptions: {
      globals: { ...globals.node },
    },
  },
  // Security: every renderer-controlled URL that reaches the OS shell must
  // flow through openExternalSafely in src/main/external-url.ts (scheme
  // allowlist). Enforce it statically so a direct shell.openExternal call
  // cannot silently regress the protection.
  {
    files: ["src/main/**/*.ts"],
    rules: {
      "no-restricted-syntax": [
        "error",
        {
          selector:
            "CallExpression[callee.object.name='shell'][callee.property.name='openExternal']",
          message:
            "Do not call shell.openExternal directly. Use openExternalSafely from './external-url' so the http/https allowlist stays enforced.",
        },
      ],
    },
  },
  {
    files: ["src/main/external-url.ts"],
    rules: {
      "no-restricted-syntax": "off",
    },
  },
];

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
  // Security: every renderer-controlled URL that reaches the OS shell or the
  // native download system must flow through the safe wrappers in
  // src/main/external-url.ts (scheme allowlist). Enforce it statically so
  // direct shell.openExternal / webContents.downloadURL calls cannot silently
  // regress the protection.
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
        {
          selector:
            "CallExpression[callee.object.property.name='webContents'][callee.property.name='downloadURL']",
          message:
            "Do not call webContents.downloadURL directly. Use downloadURLSafely from './external-url' so the http/https allowlist stays enforced.",
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
  // Navigation boundary (MUL-4741): the tab Coordinator must be the only
  // navigation initiator — a Router location change without a Coordinator
  // token is a protocol error. Application code must not navigate directly;
  // it goes through the navigation adapter / Coordinator in src/platform.
  // Remaining legacy sites carry an inline eslint-disable tagged MUL-4741;
  // the Phase 2 migration removes them one by one, and this rule holding
  // with zero disables is the machine check that the migration is complete.
  {
    files: ["src/renderer/src/**/*.{ts,tsx}"],
    ignores: ["src/renderer/src/platform/**", "src/renderer/src/**/*.test.*"],
    rules: {
      "no-restricted-imports": [
        "error",
        {
          paths: [
            {
              name: "react-router-dom",
              importNames: ["useNavigate", "Navigate"],
              message:
                "Direct navigation from application code breaks the Coordinator protocol (MUL-4741). Use the navigation adapter from src/platform instead.",
            },
          ],
        },
      ],
      "no-restricted-syntax": [
        "error",
        {
          selector:
            "CallExpression[callee.object.name='router'][callee.property.name='navigate']",
          message:
            "Direct router.navigate from application code breaks the Coordinator protocol (MUL-4741). Route it through the navigation adapter in src/platform.",
        },
      ],
    },
  },
];

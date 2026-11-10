import reactConfig from "@multica/eslint-config/react";
import { ICON_LIBRARY_IMPORT_PATTERNS } from "@multica/eslint-config/base";

// Package boundary (AGENTS.md): core is headless and runs unchanged in web,
// desktop and SSR. No UI libraries, no react-dom, no next/*, no Web Storage
// globals and no process.env outside tests — tests may stub env and storage.

const UI_LIBRARIES = [
  "react-dom",
  "@multica/ui",
  "@multica/views",
  "@base-ui/react",
  "@radix-ui",
  "sonner",
  "cmdk",
  "recharts",
  "next-themes",
];
const escape = (s) => s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
const UI_LIBRARY_IMPORT_PATTERN = {
  regex: `^(${UI_LIBRARIES.map(escape).join("|")})(/.*)?$`,
  message:
    "packages/core is headless: no react-dom or UI libraries. Render in packages/ui or packages/views and pass UI into core through props or the platform layer.",
};
const NEXT_IMPORT_PATTERN = {
  regex: "^next(/.*)?$",
  message:
    "packages/core is shared by web and desktop: keep next/* in apps/web and have the app layer pass the value in.",
};

const PROCESS_ENV = {
  object: "process",
  property: "env",
  message:
    "packages/core must not read process.env. Have the app layer pass the value in (CoreProvider props or a function argument).",
};

const WEB_STORAGE = [
  {
    name: "localStorage",
    message:
      "Persist through the StorageAdapter (platform/storage.ts or the adapter injected via CoreProvider), not localStorage directly.",
  },
  {
    name: "sessionStorage",
    message:
      "packages/core must not touch sessionStorage directly. Persist through the StorageAdapter, or have the app layer pass the value in.",
  },
];

// `allowedStorage` lifts the Web Storage ban for the files that legitimately
// own it; every other restriction stays on.
const boundaryRules = (allowedStorage = []) => {
  const storage = WEB_STORAGE.filter((s) => !allowedStorage.includes(s.name));
  return {
    "no-restricted-globals": ["error", ...storage],
    "no-restricted-properties": [
      "error",
      PROCESS_ENV,
      ...storage.flatMap(({ name, message }) =>
        ["window", "globalThis", "self"].map((object) => ({
          object,
          property: name,
          message,
        })),
      ),
    ],
  };
};

const TESTS = ["**/*.test.{ts,tsx}", "**/test/**", "**/vitest.config.*"];

export default [
  ...reactConfig,
  {
    files: ["**/*.{ts,tsx}"],
    ignores: TESTS,
    rules: {
      ...boundaryRules(),
      "no-restricted-imports": [
        "error",
        {
          patterns: [
            ...ICON_LIBRARY_IMPORT_PATTERNS,
            UI_LIBRARY_IMPORT_PATTERN,
            NEXT_IMPORT_PATTERN,
          ],
        },
      ],
    },
  },
  // The sanctioned `defaultStorage` adapter.
  {
    files: ["platform/storage.ts"],
    rules: boundaryRules(["localStorage"]),
  },
  // Exception dedupe and theme safe mode are scoped to the browser tab
  // session, which the StorageAdapter cannot express.
  {
    files: ["analytics/exception-dedupe.ts", "appearance/safe-mode.ts"],
    rules: boundaryRules(["sessionStorage"]),
  },
];

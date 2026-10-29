import eslint from "@eslint/js";
import tseslint from "typescript-eslint";
import importPlugin from "eslint-plugin-import-x";

// Multica draws its own icons: every glyph comes from @multica/ui/icons, and a
// missing one is drawn in packages/ui/icons/drawings. The pattern covers each
// library's root and its subpaths (`react-icons/fa`, `@heroicons/react/24/outline`).
// Exported because flat config replaces a rule's options wholesale: a package
// that sets its own `no-restricted-imports` must include these patterns again.
export const ICON_LIBRARIES = [
  "lucide-react",
  "react-icons",
  "@heroicons/react",
  "@tabler/icons-react",
  "@radix-ui/react-icons",
  "@phosphor-icons/react",
];
const escape = (s) => s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
export const ICON_LIBRARY_IMPORT_PATTERNS = [
  {
    regex: `^(${ICON_LIBRARIES.map(escape).join("|")})(/.*)?$`,
    message:
      "Import icons from @multica/ui/icons; draw a missing one in packages/ui/icons/drawings (see .agents/skills/multica-icons).",
  },
];

/** @type {import("eslint").Linter.Config[]} */
export default [
  eslint.configs.recommended,
  ...tseslint.configs.recommended,
  {
    plugins: {
      "import-x": importPlugin,
    },
    rules: {
      // Already enforced by TypeScript compiler (noUnusedLocals/noUnusedParameters)
      "@typescript-eslint/no-unused-vars": "off",
      // Allow explicit any where needed
      "@typescript-eslint/no-explicit-any": "off",
      // Prevent phantom dependencies — imports must be declared in package.json
      "import-x/no-extraneous-dependencies": ["error", {
        devDependencies: [
          "**/*.test.{ts,tsx}",
          "**/*.spec.{ts,tsx}",
          "**/test/**",
          "**/tests/**",
          "**/vitest.config.*",
          "**/vite.config.*",
          "**/electron.vite.config.*",
          "**/eslint.config.*",
          "**/scripts/**",
          "**/src/main/**",
          "**/src/preload/**",
        ],
        peerDependencies: true,
      }],
      "no-restricted-imports": ["error", {
        patterns: ICON_LIBRARY_IMPORT_PATTERNS,
      }],
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

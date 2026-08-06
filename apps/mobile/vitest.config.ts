import { defineConfig } from "vitest/config";

// Mobile vitest is intentionally minimal — Node environment only, scoped to
// pure-function tests in `lib/` and the zod response schemas in `data/`. We
// don't ship jsdom or RN test renderers here because the app runs on Hermes /
// native shims and any DOM-shaped runner would be a lie. Tests that need RN
// component rendering would need a separate jest+react-native-testing-library
// track; for now we keep this lane for helpers, serializers and schemas.
//
// `data/**` is included so the malformed-response tests the root CLAUDE.md
// requires for every new endpoint ("API Response Compatibility") have a home —
// schemas are pure data, so they run in the same Node lane.
//
// Co-located test files (foo.ts + foo.test.ts) match how the rest of the
// monorepo organises vitest suites.
export default defineConfig({
  // `@/` is the app's path alias (tsconfig `paths`, resolved by Metro at
  // runtime). Vitest has no Metro, so mirror it here — otherwise a `data/`
  // module that imports `@/lib/...` at runtime is unresolvable in this lane.
  resolve: {
    alias: {
      "@": new URL(".", import.meta.url).pathname,
    },
  },
  test: {
    environment: "node",
    globals: true,
    include: ["lib/**/*.test.ts", "data/**/*.test.ts"],
    passWithNoTests: true,
  },
});

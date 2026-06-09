import { defineConfig } from "vitest/config";

// Mobile vitest is intentionally minimal — Node environment only, scoped to
// pure-function tests in `lib/`. We don't ship jsdom or RN test renderers
// here because the app runs on Hermes / native shims and any DOM-shaped
// runner would be a lie. Tests that need RN component rendering would
// need a separate jest+react-native-testing-library track; for now we
// keep this lane for helpers and serializers only.
//
// Co-located test files (foo.ts + foo.test.ts) match how the rest of the
// monorepo organises vitest suites.
export default defineConfig({
  test: {
    environment: "node",
    globals: true,
    include: ["lib/**/*.test.ts"],
    passWithNoTests: true,
  },
});

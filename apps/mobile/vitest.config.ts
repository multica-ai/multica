import { defineConfig } from "vitest/config";
import path from "node:path";

// Mobile vitest is intentionally minimal — Node environment only, scoped to
// pure-function helpers in `lib/` and headless data-layer logic in `data/`
// (query-key + cache-patch functions that take a QueryClient but touch no
// DOM / RN native modules). We don't ship jsdom or RN test renderers here
// because the app runs on Hermes / native shims and any DOM-shaped runner
// would be a lie. Tests that need RN component rendering would need a
// separate jest+react-native-testing-library track; for now we keep this
// lane for helpers, serializers, and cache updaters only. Data-layer tests
// must mock `@/data/api` so the native fetch chain never loads.
//
// Co-located test files (foo.ts + foo.test.ts) match how the rest of the
// monorepo organises vitest suites.
export default defineConfig({
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "."),
    },
  },
  test: {
    environment: "node",
    globals: true,
    include: ["lib/**/*.test.ts", "data/**/*.test.ts"],
    passWithNoTests: true,
  },
});

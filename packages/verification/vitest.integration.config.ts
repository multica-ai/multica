import { defineConfig } from "vitest/config";

// Explicit manual entry: never included in the repository's default test graph.
export default defineConfig({
  test: {
    include: ["src/**/*.integration.ts"],
    testTimeout: 45000,
    hookTimeout: 120000,
    fileParallelism: false,
  },
});

import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  test: {
    environment: "jsdom",
    globals: true,
    pool: "threads",
    isolate: true,
    maxWorkers: 2,
    setupFiles: ["./test/setup.ts"],
    include: ["**/*.test.{ts,tsx}"],
    // CEREBRO-PATCH(test-stability): The views package has many jsdom-heavy route tests. Limiting workers keeps
    // full Turbo runs from starving Vitest workers under suite-wide load.
    testTimeout: 60000,
  },
});

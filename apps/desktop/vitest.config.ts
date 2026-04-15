import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    globals: true,
    include: ["src/**/*.test.ts", "scripts/**/*.test.mjs"],
    environment: "node",
    passWithNoTests: true,
  },
});

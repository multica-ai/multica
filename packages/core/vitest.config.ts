import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    globals: true,
    setupFiles: ["@multica/vitest-setup/web-storage"],
    include: ["**/*.test.{ts,tsx}"],
    passWithNoTests: true,
  },
});

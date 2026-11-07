import { defineConfig } from "vitest/config";
import path from "node:path";

// Node helpers and data-layer tests mock native/API boundaries. Composer
// lifecycle tests use React's test renderer with host/native substitutes:
// they exercise real hooks and cleanup, not native layout or keyboard behavior.
// The run cancel button and inbox use headless callback/prop tests with native
// modules mocked. App configuration tests cover prebuild options without RN.

export default defineConfig({
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "."),
    },
  },
  test: {
    environment: "node",
    globals: true,
    include: [
      "app.config.test.ts",
      "lib/**/*.test.ts",
      "data/**/*.test.ts",
      "components/**/*.test.tsx",
      "components/issue/run-cancel-button.test.ts",
      "components/inbox/inbox-screen.test.ts",
    ],
    passWithNoTests: true,
  },
});

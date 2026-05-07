import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./test/setup.ts"],
    include: ["**/*.test.{ts,tsx}"],
    // CI's GitHub-hosted runner is meaningfully slower than dev machines —
    // userEvent.type() with 40+ characters has tipped past the 5s default
    // (modals/create-issue). 15s gives headroom without hiding genuine hangs.
    testTimeout: 15000,
  },
});

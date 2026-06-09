import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./test/setup.ts"],
    include: ["**/*.test.{ts,tsx}"],
    // CI runners hit the 5s default on this package's synchronous renders;
    // 30s mirrors the heavier @multica/views jsdom test budget without
    // hiding genuine hangs.
    testTimeout: 30000,
  },
});

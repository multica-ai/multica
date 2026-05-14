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
    // 15s mirrors @multica/views (main commit 14e4e622) and gives headroom
    // without hiding genuine hangs.
    testTimeout: 15000,
  },
});

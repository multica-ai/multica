import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  test: {
    environment: "jsdom",
    globals: true,
    pool: "threads",
    isolate: true,
    setupFiles: ["./test/setup.ts"],
    include: ["**/*.test.{ts,tsx}"],
    testTimeout: 60000,
  },
});

import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import path from "path";

export default defineConfig({
  plugins: [react()],
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./test/setup.ts"],
    include: ["**/*.test.{ts,tsx}"],
    pool: "threads",
    isolate: true,
    maxWorkers: 1,
    testTimeout: 30000,
  },
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "."),
      "@core": path.resolve(__dirname, "core"),
    },
  },
});

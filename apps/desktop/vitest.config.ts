import { resolve } from "path";
import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react(), {
    name: "verification-disabled-in-default-tests",
    resolveId(id) { if (/^#verification-dev\/(main|preload|renderer)$/.test(id)) return `\0verification-test-${id.split("/")[1]}`; return null; },
    load(id) { if (id.startsWith("\0verification-test-")) return "export const verificationDev = undefined; export const verificationPreload = undefined; export const verificationRenderer = undefined;"; return null; },
  }],
  resolve: {
    alias: {
      "@": resolve(__dirname, "src/renderer/src"),
    },
  },
  test: {
    globals: true,
    include: ["src/**/*.test.{ts,tsx}", "scripts/**/*.test.mjs"],
    environment: "jsdom",
    setupFiles: ["./test/setup.ts"],
    passWithNoTests: true,
    // jsdom page tests run about ten times slower on shared CI runners than
    // locally; the 5s default fails them there before they settle.
    testTimeout: 20_000,
  },
});

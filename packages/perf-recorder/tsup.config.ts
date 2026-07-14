import { defineConfig } from "tsup";

// Two outputs, mirroring react-scan / react-grab packaging:
//  - ESM entries consumed by the in-repo dev-only host loaders
//    (`@multica/perf-recorder`, `/install`, `/auto`).
//  - A single self-executing IIFE global (`dist/auto.global.js`) that inlines
//    bippy, so the eventual standalone package can be referenced with a plain
//    <script src> exactly like react-grab's `dist/index.global.js`.
export default defineConfig([
  {
    entry: {
      index: "src/index.ts",
      install: "src/install.ts",
      auto: "src/auto.ts",
    },
    format: ["esm"],
    target: "es2022",
    dts: true,
    clean: true,
    treeshake: true,
    platform: "browser",
  },
  {
    entry: { auto: "src/auto.ts" },
    format: ["iife"],
    globalName: "MulticaPerfRecorder",
    outExtension: () => ({ js: ".global.js" }),
    target: "es2022",
    minify: true,
    treeshake: true,
    platform: "browser",
    // Inline all deps (bippy) so the global file is self-contained.
    noExternal: [/.*/],
  },
]);

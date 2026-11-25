import { resolve } from "path";
import { defineConfig, externalizeDepsPlugin } from "electron-vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { runtimeConfigFromServerUrl } from "./src/shared/runtime-config";

// Self-hosted builds (see src/main/build-config.ts): the server a packaged app
// connects to when ~/.multica/desktop.json is absent, and an optional
// electron-updater generic feed. Validated here so a typo fails the build
// instead of every launch. Unset keeps the Multica Cloud defaults.
const buildServerUrl = process.env.MULTICA_DESKTOP_SERVER_URL?.trim()
  ? runtimeConfigFromServerUrl(process.env.MULTICA_DESKTOP_SERVER_URL).apiUrl
  : "";
const buildUpdateUrl = normalizeUpdateUrl(process.env.MULTICA_DESKTOP_UPDATE_URL);

function normalizeUpdateUrl(value: string | undefined): string {
  if (!value?.trim()) return "";
  let url: URL;
  try {
    url = new URL(value.trim());
  } catch {
    throw new Error("MULTICA_DESKTOP_UPDATE_URL must be a valid URL");
  }
  if (url.protocol !== "http:" && url.protocol !== "https:") {
    throw new Error("MULTICA_DESKTOP_UPDATE_URL must use http or https");
  }
  return url.toString();
}

export default defineConfig({
  main: {
    plugins: [externalizeDepsPlugin()],
    define: {
      __MULTICA_DESKTOP_SERVER_URL__: JSON.stringify(buildServerUrl),
      __MULTICA_DESKTOP_UPDATE_URL__: JSON.stringify(buildUpdateUrl),
    },
  },
  preload: {
    // `@electron-toolkit/preload` must be bundled INTO the preload script:
    // the renderer windows run with `sandbox: true`, and a sandboxed preload's
    // `require` can only load `electron` plus a couple of node builtins — an
    // externalized `require("@electron-toolkit/preload")` would throw and
    // every contextBridge API would vanish. electron-vite emits preload as a
    // single CJS bundle, which is exactly what the sandbox requires.
    plugins: [externalizeDepsPlugin({ exclude: ["@electron-toolkit/preload"] })],
  },
  renderer: {
    server: {
      // Allow parallel worktrees to run `pnpm dev:desktop` side-by-side
      // (e.g. Multica Canary alongside a primary checkout) by overriding
      // the renderer port via env. Falls back to 5173 for the common case.
      port: Number(process.env.DESKTOP_RENDERER_PORT) || 5173,
      strictPort: true,
    },
    plugins: [react(), tailwindcss()],
    resolve: {
      alias: {
        "@": resolve("src/renderer/src"),
      },
      dedupe: ["react", "react-dom", "@tanstack/react-query"],
    },
  },
});

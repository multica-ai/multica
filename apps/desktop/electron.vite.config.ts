import { resolve } from "path";
import { defineConfig } from "electron-vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  main: {
    // Bundle workspace packages into the main-process output. Externalizing
    // them leaves raw `.ts` sources in node_modules, which Node 22 (Electron 39)
    // refuses to type-strip at runtime (ERR_UNSUPPORTED_NODE_MODULES_TYPE_STRIPPING).
    build: {
      externalizeDeps: {
        exclude: ["@multica/core"],
      },
    },
  },
  preload: {
    build: {
      externalizeDeps: true,
    },
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

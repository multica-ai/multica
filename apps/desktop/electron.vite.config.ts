import { resolve } from "path";
import { defineConfig, externalizeDepsPlugin } from "electron-vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  main: {
    plugins: [externalizeDepsPlugin()],
  },
  preload: {
    plugins: [externalizeDepsPlugin()],
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
      // `@tanstack/react-query` MUST be deduped alongside react/react-dom.
      // It's a dependency of packages/core (which provides QueryProvider)
      // and packages/views, but NOT of apps/desktop — and App.tsx imports
      // useQuery/useQueryClient directly. Without dedupe, Rollup resolves
      // those imports via two different node_modules paths and bundles two
      // copies, producing two distinct QueryClientContext objects: the
      // provider writes to one, useQueryClient reads the other → "No
      // QueryClient set" → white screen. dev hides this because esbuild's
      // optimizeDeps pre-bundles to a single instance; only the production
      // rollup build splits. See the 0.2.47 blank-window regression.
      dedupe: ["react", "react-dom", "@tanstack/react-query"],
    },
  },
});

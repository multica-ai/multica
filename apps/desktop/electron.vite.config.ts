import { resolve } from "path";
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { builtinModules } from "node:module";
import { defineConfig, externalizeDepsPlugin } from "electron-vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

// Custom chunking replaces the externals electron-vite would otherwise add, so list them here:
// Electron, Node built-ins and runtime dependencies, except workspace packages that ship raw TypeScript.
const bundledDeps = new Set(["@multica/tutti-agent-gui-host", "@multica/core", "@tutti-os/browser-node", "@multica/computer-use"]);
const runtimeDeps = Object.keys(JSON.parse(readFileSync(resolve("package.json"), "utf8")).dependencies ?? {}).filter((dep) => !bundledDeps.has(dep));
const mainExternal = (id: string) => id === "electron" || id.startsWith("electron/") || id.startsWith("node:") || builtinModules.includes(id)
  || runtimeDeps.some((dep) => id === dep || id.startsWith(`${dep}/`));

export default defineConfig({
  main: {
    // Workspace packages expose raw TypeScript; main must bundle their runtime imports.
    build: {
      externalizeDeps: false,
      lib: {
        entry: {
          index: resolve("src/main/index.ts"),
          // Standalone stdio MCP that Codex runs with ELECTRON_RUN_AS_NODE=1.
          "developer-logs-mcp": resolve("src/main/developer-logs-mcp.ts"),
          // One-conversation Claude Agent SDK adapter the daemon runs with ELECTRON_RUN_AS_NODE=1.
          "claude-host": resolve("src/claude-host/entry.ts"),
          "computer-host": resolve("src/computer-host/entry.ts"),
        },
      },
      rolldownOptions: {
        external: mainExternal,
        output: {
          entryFileNames: (chunk) => chunk.name === "computer-host" ? "computer-host.cjs" : "[name].js",
          // Keep everything the entries share out of index.js: the standalone
          // MCP and the Claude adapter must never load (and so boot) the desktop app.
          advancedChunks: { groups: [
            { name: "developer-logs", test: /src\/main\/(developer-log-export|developer-runtime-logs|developer-logs-mcp-server|local-timing-log)\.ts$|tutti-agent-gui-host\/src\/local-timing\.ts$/ },
            { name: "shared", test: /src\/shared\/|packages\/core\// },
            { name: "claude-host", test: (id: string) => /src\/claude-host\//.test(id) && !/src\/claude-host\/entry\.ts$/.test(id) },
          ] },
        },
      },
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
    plugins: [
      {
        name: "multica-tutti-agent-gui-assets",
        config() {
          execFileSync(process.execPath, [
            resolve("../../packages/tutti-agent-gui-host/scripts/build.mjs"),
          ], { stdio: "inherit" });
        },
      },
      react(),
      tailwindcss(),
    ],
    resolve: {
      alias: {
        "@": resolve("src/renderer/src"),
      },
      dedupe: ["react", "react-dom", "@tanstack/react-query"],
    },
  },
});

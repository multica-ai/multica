import { defineConfig } from "tsup";

// Bundle everything into a single executable JS file. The shebang is
// added so the published binary works under `npx` / direct exec, and
// node20+ ESM is the only target — MCP clients that spawn this server
// always have Node available.
//
// `noExternal` is critical: tsup's default behavior is to externalize
// every package in `dependencies`, which produces a bundle full of
// `import "@modelcontextprotocol/sdk/..."` lines that fail at runtime
// when the binary is run outside the worktree's node_modules. We ship
// the binary as a downloadable static asset (apps/web/public) and
// users land it at ~/.local/bin/multica-mcp — there's no node_modules
// next to it, so every npm dependency MUST be inlined.
//
// We list the deps explicitly rather than `noExternal: [/.*/]` so the
// failure mode for adding a new dep is "cannot resolve at runtime"
// (loud and immediate) rather than "silently bundles whatever node
// built-ins exist" (subtle).
export default defineConfig({
  entry: ["src/index.ts"],
  format: ["esm"],
  outDir: "dist",
  target: "node20",
  platform: "node",
  bundle: true,
  splitting: false,
  clean: true,
  sourcemap: true,
  dts: false,
  banner: { js: "#!/usr/bin/env node" },
  // Node built-ins must NEVER be inlined.
  external: [/^node:/],
  // Force-bundle every npm dep so the output JS runs standalone.
  noExternal: ["@modelcontextprotocol/sdk", "zod"],
});

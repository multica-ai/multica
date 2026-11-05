// Builds the page e2e specs drive with fake host data. It is a test rig: neither
// app ships or loads it. Output goes to apps/web/public/tutti-agent-gui, which is
// not committed.
import { execFileSync } from "node:child_process";
import { resolve } from "node:path";
import { rm } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { build } from "vite";

const prepare = fileURLToPath(new URL("./prepare.mjs", import.meta.url));
const outDir = (process.argv[2] ? resolve(process.argv[2]) : undefined) ?? fileURLToPath(new URL("../../../apps/web/public/tutti-agent-gui", import.meta.url));
execFileSync(process.execPath, [prepare], { stdio: "inherit" });
await rm(outDir, { recursive: true, force: true });
await build({
  configFile: fileURLToPath(new URL("../vite.config.ts", import.meta.url)),
  root: fileURLToPath(new URL("../e2e-harness", import.meta.url)),
  base: "./",
  build: {
    outDir, emptyOutDir: true, sourcemap: false, minify: false,
    rollupOptions: { input: {
      index: fileURLToPath(new URL("../e2e-harness/index.html", import.meta.url)),
      performance: fileURLToPath(new URL("../e2e-harness/performance.html", import.meta.url)),
      direct: fileURLToPath(new URL("../e2e-harness/direct.html", import.meta.url)),
    } },
  },
});

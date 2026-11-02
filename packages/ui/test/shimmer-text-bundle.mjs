import { buildSync } from "esbuild";
import { fileURLToPath, URL } from "node:url";

// Playwright's JSX transform makes component descriptors, not React elements.
// Bundle the fixture with the same React JSX runtime as the consuming apps.
export const shimmerTextScript = buildSync({
  entryPoints: [fileURLToPath(new URL("./shimmer-text-fixture.tsx", import.meta.url))],
  bundle: true,
  write: false,
  format: "iife",
  globalName: "shimmerFixture",
  define: { "process.env.NODE_ENV": '"production"' },
}).outputFiles[0].text;

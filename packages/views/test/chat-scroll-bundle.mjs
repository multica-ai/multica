import { buildSync } from "esbuild";
import { fileURLToPath, URL } from "node:url";

// Exercise the production hook with real Virtuoso measurements and retries.
export const chatScrollScript = buildSync({
  entryPoints: [fileURLToPath(new URL("./chat-scroll-fixture.tsx", import.meta.url))],
  bundle: true,
  write: false,
  format: "iife",
  globalName: "chatScrollFixture",
  define: { "process.env.NODE_ENV": '"production"' },
}).outputFiles[0].text;

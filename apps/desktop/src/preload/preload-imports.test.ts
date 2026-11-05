// @vitest-environment node
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { expect, it } from "vitest";

// The sandboxed preload can require only electron; electron-vite externalizes
// every other package except the ones bundled in electron.vite.config.ts.
// The verification virtual module is bundled in dev and resolved to undefined in builds.
const bundled = new Set(["electron", "@electron-toolkit/preload", "#verification-dev/preload"]);

function runtimeImports(file: string, seen = new Set<string>()): string[] {
  if (seen.has(file)) return [];
  seen.add(file);
  const source = readFileSync(file, "utf8");
  const found: string[] = [];
  for (const match of source.matchAll(/^\s*(?:import|export)\s+(?!type\b)(?:[^"';]*?\s+from\s+)?["']([^"']+)["']/gm)) {
    const specifier = match[1]!;
    if (specifier.startsWith(".")) found.push(...runtimeImports(resolve(dirname(file), `${specifier.replace(/\.ts$/, "")}.ts`), seen));
    else found.push(specifier);
  }
  return found;
}

it("imports only packages the sandboxed preload can load", () => {
  const imports = runtimeImports(resolve(__dirname, "index.ts"));
  expect(imports.filter((specifier) => !bundled.has(specifier) && !specifier.startsWith("node:"))).toEqual([]);
});

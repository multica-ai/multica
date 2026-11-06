// @vitest-environment node
import { cp, mkdir, mkdtemp, realpath, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { createServer, type ViteDevServer } from "vite";
import { expect, test } from "vitest";

test("transforms vendored i18n without a hoisted tsconfig package", async () => {
  // A full workspace install can hide undeclared config dependencies through hoisting.
  const root = await realpath(await mkdtemp(join(tmpdir(), "multica-vendored-i18n-")));
  const packages = fileURLToPath(new URL("../../../vendor/tutti/packages/", import.meta.url));
  let server: ViteDevServer | undefined;
  try {
    await mkdir(join(root, "ui"), { recursive: true });
    await mkdir(join(root, "configs"), { recursive: true });
    await cp(join(packages, "ui/i18n-runtime"), join(root, "ui/i18n-runtime"), {
      recursive: true,
      filter: (path) => !path.split(/[\\/]/).includes("node_modules"),
    });
    await cp(join(packages, "configs/tsconfig"), join(root, "configs/tsconfig"), {
      recursive: true,
      filter: (path) => !path.split(/[\\/]/).includes("node_modules"),
    });
    server = await createServer({
      configFile: false,
      root,
      server: { middlewareMode: true, watch: null, fs: { allow: [root] } },
      optimizeDeps: { noDiscovery: true, include: [] },
    });
    const result = await server.transformRequest("/ui/i18n-runtime/src/index.ts");
    expect(result?.code).toContain("export function createI18nRuntime");
  } finally {
    await server?.close();
    await rm(root, { recursive: true, force: true });
  }
});

// @vitest-environment node
import { cp, mkdir, mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { expect, it } from "vitest";
import { transformWithOxc } from "vite";

it("compiles vendored i18n sources without a hoisted config package", async () => {
  const fixture = await mkdtemp(join(tmpdir(), "multica-vendored-tsconfig-"));
  const packages = fileURLToPath(new URL("../../../vendor/tutti/packages/", import.meta.url));
  try {
    // Exclude node_modules so the fixture has only the committed config closure.
    for (const name of ["ui/i18n-runtime", "configs/tsconfig"]) {
      const destination = join(fixture, "packages", name);
      await mkdir(destination, { recursive: true });
      await cp(join(packages, name), destination, {
        recursive: true,
        filter: (path) => !path.split(/[\\/]/).includes("node_modules"),
      });
    }
    const filename = join(fixture, "packages/ui/i18n-runtime/src/index.ts");
    const result = await transformWithOxc(await readFile(filename, "utf8"), filename);
    expect(result.code).toContain("export function createLocaleObjectI18nModuleManifest");
  } finally {
    await rm(fixture, { recursive: true, force: true });
  }
});

import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const repoRoot = resolve(__dirname, "../..");

function readJson(path: string) {
  return JSON.parse(readFileSync(resolve(repoRoot, path), "utf8")) as {
    scripts?: Record<string, string>;
  };
}

describe("workspace package build boundary", () => {
  it("builds shared packages before the Next production build", () => {
    const webPackage = readJson("apps/web/package.json");
    const prebuild = webPackage.scripts?.prebuild ?? "";

    expect(prebuild).toContain("@multica/core");
    expect(prebuild).toContain("@multica/ui");
    expect(prebuild).toContain("@multica/views");
  });

  it("emits shared workspace packages to dist for production bundling", () => {
    for (const packageName of ["core", "ui", "views"]) {
      const packageJson = readJson(`packages/${packageName}/package.json`);
      const buildTsconfig = readJson(`packages/${packageName}/tsconfig.build.json`);

      expect(packageJson.scripts?.build).toBe("tsc -p tsconfig.build.json");
      expect(buildTsconfig).toMatchObject({
        compilerOptions: {
          outDir: "dist",
          sourceMap: false,
          declarationMap: false,
        },
      });
    }
  });

  it("uses dist aliases for shared packages during production Next builds", () => {
    const nextConfigSource = readFileSync(
      resolve(repoRoot, "apps/web/next.config.ts"),
      "utf8",
    );

    expect(nextConfigSource).toContain("workspaceDistAliases");
    expect(nextConfigSource).toContain("isProductionBuild");
    expect(nextConfigSource).toContain("packages/core/dist");
    expect(nextConfigSource).toContain("packages/ui/dist");
    expect(nextConfigSource).toContain("packages/views/dist");
    expect(nextConfigSource).toContain("isProductionBuild\n    ? {}\n    : { transpilePackages");
    expect(nextConfigSource).toContain("turbopack");
    expect(nextConfigSource).toContain("workspaceTurbopackDistAliases");
    expect(nextConfigSource).toContain("resolveAlias: isProductionBuild ? workspaceTurbopackDistAliases : {}");
  });
});

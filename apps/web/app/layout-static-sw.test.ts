// @vitest-environment node
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

const dir = dirname(fileURLToPath(import.meta.url));

describe("root layout registers the static-asset service worker", () => {
  it("mounts StaticAssetSW so production return visits reuse hashed chunks", () => {
    const src = readFileSync(join(dir, "layout.tsx"), "utf8");

    expect(src).toMatch(/from "@\/components\/static-asset-sw"/);
    expect(src).toMatch(/<StaticAssetSW \/>/);
  });
});

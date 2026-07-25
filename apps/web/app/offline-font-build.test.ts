import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const repoRoot = resolve(process.cwd(), "../..");

describe("offline web build fonts", () => {
  it("does not fetch Google fonts from the root layouts", () => {
    const layouts = [
      "apps/web/app/layout.tsx",
      "apps/web/app/(landing)/layout.tsx",
    ].map((path) => readFileSync(resolve(repoRoot, path), "utf8"));

    for (const source of layouts) {
      expect(source).not.toContain("next/font/google");
    }
  });
});

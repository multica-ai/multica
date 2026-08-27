// @vitest-environment node
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

const dir = dirname(fileURLToPath(import.meta.url));

function read(relative: string): string {
  return readFileSync(join(dir, relative), "utf8");
}

describe("landing client graph stays off fat views barrels", () => {
  it("imports status/priority glyphs from leaf modules", () => {
    const src = read("components/features-section.tsx");
    expect(src).not.toMatch(/from "@multica\/views\/issues\/components"/);
    expect(src).toMatch(/issues\/components\/status-icon"/);
    expect(src).toMatch(/issues\/components\/priority-icon"/);
  });

  it("imports the waitlist form without the onboarding barrel", () => {
    const src = read("components/download/cloud-section.tsx");
    expect(src).not.toMatch(/from "@multica\/views\/onboarding"/);
    expect(src).toMatch(/onboarding\/cloud-waitlist-expand"/);
  });

  it("does not statically import every landing dictionary into the client provider", () => {
    const src = read("i18n/context.tsx");
    expect(src).not.toMatch(/from "\.\/en"/);
    expect(src).not.toMatch(/from "\.\/ja"/);
    expect(src).not.toMatch(/from "\.\/zh"/);
    expect(src).not.toMatch(/from "\.\/ko"/);
    expect(src).toMatch(/from "\.\/load-dict"/);
  });
});

// @vitest-environment node
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

import { generatedBlock, patchLandingFile, patchTokenFile } from "./generate";

const here = dirname(fileURLToPath(import.meta.url));

describe("generated CSS stays in lockstep", () => {
  it("tokens.css matches derive()", () => {
    const source = readFileSync(resolve(here, "../styles/tokens.css"), "utf8");
    expect(source).toContain(generatedBlock("light"));
    expect(source).toContain(generatedBlock("dark"));
    expect(patchTokenFile(source)).toBe(source);
  });

  it("landing-light matches the light skin", () => {
    const source = readFileSync(
      resolve(here, "../../../apps/web/app/custom.css"),
      "utf8",
    );
    expect(patchLandingFile(source)).toBe(source);
  });
});

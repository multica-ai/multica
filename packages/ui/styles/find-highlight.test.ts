// @vitest-environment node
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { FIND_HIGHLIGHT_CSS } from "./find-highlight";

const stylesDir = dirname(fileURLToPath(import.meta.url));

describe("FIND_HIGHLIGHT_CSS", () => {
  it("keeps both Custom Highlight API names out of LightningCSS input", () => {
    expect(FIND_HIGHLIGHT_CSS).toContain("::highlight(multica-find)");
    expect(FIND_HIGHLIGHT_CSS).toContain("::highlight(multica-find-active)");
    const baseCss = readFileSync(join(stylesDir, "base.css"), "utf8");
    expect(baseCss).not.toContain("::highlight(");
  });
});

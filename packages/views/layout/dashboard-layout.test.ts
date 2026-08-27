// @vitest-environment node
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

const dir = dirname(fileURLToPath(import.meta.url));

describe("compact web does not statically import the session tab strip", () => {
  it("loads TabBar through import() so phones never download dnd-kit tabs", () => {
    const src = readFileSync(join(dir, "dashboard-layout.tsx"), "utf8");

    expect(src).not.toMatch(/import \{ TabBar \} from "\.\/tab-bar"/);
    expect(src).toMatch(/import\("\.\/tab-bar"\)/);
  });
});

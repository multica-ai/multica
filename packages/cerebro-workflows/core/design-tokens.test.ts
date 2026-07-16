import { readdirSync, readFileSync } from "node:fs";
import { dirname, extname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

const packageRoot = join(dirname(fileURLToPath(import.meta.url)), "..");
const forbiddenColor = /#[0-9a-fA-F]{3,8}\b|(?:bg|text|border|ring)-(?:blue|emerald|green|red|purple|indigo|slate|gray|zinc|orange|amber|yellow|cyan|sky|teal|lime|rose|pink|violet|fuchsia)-/;

function sourceFiles(directory: string): string[] {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const path = join(directory, entry.name);
    if (entry.isDirectory()) return sourceFiles(path);
    return [".ts", ".tsx"].includes(extname(path)) && !entry.name.includes(".test.") ? [path] : [];
  });
}

describe("Cerebro workflows design tokens", () => {
  it("does not use hardcoded palette colors", () => {
    const offenders = sourceFiles(packageRoot)
      .filter((path) => forbiddenColor.test(readFileSync(path, "utf8")))
      .map((path) => path.slice(packageRoot.length + 1));

    expect(offenders).toEqual([]);
  });
});

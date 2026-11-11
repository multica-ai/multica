import { readFileSync, readdirSync, statSync } from "node:fs";
import { join, relative } from "node:path";
import { describe, expect, it } from "vitest";
import { THEME_ANCHORS, THEME_PUBLIC_SLOTS } from "@multica/core/appearance";

/**
 * The theme anchors are a public contract (docs/theming.md). This keeps the
 * registry, the source and the docs in step: every listed anchor is still
 * rendered, nothing renders an unlisted one, and the docs mention each
 * anchor and slot.
 */

const REPO = join(__dirname, "../../..");
const SOURCE_ROOTS = ["packages/views", "packages/ui/components", "apps/desktop/src/renderer", "apps/web/app", "apps/web/components"];

function sourceFiles(dir: string, out: string[] = []): string[] {
  let entries: string[];
  try {
    entries = readdirSync(dir);
  } catch {
    return out;
  }
  for (const entry of entries) {
    if (entry === "node_modules" || entry.startsWith(".")) continue;
    const path = join(dir, entry);
    if (statSync(path).isDirectory()) sourceFiles(path, out);
    else if (/\.tsx?$/.test(entry) && !/\.test\.tsx?$/.test(entry)) out.push(path);
  }
  return out;
}

const files = SOURCE_ROOTS.flatMap((root) => sourceFiles(join(REPO, root)));
const used = new Map<string, string[]>();
for (const file of files) {
  const text = readFileSync(file, "utf8");
  for (const match of text.matchAll(/(?:data-mc=|"data-mc":\s*)"([a-z-]+)"/g)) {
    used.set(match[1]!, [...(used.get(match[1]!) ?? []), relative(REPO, file)]);
  }
}
const docs = readFileSync(join(REPO, "docs/theming.md"), "utf8");

describe("theme anchors", () => {
  it("renders every registered anchor somewhere", () => {
    const missing = Object.keys(THEME_ANCHORS).filter((name) => !used.has(name));
    expect(missing).toEqual([]);
  });

  it("only renders registered anchors", () => {
    const unknown = [...used.keys()].filter((name) => !(name in THEME_ANCHORS));
    expect(unknown).toEqual([]);
  });

  it("keeps every public slot on a UI primitive", () => {
    const ui = sourceFiles(join(REPO, "packages/ui/components")).map((f) => readFileSync(f, "utf8")).join("\n");
    const missing = THEME_PUBLIC_SLOTS.filter((slot) => !ui.includes(`data-slot="${slot}"`));
    expect(missing).toEqual([]);
  });

  it("documents every anchor and slot", () => {
    const names = [
      ...Object.keys(THEME_ANCHORS).map((name) => `data-mc="${name}"`),
      ...THEME_PUBLIC_SLOTS.map((slot) => `\`${slot}\``),
    ];
    expect(names.filter((name) => !docs.includes(name))).toEqual([]);
  });
});

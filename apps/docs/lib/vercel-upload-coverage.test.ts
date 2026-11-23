// @vitest-environment node
import { execFileSync } from "node:child_process";
import { existsSync, readdirSync, readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";

// Vercel uploads the repository filtered by the root .vercelignore, which
// drops most root files and other workspaces. A docs page that embeds a file
// with `<include>` builds locally but fails every Vercel deployment when that
// file is filtered out, as SELF_HOSTING_AI.md was (MUL-9943). .vercelignore is
// listed in the "docs" group of .github/ci-paths.json so editing it runs this.

const repoRoot = path.resolve(__dirname, "../../..");
const docsDir = path.join(repoRoot, "apps/docs/content/docs");
const includePattern = /<include(?:\s[^>]*)?>([^<]+)<\/include>/g;

function mdxFiles(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) return mdxFiles(full);
    return /\.mdx?$/.test(entry.name) ? [full] : [];
  });
}

function includedFiles(): { page: string; target: string }[] {
  return mdxFiles(docsDir).flatMap((file) =>
    [...readFileSync(file, "utf8").matchAll(includePattern)].map((match) => ({
      page: path.relative(repoRoot, file),
      // remark-include resolves the path relative to the page; `#section` and
      // `?query` suffixes pick part of the file.
      target: path.relative(
        repoRoot,
        path.resolve(path.dirname(file), match[1]!.trim().replace(/[#?].*$/, "")),
      ),
    })),
  );
}

// Uses Git's ignore matching, which follows the same gitignore syntax Vercel
// documents for .vercelignore.
function excludedFromUpload(files: string[]): string[] {
  if (files.length === 0) return [];
  const output = execFileSync(
    "git",
    ["ls-files", "--cached", "--ignored", "--exclude-from=.vercelignore", "--", ...files],
    { cwd: repoRoot, encoding: "utf8" },
  );
  return output.split("\n").filter(Boolean);
}

describe("Vercel upload", () => {
  const includes = includedFiles();

  it("finds the pages that embed repository files", () => {
    expect(includes.map(({ target }) => target)).toContain("SELF_HOSTING_AI.md");
  });

  it("keeps every file a docs page embeds with <include>", () => {
    for (const { page, target } of includes) {
      expect(existsSync(path.join(repoRoot, target)), `${page} includes missing ${target}`).toBe(true);
    }
    const excluded = excludedFromUpload([...new Set(includes.map(({ target }) => target))]);
    expect(excluded, "allow these in .vercelignore").toEqual([]);
  });
});

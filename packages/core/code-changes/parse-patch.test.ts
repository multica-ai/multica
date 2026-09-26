// @vitest-environment node
import { describe, expect, it } from "vitest";
import { parsePatch, unquotePath } from "./parse-patch";

const GIT_PATCH = [
  "diff --git a/src/app.ts b/src/app.ts",
  "index 1111111..2222222 100644",
  "--- a/src/app.ts",
  "+++ b/src/app.ts",
  "@@ -1,4 +1,5 @@ export function app()",
  " import { a } from './a';",
  "-const x = 1;",
  "+const x = 2;",
  "+const y = 3;",
  " ",
  " export {};",
  "diff --git a/new.md b/new.md",
  "new file mode 100644",
  "index 0000000..3333333",
  "--- /dev/null",
  "+++ b/new.md",
  "@@ -0,0 +1,2 @@",
  "+# Title",
  "+--- not a header, an added line",
  "diff --git a/old name.txt b/new name.txt",
  "similarity index 90%",
  "rename from old name.txt",
  "rename to new name.txt",
  "--- a/old name.txt",
  "+++ b/new name.txt",
  "@@ -1 +1 @@",
  "-before",
  "\\ No newline at end of file",
  "+after",
  "diff --git a/gone.txt b/gone.txt",
  "deleted file mode 100644",
  "--- a/gone.txt",
  "+++ /dev/null",
  "@@ -1 +0,0 @@",
  "--- this removed line starts with three dashes",
  "diff --git a/logo.png b/logo.png",
  "index 4444444..5555555 100644",
  "Binary files a/logo.png and b/logo.png differ",
  "",
].join("\n");

describe("parsePatch", () => {
  const files = parsePatch(GIT_PATCH);

  it("reads every file with its status", () => {
    expect(files.map((f) => [f.path, f.status])).toEqual([
      ["src/app.ts", "modified"],
      ["new.md", "added"],
      ["new name.txt", "renamed"],
      ["gone.txt", "deleted"],
      ["logo.png", "modified"],
    ]);
    expect(files[2]!.oldPath).toBe("old name.txt");
    expect(files[4]!.binary).toBe(true);
    expect(files[4]!.hunks).toEqual([]);
  });

  it("numbers lines and counts changes", () => {
    const app = files[0]!;
    expect(app.additions).toBe(2);
    expect(app.deletions).toBe(1);
    const hunk = app.hunks[0]!;
    expect(hunk.section).toBe("export function app()");
    expect(hunk.lines.map((l) => [l.kind, l.oldNumber, l.newNumber])).toEqual([
      ["context", 1, 1],
      ["del", 2, null],
      ["add", null, 2],
      ["add", null, 3],
      ["context", 3, 4],
      ["context", 4, 5],
    ]);
  });

  it("reads hunks by their line counts, not by a line's leading dashes", () => {
    expect(files[1]!.hunks[0]!.lines.map((l) => l.text)).toEqual(["# Title", "--- not a header, an added line"]);
    expect(files[3]!.deletions).toBe(1);
    expect(files[3]!.hunks[0]!.lines[0]!.text).toBe("-- this removed line starts with three dashes");
  });

  it("marks a line that has no newline at the end", () => {
    const rename = files[2]!.hunks[0]!.lines;
    expect(rename[0]!.noNewlineAtEnd).toBe(true);
    expect(rename[1]!.noNewlineAtEnd).toBeUndefined();
  });

  it("reads a plain diff -u without git headers", () => {
    const plain = parsePatch([
      "--- a/one.txt\t2026-01-01 00:00:00",
      "+++ b/one.txt\t2026-01-02 00:00:00",
      "@@ -1 +1 @@",
      "-a",
      "+b",
      "--- two.txt",
      "+++ two.txt",
      "@@ -1,2 +1,2 @@",
      " keep",
      "-c",
      "+d",
    ].join("\n"));
    expect(plain.map((f) => [f.path, f.additions, f.deletions])).toEqual([
      ["one.txt", 1, 1],
      ["two.txt", 1, 1],
    ]);
  });

  it("skips a format-patch preamble and signature", () => {
    const mail = parsePatch([
      "From 1234 Mon Sep 17 00:00:00 2001",
      "Subject: [PATCH] fix",
      "---",
      " a.txt | 2 +-",
      "",
      "diff --git a/a.txt b/a.txt",
      "--- a/a.txt",
      "+++ b/a.txt",
      "@@ -1 +1 @@",
      "-x",
      "+y",
      "-- ",
      "2.45.0",
    ].join("\n"));
    expect(mail).toHaveLength(1);
    expect(mail[0]!.additions).toBe(1);
  });

  it("tolerates CRLF line endings", () => {
    const crlf = parsePatch("diff --git a/a b/a\r\n--- a/a\r\n+++ b/a\r\n@@ -1 +1 @@\r\n-x\r\n+y\r\n");
    expect(crlf[0]!.hunks[0]!.lines.map((l) => l.text)).toEqual(["x", "y"]);
  });

  it("returns nothing for text that is not a diff", () => {
    expect(parsePatch("hello\nworld\n")).toEqual([]);
    expect(parsePatch("")).toEqual([]);
  });

  it("reads quoted non-ASCII paths", () => {
    const quoted = parsePatch([
      'diff --git "a/\\346\\226\\207.md" "b/\\346\\226\\207.md"',
      "new file mode 100644",
      "--- /dev/null",
      '+++ "b/\\346\\226\\207.md"',
      "@@ -0,0 +1 @@",
      "+x",
    ].join("\n"));
    expect(quoted[0]!.path).toBe("文.md");
    expect(quoted[0]!.status).toBe("added");
  });
});

describe("unquotePath", () => {
  it("decodes C-style escapes", () => {
    expect(unquotePath('"a\\tb\\"c"')).toBe('a\tb"c');
    expect(unquotePath("plain")).toBe("plain");
  });
});

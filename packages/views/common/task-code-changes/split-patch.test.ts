// @vitest-environment node
import { describe, expect, it } from "vitest";
import { splitPatchByFile } from "./split-patch";

const PATCH = [
  "diff --git a/src/app.ts b/src/app.ts",
  "index 1111111..2222222 100644",
  "--- a/src/app.ts",
  "+++ b/src/app.ts",
  "@@ -1 +1 @@",
  "-old",
  "+new",
  "diff --git a/gone.txt b/gone.txt",
  "deleted file mode 100644",
  "--- a/gone.txt",
  "+++ /dev/null",
  "@@ -1 +0,0 @@",
  "-bye",
  "diff --git a/old-name.txt b/new-name.txt",
  "similarity index 100%",
  "rename from old-name.txt",
  "rename to new-name.txt",
  "diff --git a/logo.png b/logo.png",
  "new file mode 100644",
  "Binary files /dev/null and b/logo.png differ",
  "",
].join("\n");

describe("splitPatchByFile", () => {
  it("keys each file's section by its path", () => {
    const sections = splitPatchByFile(PATCH);
    expect([...sections.keys()]).toEqual(["src/app.ts", "gone.txt", "new-name.txt", "logo.png"]);
    expect(sections.get("src/app.ts")).toContain("+new");
    expect(sections.get("src/app.ts")).not.toContain("gone.txt");
    expect(sections.get("gone.txt")).toContain("-bye");
  });

  it("does not split on a changed line that mentions a diff header", () => {
    const patch = "diff --git a/notes.md b/notes.md\n--- a/notes.md\n+++ b/notes.md\n@@ -0,0 +1 @@\n+diff --git a/x b/x\n";
    expect([...splitPatchByFile(patch).keys()]).toEqual(["notes.md"]);
  });

  it("returns nothing for an empty patch", () => {
    expect(splitPatchByFile("").size).toBe(0);
  });
});

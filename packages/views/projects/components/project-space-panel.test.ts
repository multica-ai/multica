import { describe, expect, it } from "vitest";
import {
  pathInsideBatch,
  type BrowserFile,
} from "./project-space-panel";

function browserFile(name: string, relativePath?: string): BrowserFile {
  return {
    name,
    webkitRelativePath: relativePath,
  } as BrowserFile;
}

describe("pathInsideBatch", () => {
  it("removes the selected root folder because the batch already provides it", () => {
    expect(
      pathInsideBatch(
        browserFile("notes.md", "research/source/notes.md"),
        "research",
      ),
    ).toBe("source/notes.md");
  });

  it("keeps paths from a normal multi-file selection unchanged", () => {
    expect(pathInsideBatch(browserFile("notes.md"), "upload-2026")).toBe(
      "notes.md",
    );
  });
});

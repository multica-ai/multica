import { describe, expect, it } from "vitest";
import {
  isAbsoluteLocalPath,
  joinRunRootRelativePath,
  pickEffectiveOpener,
  resolveLocalFileCandidate,
  resolveRunFileChangeCandidate,
  type LocalFileOpener,
} from "./open-local-file";

const openers: LocalFileOpener[] = [
  { id: "vscode", kind: "editor", name: "Visual Studio Code" },
  { id: "cursor", kind: "editor", name: "Cursor" },
  { id: "file-manager", kind: "file_manager", name: "File manager" },
];

describe("isAbsoluteLocalPath", () => {
  it("accepts native absolute paths and rejects editor protocols", () => {
    expect(isAbsoluteLocalPath("/Users/dev/a.ts")).toBe(true);
    expect(isAbsoluteLocalPath("C:\\Users\\dev\\a.ts")).toBe(true);
    expect(isAbsoluteLocalPath("src/a.ts")).toBe(false);
    expect(isAbsoluteLocalPath("vscode://file/test")).toBe(false);
    expect(isAbsoluteLocalPath("file:///Users/dev/a.ts")).toBe(false);
  });
});

describe("resolveLocalFileCandidate", () => {
  it("keeps an already-absolute path", () => {
    expect(resolveLocalFileCandidate("/tmp/a.ts", "/Users/dev/proj")).toBe(
      "/tmp/a.ts",
    );
  });

  it("joins a relative path onto this run's absolute workdir", () => {
    expect(resolveLocalFileCandidate("src/a.ts", "/Users/dev/proj")).toBe(
      "/Users/dev/proj/src/a.ts",
    );
    expect(
      resolveLocalFileCandidate("src/a.ts", "C:\\Users\\dev\\proj"),
    ).toBe("C:\\Users\\dev\\proj\\src\\a.ts");
  });

  it("hides the button when the path cannot be turned into a local absolute path", () => {
    expect(resolveLocalFileCandidate("src/a.ts")).toBeNull();
    expect(resolveLocalFileCandidate("src/a.ts", "relative/workdir")).toBeNull();
    expect(resolveLocalFileCandidate("vscode://file/test")).toBeNull();
    expect(resolveLocalFileCandidate("")).toBeNull();
  });
});

describe("pickEffectiveOpener", () => {
  it("uses the stored opener when it is still installed", () => {
    expect(pickEffectiveOpener(openers, "vscode")?.id).toBe("vscode");
  });

  it("falls back to Cursor, then VS Code, then the file manager", () => {
    expect(pickEffectiveOpener(openers, "zed")?.id).toBe("cursor");
    expect(
      pickEffectiveOpener(
        openers.filter((item) => item.id !== "cursor"),
        null,
      )?.id,
    ).toBe("vscode");
    expect(
      pickEffectiveOpener(
        openers.filter((item) => item.kind === "file_manager"),
        "cursor",
      )?.id,
    ).toBe("file-manager");
  });
});

describe("joinRunRootRelativePath", () => {
  it("joins a nested repo root label onto the file path", () => {
    expect(joinRunRootRelativePath("repo-b", "CLAUDE.md")).toBe(
      "repo-b/CLAUDE.md",
    );
    expect(joinRunRootRelativePath("a/b", "src/a.ts")).toBe("a/b/src/a.ts");
  });

  it("treats the run-workdir sentinel label as the workdir itself", () => {
    expect(joinRunRootRelativePath("workdir", "CLAUDE.md")).toBe("CLAUDE.md");
  });

  it("rejects labels that could escape or re-anchor the workdir", () => {
    expect(joinRunRootRelativePath("", "a.ts")).toBeNull();
    expect(joinRunRootRelativePath("../outside", "a.ts")).toBeNull();
    expect(joinRunRootRelativePath("/absolute/root", "a.ts")).toBeNull();
    expect(joinRunRootRelativePath("C:\\repo", "a.ts")).toBeNull();
    expect(joinRunRootRelativePath("repo-b", "")).toBeNull();
    expect(joinRunRootRelativePath("repo-b", "../a.ts")).toBeNull();
    expect(joinRunRootRelativePath("repo-b", "/absolute/a.ts")).toBeNull();
  });
});

describe("resolveRunFileChangeCandidate", () => {
  it("resolves a nested repo file onto the run workdir", () => {
    expect(
      resolveRunFileChangeCandidate("repo-b", "CLAUDE.md", "/Users/dev/proj"),
    ).toBe("/Users/dev/proj/repo-b/CLAUDE.md");
    expect(
      resolveRunFileChangeCandidate("workdir", "CLAUDE.md", "/Users/dev/proj"),
    ).toBe("/Users/dev/proj/CLAUDE.md");
  });

  it("returns null without an absolute workdir or for unsafe labels", () => {
    expect(resolveRunFileChangeCandidate("repo-b", "CLAUDE.md")).toBeNull();
    expect(
      resolveRunFileChangeCandidate("repo-b", "CLAUDE.md", "relative/proj"),
    ).toBeNull();
    expect(
      resolveRunFileChangeCandidate("../outside", "a.ts", "/Users/dev/proj"),
    ).toBeNull();
  });
});

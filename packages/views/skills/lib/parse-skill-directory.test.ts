import { describe, expect, it } from "vitest";
import { parseSkillDirectory } from "./parse-skill-directory";

function makeDirectoryFile(
  body: string,
  name: string,
  webkitRelativePath: string,
): File {
  const file = new File([body], name, { type: "text/markdown" });
  Object.defineProperty(file, "webkitRelativePath", {
    configurable: true,
    value: webkitRelativePath,
  });
  return file;
}

function makeFileList(files: File[]): FileList {
  return Object.assign(files, {
    item: (index: number) => files[index] ?? null,
  }) as unknown as FileList;
}

describe("parseSkillDirectory", () => {
  it("keeps supporting files next to a root SKILL.md", async () => {
    const skills = await parseSkillDirectory(
      makeFileList([
        makeDirectoryFile(
          "---\nname: root-skill\ndescription: Root skill\n---\nbody",
          "SKILL.md",
          "root-skill/SKILL.md",
        ),
        makeDirectoryFile("guide", "guide.md", "root-skill/guide.md"),
      ]),
    );

    expect(skills).toEqual([
      {
        name: "root-skill",
        description: "Root skill",
        content: "---\nname: root-skill\ndescription: Root skill\n---\nbody",
        files: [{ path: "guide.md", content: "guide" }],
      },
    ]);
  });

  it("does not attach files claimed by nested SKILL.md directories to the root skill", async () => {
    const skills = await parseSkillDirectory(
      makeFileList([
        makeDirectoryFile("---\nname: root\n---\nroot", "SKILL.md", "repo/SKILL.md"),
        makeDirectoryFile("root guide", "guide.md", "repo/guide.md"),
        makeDirectoryFile("---\nname: nested\n---\nnested", "SKILL.md", "repo/tools/SKILL.md"),
        makeDirectoryFile("nested guide", "guide.md", "repo/tools/guide.md"),
      ]),
    );

    expect(skills).toEqual([
      {
        name: "root",
        description: "",
        content: "---\nname: root\n---\nroot",
        files: [{ path: "guide.md", content: "root guide" }],
      },
      {
        name: "nested",
        description: "",
        content: "---\nname: nested\n---\nnested",
        files: [{ path: "guide.md", content: "nested guide" }],
      },
    ]);
  });

  it("skips cache, dependency, binary, and license supporting files", async () => {
    const skills = await parseSkillDirectory(
      makeFileList([
        makeDirectoryFile("---\nname: clean\n---\nbody", "SKILL.md", "clean/SKILL.md"),
        makeDirectoryFile("guide", "guide.md", "clean/references/guide.md"),
        makeDirectoryFile("compiled", "tool.cpython-312.pyc", "clean/scripts/__pycache__/tool.cpython-312.pyc"),
        makeDirectoryFile("dependency", "index.js", "clean/node_modules/pkg/index.js"),
        makeDirectoryFile("png", "logo.png", "clean/assets/logo.png"),
        makeDirectoryFile("license", "LICENSE", "clean/LICENSE"),
      ]),
    );

    expect(skills).toEqual([
      {
        name: "clean",
        description: "",
        content: "---\nname: clean\n---\nbody",
        files: [{ path: "references/guide.md", content: "guide" }],
      },
    ]);
  });

  it("keeps top-level dot directory formats detectable", async () => {
    const skills = await parseSkillDirectory(
      makeFileList([
        makeDirectoryFile("---\nname: Summarize\n---\nbody", "summarize.md", ".claude/commands/summarize.md"),
      ]),
    );

    expect(skills).toEqual([
      {
        name: "Summarize",
        description: "",
        content: "---\nname: Summarize\n---\nbody",
      },
    ]);
  });

  it("keeps dot directory formats detectable under an uploaded repo root", async () => {
    const skills = await parseSkillDirectory(
      makeFileList([
        makeDirectoryFile("---\nname: Summarize\n---\nbody", "summarize.md", "repo/.claude/commands/summarize.md"),
      ]),
    );

    expect(skills).toEqual([
      {
        name: "Summarize",
        description: "",
        content: "---\nname: Summarize\n---\nbody",
      },
    ]);
  });

  it("skips unrelated dot directories after the uploaded root is stripped", async () => {
    const skills = await parseSkillDirectory(
      makeFileList([
        makeDirectoryFile("---\nname: clean\n---\nbody", "SKILL.md", "repo/SKILL.md"),
        makeDirectoryFile("workflow", "ci.yml", "repo/.github/workflows/ci.yml"),
        makeDirectoryFile("guide", "guide.md", "repo/references/guide.md"),
      ]),
    );

    expect(skills).toEqual([
      {
        name: "clean",
        description: "",
        content: "---\nname: clean\n---\nbody",
        files: [{ path: "references/guide.md", content: "guide" }],
      },
    ]);
  });
});

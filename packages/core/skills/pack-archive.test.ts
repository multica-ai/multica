import { describe, expect, it } from "vitest";
import {
  MAX_SKILL_FILE_BYTES,
  isIgnoredArchiveEntry,
  isSkillArchiveFilename,
  packStoreZip,
  prepareSkillArchiveFromEntries,
  prepareSkillArchiveFromPickerFiles,
  wrapExistingSkillArchive,
} from "./pack-archive";

const SKILL_MD = `---
name: review-helper
description: Reviews code changes
---

# Review Helper

Do the review.
`;

function bytes(s: string): Uint8Array {
  return new TextEncoder().encode(s);
}

/** Minimal STORE-zip reader for round-trip assertions. */
function unzipStore(data: Uint8Array): Map<string, Uint8Array> {
  const view = new DataView(data.buffer, data.byteOffset, data.byteLength);
  const out = new Map<string, Uint8Array>();
  let offset = 0;
  while (offset + 4 <= data.length) {
    const sig = view.getUint32(offset, true);
    if (sig !== 0x04034b50) break;
    const nameLen = view.getUint16(offset + 26, true);
    const extraLen = view.getUint16(offset + 28, true);
    const size = view.getUint32(offset + 22, true);
    const name = new TextDecoder().decode(
      data.subarray(offset + 30, offset + 30 + nameLen),
    );
    const start = offset + 30 + nameLen + extraLen;
    out.set(name, data.subarray(start, start + size));
    offset = start + size;
  }
  return out;
}

describe("prepareSkillArchiveFromEntries", () => {
  it("packs a nested wrapper directory", async () => {
    const prepared = prepareSkillArchiveFromEntries([
      { relativePath: "review-helper/SKILL.md", data: bytes(SKILL_MD) },
      { relativePath: "review-helper/scripts/run.sh", data: bytes("echo hi") },
      { relativePath: "review-helper/.DS_Store", data: bytes("junk") },
    ]);
    expect(prepared.ok).toBe(true);
    if (!prepared.ok) return;
    expect(prepared.preview.skillName).toBe("review-helper");
    expect(prepared.preview.description).toBe("Reviews code changes");
    expect(prepared.preview.fileCount).toBe(2);
    expect(prepared.preview.source).toBe("folder");
    expect(prepared.file.name).toBe("review-helper.skill");

    const zip = new Uint8Array(await prepared.file.arrayBuffer());
    const files = unzipStore(zip);
    expect([...files.keys()].sort()).toEqual([
      "review-helper/SKILL.md",
      "review-helper/scripts/run.sh",
    ]);
    expect(new TextDecoder().decode(files.get("review-helper/scripts/run.sh"))).toBe(
      "echo hi",
    );
  });

  it("packs a root-level SKILL.md", () => {
    const prepared = prepareSkillArchiveFromEntries([
      { relativePath: "SKILL.md", data: bytes(SKILL_MD) },
      { relativePath: "references/doc.md", data: bytes("doc") },
    ]);
    expect(prepared.ok).toBe(true);
    if (!prepared.ok) return;
    expect(prepared.preview.skillName).toBe("review-helper");
    expect(prepared.preview.fileCount).toBe(2);
  });

  it("uses the wrapper directory name when frontmatter has no name", () => {
    const prepared = prepareSkillArchiveFromEntries([
      {
        relativePath: "my-skill/SKILL.md",
        data: bytes("# Untitled\n"),
      },
    ]);
    expect(prepared.ok).toBe(true);
    if (!prepared.ok) return;
    expect(prepared.preview.skillName).toBe("my-skill");
  });

  it("rejects a folder without SKILL.md", () => {
    const prepared = prepareSkillArchiveFromEntries([
      { relativePath: "notes/readme.md", data: bytes("hi") },
    ]);
    expect(prepared).toEqual({ ok: false, error: "missing_skill_md" });
  });

  it("rejects an empty file list", () => {
    expect(prepareSkillArchiveFromEntries([])).toEqual({
      ok: false,
      error: "empty",
    });
  });

  it("skips binary supporting files instead of failing the import", () => {
    const prepared = prepareSkillArchiveFromEntries([
      { relativePath: "skill/SKILL.md", data: bytes(SKILL_MD) },
      { relativePath: "skill/icon.png", data: new Uint8Array([0x89, 0x50, 0x4e, 0x47]) },
    ]);
    expect(prepared.ok).toBe(true);
    if (!prepared.ok) return;
    expect(prepared.preview.fileCount).toBe(1);
  });
});

describe("wrapExistingSkillArchive", () => {
  it("keeps a .skill file as-is", () => {
    const payload = bytes("pk");
    const copy = new Uint8Array(payload.byteLength);
    copy.set(payload);
    const file = new File([copy.buffer], "review-helper.skill", {
      type: "application/zip",
    });
    const prepared = wrapExistingSkillArchive(file);
    expect(prepared.ok).toBe(true);
    if (!prepared.ok) return;
    expect(prepared.file).toBe(file);
    expect(prepared.preview.skillName).toBe("review-helper");
    expect(prepared.preview.source).toBe("archive");
    expect(prepared.preview.fileCount).toBeNull();
  });
});

describe("isSkillArchiveFilename", () => {
  it("accepts .skill and .zip", () => {
    expect(isSkillArchiveFilename("foo.skill")).toBe(true);
    expect(isSkillArchiveFilename("Foo.ZIP")).toBe(true);
    expect(isSkillArchiveFilename("SKILL.md")).toBe(false);
  });
});

describe("isIgnoredArchiveEntry", () => {
  it("drops editor/OS noise and license files", () => {
    expect(isIgnoredArchiveEntry("skill/.DS_Store")).toBe(true);
    expect(isIgnoredArchiveEntry("__MACOSX/foo")).toBe(true);
    expect(isIgnoredArchiveEntry("skill/LICENSE")).toBe(true);
    expect(isIgnoredArchiveEntry("skill/scripts/run.sh")).toBe(false);
  });
});

function fileWithPath(
  name: string,
  relativePath: string,
  content: Uint8Array,
  opts?: { size?: number; arrayBuffer?: () => Promise<ArrayBuffer> },
): File {
  const copy = new Uint8Array(content.byteLength);
  copy.set(content);
  const file = new File([copy.buffer], name);
  Object.defineProperty(file, "webkitRelativePath", { value: relativePath });
  if (opts?.size != null) {
    Object.defineProperty(file, "size", { value: opts.size });
  }
  if (opts?.arrayBuffer) {
    Object.defineProperty(file, "arrayBuffer", { value: opts.arrayBuffer });
  }
  return file;
}

describe("prepareSkillArchiveFromPickerFiles", () => {
  it("does not call arrayBuffer for ignored or oversized entries", async () => {
    const skill = fileWithPath("SKILL.md", "skill/SKILL.md", bytes(SKILL_MD));
    const ignored = fileWithPath(
      ".DS_Store",
      "skill/.DS_Store",
      new Uint8Array([1]),
      {
        arrayBuffer: async () => {
          throw new Error("should not read ignored");
        },
      },
    );
    const oversized = fileWithPath(
      "big.md",
      "skill/big.md",
      new Uint8Array(0),
      {
        size: MAX_SKILL_FILE_BYTES + 1,
        arrayBuffer: async () => {
          throw new Error("should not read oversized");
        },
      },
    );
    const binary = fileWithPath(
      "icon.png",
      "skill/icon.png",
      new Uint8Array([0x89, 0x50]),
      {
        arrayBuffer: async () => {
          throw new Error("should not read binary");
        },
      },
    );

    const prepared = await prepareSkillArchiveFromPickerFiles([
      skill,
      ignored,
      oversized,
      binary,
    ]);
    expect(prepared.ok).toBe(true);
    if (!prepared.ok) return;
    expect(prepared.preview.fileCount).toBe(1);
  });
});

describe("packStoreZip", () => {
  it("round-trips file names and contents", () => {
    const zip = packStoreZip([
      { path: "SKILL.md", data: bytes("hello") },
      { path: "scripts/run.sh", data: bytes("echo hi") },
    ]);
    expect(zip[0]).toBe(0x50);
    expect(zip[1]).toBe(0x4b);
    const files = unzipStore(zip);
    expect(new TextDecoder().decode(files.get("SKILL.md"))).toBe("hello");
    expect(new TextDecoder().decode(files.get("scripts/run.sh"))).toBe("echo hi");
  });
});

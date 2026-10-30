// @vitest-environment node
import { describe, expect, it, vi } from "vitest";
import {
  MAX_SKILL_FILE_BYTES,
  discoverSkillArchivesFromPickerFiles,
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
    expect(prepared.reconciliationName).toBe("review-helper");
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
    expect(
      new TextDecoder().decode(files.get("review-helper/scripts/run.sh")),
    ).toBe("echo hi");
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
      {
        relativePath: "skill/icon.png",
        data: new Uint8Array([0x89, 0x50, 0x4e, 0x47]),
      },
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
    expect(new TextDecoder().decode(files.get("scripts/run.sh"))).toBe(
      "echo hi",
    );
  });
});

describe("discoverSkillArchivesFromPickerFiles", () => {
  const entry = (path: string) =>
    fileWithPath("SKILL.md", path, bytes(SKILL_MD));

  it("discovers 13 sibling roots in stable path order and packs isolated contents", async () => {
    const files = Array.from({ length: 13 }, (_, i) =>
      entry(`skills/skill-${String(i).padStart(2, "0")}/SKILL.md`),
    ).reverse();
    const result = await discoverSkillArchivesFromPickerFiles(files);
    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.items).toHaveLength(13);
    expect(result.items.map((item) => item.id)).toEqual(
      [...result.items.map((item) => item.id)].sort(),
    );
    for (const item of result.items) {
      const packed = await item.prepare();
      expect(packed.ok).toBe(true);
      if (packed.ok)
        expect([
          ...unzipStore(new Uint8Array(await packed.file.arrayBuffer())).keys(),
        ]).toEqual([`${item.id}SKILL.md`]);
    }
  });

  it("keeps different-depth siblings and similar prefixes separate, stopping at parent roots", async () => {
    const result = await discoverSkillArchivesFromPickerFiles([
      entry("skills/foo/SKILL.md"),
      entry("skills/foobar/SKILL.md"),
      entry("skills/group/deep/SKILL.md"),
      entry("skills/foo/example/SKILL.md"),
      entry("skills/.hidden/SKILL.md"),
    ]);
    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.items.map((item) => item.id)).toEqual([
      "skills/foo/",
      "skills/foobar/",
      "skills/group/deep/",
    ]);
    const packed = await result.items[0]!.prepare();
    if (!packed.ok) throw new Error("packing failed");
    expect([
      ...unzipStore(new Uint8Array(await packed.file.arrayBuffer())).keys(),
    ]).toEqual(["skills/foo/SKILL.md", "skills/foo/example/SKILL.md"]);
  });

  it("treats a selected root with SKILL.md as one skill", async () => {
    const result = await discoverSkillArchivesFromPickerFiles([
      entry("skills/SKILL.md"),
      entry("skills/child/SKILL.md"),
    ]);
    expect(result.ok && result.items.map((item) => item.id)).toEqual([
      "skills/",
    ]);
  });

  it("rejects missing entrypoints and empty selections", async () => {
    expect(await discoverSkillArchivesFromPickerFiles([])).toEqual({
      ok: false,
      error: "empty",
    });
    expect(
      await discoverSkillArchivesFromPickerFiles([
        fileWithPath("README.md", "skills/README.md", bytes("readme")),
      ]),
    ).toEqual({ ok: false, error: "missing_skill_md" });
  });

  it("isolates size, count and read failures without reading supporting files at discovery", async () => {
    const read = vi.fn(async () => {
      throw new Error("must not read supporting files during discovery");
    });
    const result = await discoverSkillArchivesFromPickerFiles([
      entry("skills/good/SKILL.md"),
      fileWithPath("doc.md", "skills/good/doc.md", bytes("doc"), {
        arrayBuffer: read,
      }),
      fileWithPath("SKILL.md", "skills/large/SKILL.md", bytes(""), {
        size: MAX_SKILL_FILE_BYTES + 1,
        arrayBuffer: read,
      }),
      entry("skills/count/SKILL.md"),
      ...Array.from({ length: 257 }, (_, i) =>
        fileWithPath(`${i}.md`, `skills/count/${i}.md`, bytes(""), {
          arrayBuffer: read,
        }),
      ),
      fileWithPath("SKILL.md", "skills/unreadable/SKILL.md", bytes(""), {
        arrayBuffer: read,
      }),
      fileWithPath("icon.png", "skills/good/icon.png", bytes(""), {
        arrayBuffer: read,
      }),
      fileWithPath(".hidden", "skills/good/.hidden", bytes(""), {
        arrayBuffer: read,
      }),
    ]);
    if (!result.ok) throw new Error("discovery failed");
    expect(result.items.map((item) => [item.id, item.error])).toEqual([
      ["skills/count/", "too_many_files"],
      ["skills/good/", undefined],
      ["skills/large/", "too_large"],
      ["skills/unreadable/", "unreadable"],
    ]);
    expect(result.items[1]!.preview.fileCount).toBe(2);
    expect(read).toHaveBeenCalledTimes(1);
  });
  it("applies the bundle byte limit per root", async () => {
    const result = await discoverSkillArchivesFromPickerFiles([
      fileWithPath("SKILL.md", "skills/large/SKILL.md", bytes(SKILL_MD)),
      ...Array.from({ length: 9 }, (_, i) =>
        fileWithPath(`${i}.md`, `skills/large/${i}.md`, bytes(""), {
          size: MAX_SKILL_FILE_BYTES,
        }),
      ),
      fileWithPath("SKILL.md", "skills/small/SKILL.md", bytes(SKILL_MD)),
    ]);
    if (!result.ok) throw new Error("discovery failed");
    expect(result.items.map((item) => item.error)).toEqual([
      "too_large",
      undefined,
    ]);
    expect((await result.items[1]!.prepare()).ok).toBe(true);
  });
});

describe("prepared archive reconciliation identity", () => {
  it.each(["entries", "picker"] as const)(
    "uses raw BOM semantics for %s while preserving preview and uploaded bytes",
    async (source) => {
      const content = bytes("\ufeff---\nname: alpha\n---\n# Alpha");
      const relativePath = "download/SKILL.md";
      const archive =
        source === "entries"
          ? prepareSkillArchiveFromEntries([{ relativePath, data: content }])
          : await prepareSkillArchiveFromPickerFiles([
              fileWithPath("SKILL.md", relativePath, content),
            ]);
      if (!archive.ok) throw new Error("packing failed");
      expect(archive.preview.skillName).toBe("alpha");
      expect(archive.reconciliationName).toBe("download");
      const packed = unzipStore(
        new Uint8Array(await archive.file.arrayBuffer()),
      );
      expect(packed.get(relativePath)).toEqual(content);
    },
  );

  it("uses the generated upload name for a root-level entry without frontmatter", () => {
    const archive = prepareSkillArchiveFromEntries([
      { relativePath: "SKILL.md", data: bytes("# Root") },
    ]);
    if (!archive.ok) throw new Error("packing failed");
    expect(archive.reconciliationName).toBe("skill");
  });

  it("keeps a prebuilt archive identity unknown regardless of its filename", () => {
    const archive = wrapExistingSkillArchive(new File(["zip"], "download.zip"));
    if (!archive.ok) throw new Error("wrapping failed");
    expect(archive.reconciliationName).toBeNull();
  });
});

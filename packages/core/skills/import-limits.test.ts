// @vitest-environment node
import { describe, expect, it, vi } from "vitest";
import { SkillFileSchema } from "../api/schemas";
import {
  MAX_SKILL_ARCHIVE_BYTES, MAX_SKILL_BUNDLE_BYTES, MAX_SKILL_FILE_BYTES,
  MAX_SKILL_FILE_COUNT, packStoreZipBlob, selectSkillArchiveMembers,
  wrapExistingSkillArchive,
} from "./pack-archive";

const primary = { path: "SKILL.md", size: 20 };

describe("skill import ceilings", () => {
  it("uses exact binary sizes and accepts archive boundary metadata", () => {
    expect([MAX_SKILL_FILE_BYTES, MAX_SKILL_BUNDLE_BYTES, MAX_SKILL_ARCHIVE_BYTES, MAX_SKILL_FILE_COUNT])
      .toEqual([104857600, 1099511627776, 1099511627776, 100000]);
    for (const delta of [-1, 0, 1]) {
      const file = new File([], "test.zip");
      Object.defineProperty(file, "size", { value: MAX_SKILL_ARCHIVE_BYTES + delta });
      expect(wrapExistingSkillArchive(file).ok).toBe(delta <= 0);
    }
  });

  it("enforces total and file counts using sizes only", () => {
    const files = [primary];
    let remaining = MAX_SKILL_BUNDLE_BYTES;
    while (remaining > 0) {
      const size = Math.min(remaining, MAX_SKILL_FILE_BYTES);
      files.push({ path: `ref-${files.length}.md`, size });
      remaining -= size;
    }
    expect(selectSkillArchiveMembers(files).ok).toBe(true);
    expect(selectSkillArchiveMembers([...files, { path: "extra.md", size: 1 }]))
      .toEqual({ ok: false, error: "too_large" });
    const many = Array.from({ length: MAX_SKILL_FILE_COUNT }, (_, i) => ({ path: `${i}.md`, size: 0 }));
    expect(selectSkillArchiveMembers([primary, ...many]).ok).toBe(true);
    expect(selectSkillArchiveMembers([primary, ...many, { path: "extra.md", size: 0 }]))
      .toEqual({ ok: false, error: "too_many_files" });
  });

  it("writes real ZIP64 counts for 100001 small entries", async () => {
    const empty = new Blob([]);
    const archive = packStoreZipBlob(Array.from({ length: 100001 }, (_, i) => ({ path: `${i}.md`, data: empty, crc: 0 })));
    const tail = new DataView(await archive.slice(-98).arrayBuffer());
    expect(tail.getUint32(0, true)).toBe(0x06064b50);
    expect(tail.getBigUint64(32, true)).toBe(100001n);
    expect(tail.getUint16(76 + 10, true)).toBe(65535);
  });

  it("writes 64-bit offsets using simulated file sizes without allocating GiB", async () => {
    class MetadataBlob {
      constructor(public parts: BlobPart[] = []) {}
      size = MAX_SKILL_FILE_BYTES;
    }
    vi.stubGlobal("Blob", MetadataBlob);
    try {
      const simulated = new Blob([]);
      const archive = packStoreZipBlob(Array.from({ length: 45 }, (_, i) => ({ path: `${i}.md`, data: simulated, crc: 0 }))) as unknown as MetadataBlob;
      const tail = new DataView(archive.parts.at(-1) as ArrayBuffer);
      expect(tail.getUint32(0, true)).toBe(0x06064b50);
      expect(tail.getBigUint64(48, true)).toBeGreaterThan(0xffffffffn);
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it("preserves explicit omission and tolerates malformed API flags", () => {
    const file = { id: "f", skill_id: "s", path: "ref.md" };
    expect(SkillFileSchema.parse({ ...file, content_omitted: true })).toMatchObject({ content: "", content_omitted: true });
    expect(SkillFileSchema.parse({ ...file, content_omitted: "bad" }).content_omitted).toBe(false);
    expect(SkillFileSchema.parse(file).content).toBe("");
  });
});

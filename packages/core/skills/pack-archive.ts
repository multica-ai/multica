import { parseFrontmatter } from "./frontmatter";

/** Mirrors server/internal/handler maxImportFileSize. */
export const MAX_SKILL_FILE_BYTES = 1 << 20; // 1 MiB
/** Mirrors server/internal/handler maxImportTotalSize (supporting files). */
export const MAX_SKILL_BUNDLE_BYTES = 8 << 20; // 8 MiB
/** Mirrors server/internal/handler maxImportFileCount. */
export const MAX_SKILL_FILE_COUNT = 256;
/** Mirrors server/internal/handler maxImportArchiveUploadSize. */
export const MAX_SKILL_ARCHIVE_BYTES = 16 << 20; // 16 MiB

const SKILL_MD = "skill.md";

export type SkillArchiveEntry = {
  relativePath: string;
  data: Uint8Array;
};

export type PreparedSkillArchiveError =
  | "missing_skill_md"
  | "empty"
  | "too_large"
  | "too_many_files";

export type PreparedSkillArchive =
  | {
      ok: true;
      file: File;
      preview: {
        displayName: string;
        skillName: string;
        description: string;
        fileCount: number | null;
        source: "folder" | "archive";
      };
    }
  | { ok: false; error: PreparedSkillArchiveError };

export function isSkillArchiveFilename(name: string): boolean {
  const lower = name.toLowerCase();
  return lower.endsWith(".skill") || lower.endsWith(".zip");
}

export function wrapExistingSkillArchive(file: File): PreparedSkillArchive {
  if (file.size > MAX_SKILL_ARCHIVE_BYTES) {
    return { ok: false, error: "too_large" };
  }
  const base = file.name.replace(/\\/g, "/").split("/").pop() ?? file.name;
  const skillName = stripArchiveExtension(base) || "skill";
  return {
    ok: true,
    file,
    preview: {
      displayName: base,
      skillName,
      description: "",
      fileCount: null,
      source: "archive",
    },
  };
}

export function prepareSkillArchiveFromEntries(
  entries: SkillArchiveEntry[],
): PreparedSkillArchive {
  const data = new Map<string, Uint8Array>();
  const members: SkillArchiveMemberMeta[] = [];
  for (const entry of entries) {
    const path = normalizeRelPath(entry.relativePath);
    members.push({ path, size: entry.data.byteLength });
    if (!data.has(path)) data.set(path, entry.data);
  }

  const picked = selectSkillArchiveMembers(members);
  if (!picked.ok) return picked;
  return buildPreparedArchive(picked, (path) => data.get(path) ?? EMPTY_BYTES);
}

const EMPTY_BYTES = new Uint8Array();

/**
 * Shared tail of both prepare paths: zip the members `selectSkillArchiveMembers`
 * already approved and derive the preview. `dataFor` supplies the bytes of a
 * selected path — the entries path holds them in memory, the picker path reads
 * them only once selection is done.
 */
function buildPreparedArchive(
  selection: SkillArchiveSelection,
  dataFor: (path: string) => Uint8Array,
): PreparedSkillArchive {
  const packed = selection.selected.map((member) => ({
    path: member.path,
    data: dataFor(member.path),
  }));

  const content = new TextDecoder("utf-8").decode(dataFor(selection.skillMdPath));
  const { frontmatter } = parseFrontmatter(content);
  const wrapperName = wrapperNameFromPrefix(selection.prefix);
  const skillName = frontmatter?.name?.trim() || wrapperName || "skill";
  const description = frontmatter?.description?.trim() ?? "";
  const zip = packStoreZip(packed);
  if (zip.byteLength > MAX_SKILL_ARCHIVE_BYTES) {
    return { ok: false, error: "too_large" };
  }

  const filename = `${sanitizeArchiveBasename(skillName)}.skill`;
  const file = new File([uint8ToArrayBuffer(zip)], filename, {
    type: "application/zip",
  });
  return {
    ok: true,
    file,
    preview: {
      displayName: wrapperName || skillName,
      skillName,
      description,
      fileCount: packed.length,
      source: "folder",
    },
  };
}

export type SkillArchiveMemberMeta = {
  path: string;
  size: number;
};

export type SkillArchiveSelection = {
  /** Members to pack, in input order, with normalized paths. */
  selected: SkillArchiveMemberMeta[];
  /** Normalized path of the SKILL.md that rooted the skill. */
  skillMdPath: string;
  /** Directory prefix of the skill root; "" when SKILL.md sits at the root. */
  prefix: string;
};

/**
 * The single place the ignore rules and the size / count limits are applied.
 * Decides membership from path + size alone — no I/O — so the picker path can
 * reject a directory before reading it, and both prepare paths stay in step by
 * construction rather than by keeping two copies of these rules in agreement.
 * Mirrors the server importer.
 */
export function selectSkillArchiveMembers(
  files: SkillArchiveMemberMeta[],
):
  | ({ ok: true } & SkillArchiveSelection)
  | { ok: false; error: PreparedSkillArchiveError } {
  if (files.length === 0) return { ok: false, error: "empty" };

  const normalized: SkillArchiveMemberMeta[] = [];
  for (const file of files) {
    const path = normalizeRelPath(file.path);
    if (!path || isIgnoredArchiveEntry(path)) continue;
    normalized.push({ path, size: file.size });
  }
  if (normalized.length === 0) return { ok: false, error: "empty" };

  let skillMd: (SkillArchiveMemberMeta & { prefix: string }) | null = null;
  for (const file of normalized) {
    if (!isSkillMdPath(file.path)) continue;
    const prefix = archiveEntryPrefix(file.path);
    if (!skillMd || prefix.length < skillMd.prefix.length) {
      skillMd = { ...file, prefix };
    }
  }
  if (!skillMd) return { ok: false, error: "missing_skill_md" };
  if (skillMd.size > MAX_SKILL_FILE_BYTES) {
    return { ok: false, error: "too_large" };
  }

  const selected: SkillArchiveMemberMeta[] = [];
  let supportingCount = 0;
  let supportingBytes = 0;
  for (const file of normalized) {
    if (skillMd.prefix && !file.path.startsWith(skillMd.prefix)) continue;
    const rel = skillMd.prefix ? file.path.slice(skillMd.prefix.length) : file.path;
    if (!rel || rel.endsWith("/")) continue;
    if (isSkillMdPath(rel)) {
      selected.push(file);
      continue;
    }
    if (isLikelyBinaryFilePath(rel)) continue;
    if (file.size > MAX_SKILL_FILE_BYTES) continue;
    supportingCount += 1;
    if (supportingCount > MAX_SKILL_FILE_COUNT) {
      return { ok: false, error: "too_many_files" };
    }
    supportingBytes += file.size;
    if (supportingBytes > MAX_SKILL_BUNDLE_BYTES) {
      return { ok: false, error: "too_large" };
    }
    selected.push(file);
  }
  return {
    ok: true,
    selected,
    skillMdPath: skillMd.path,
    prefix: skillMd.prefix,
  };
}

function pickerRelativePath(file: File): string {
  const rel =
    typeof file.webkitRelativePath === "string" && file.webkitRelativePath
      ? file.webkitRelativePath
      : file.name;
  return normalizeRelPath(rel);
}

/**
 * Pack a directory picker result. Ignored, binary, oversized and out-of-root
 * files are dropped from path + `File.size` before any `arrayBuffer()` call.
 */
export async function prepareSkillArchiveFromPickerFiles(
  files: File[],
): Promise<PreparedSkillArchive> {
  const sources = new Map<string, File>();
  const members: SkillArchiveMemberMeta[] = [];
  for (const file of files) {
    const path = pickerRelativePath(file);
    members.push({ path, size: file.size });
    if (!sources.has(path)) sources.set(path, file);
  }

  const picked = selectSkillArchiveMembers(members);
  if (!picked.ok) return picked;

  const data = new Map<string, Uint8Array>();
  for (const member of picked.selected) {
    if (data.has(member.path)) continue;
    const source = sources.get(member.path);
    data.set(
      member.path,
      source ? new Uint8Array(await source.arrayBuffer()) : EMPTY_BYTES,
    );
  }
  return buildPreparedArchive(picked, (path) => data.get(path) ?? EMPTY_BYTES);
}

// --- zip (STORE / uncompressed) -------------------------------------------

const CRC32_TABLE = (() => {
  const table = new Uint32Array(256);
  for (let i = 0; i < 256; i++) {
    let c = i;
    for (let j = 0; j < 8; j++) {
      c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
    }
    table[i] = c >>> 0;
  }
  return table;
})();

function crc32(data: Uint8Array): number {
  let c = 0xffffffff;
  for (let i = 0; i < data.length; i++) {
    c = CRC32_TABLE[(c ^ data[i]!) & 0xff]! ^ (c >>> 8);
  }
  return (c ^ 0xffffffff) >>> 0;
}

function utf8(s: string): Uint8Array {
  return new TextEncoder().encode(s);
}

function uint8ToArrayBuffer(data: Uint8Array): ArrayBuffer {
  const copy = new Uint8Array(data.byteLength);
  copy.set(data);
  return copy.buffer;
}

/**
 * Builds an uncompressed zip. The server parser (`parseSkillArchive`) accepts
 * STORE archives; skipping compression keeps this helper dependency-free.
 */
export function packStoreZip(entries: { path: string; data: Uint8Array }[]): Uint8Array {
  const files = entries.map((e) => ({
    name: utf8(e.path.replace(/\\/g, "/")),
    data: e.data,
    crc: crc32(e.data),
  }));

  let localSize = 0;
  let centralSize = 0;
  for (const f of files) {
    localSize += 30 + f.name.length + f.data.length;
    centralSize += 46 + f.name.length;
  }
  const out = new Uint8Array(localSize + centralSize + 22);
  const view = new DataView(out.buffer);
  let offset = 0;
  const localOffsets: number[] = [];

  for (const f of files) {
    localOffsets.push(offset);
    view.setUint32(offset, 0x04034b50, true);
    view.setUint16(offset + 4, 20, true);
    view.setUint16(offset + 6, 0x0800, true); // UTF-8 names
    view.setUint16(offset + 8, 0, true); // STORE
    view.setUint16(offset + 10, 0, true);
    view.setUint16(offset + 12, 0, true);
    view.setUint32(offset + 14, f.crc, true);
    view.setUint32(offset + 18, f.data.length, true);
    view.setUint32(offset + 22, f.data.length, true);
    view.setUint16(offset + 26, f.name.length, true);
    view.setUint16(offset + 28, 0, true);
    out.set(f.name, offset + 30);
    out.set(f.data, offset + 30 + f.name.length);
    offset += 30 + f.name.length + f.data.length;
  }

  const centralStart = offset;
  for (let i = 0; i < files.length; i++) {
    const f = files[i]!;
    view.setUint32(offset, 0x02014b50, true);
    view.setUint16(offset + 4, 20, true);
    view.setUint16(offset + 6, 20, true);
    view.setUint16(offset + 8, 0x0800, true);
    view.setUint16(offset + 10, 0, true);
    view.setUint16(offset + 12, 0, true);
    view.setUint16(offset + 14, 0, true);
    view.setUint32(offset + 16, f.crc, true);
    view.setUint32(offset + 20, f.data.length, true);
    view.setUint32(offset + 24, f.data.length, true);
    view.setUint16(offset + 28, f.name.length, true);
    view.setUint16(offset + 30, 0, true);
    view.setUint16(offset + 32, 0, true);
    view.setUint16(offset + 34, 0, true);
    view.setUint16(offset + 36, 0, true);
    view.setUint32(offset + 38, 0, true);
    view.setUint32(offset + 42, localOffsets[i]!, true);
    out.set(f.name, offset + 46);
    offset += 46 + f.name.length;
  }

  view.setUint32(offset, 0x06054b50, true);
  view.setUint16(offset + 4, 0, true);
  view.setUint16(offset + 6, 0, true);
  view.setUint16(offset + 8, files.length, true);
  view.setUint16(offset + 10, files.length, true);
  view.setUint32(offset + 12, offset - centralStart, true);
  view.setUint32(offset + 16, centralStart, true);
  view.setUint16(offset + 20, 0, true);
  return out;
}

// --- path helpers (mirror skill_import_archive.go) ------------------------

function normalizeRelPath(p: string): string {
  return p.replace(/\\/g, "/").replace(/^\/+/, "");
}

function isSkillMdPath(path: string): boolean {
  const base = path.split("/").pop() ?? "";
  return base.toLowerCase() === SKILL_MD;
}

function archiveEntryPrefix(cleanName: string): string {
  const i = cleanName.lastIndexOf("/");
  if (i <= 0) return "";
  return cleanName.slice(0, i + 1);
}

function wrapperNameFromPrefix(prefix: string): string {
  const trimmed = prefix.replace(/\/+$/, "");
  if (!trimmed) return "";
  const base = trimmed.split("/").pop() ?? "";
  if (base === "." || base === "..") return "";
  return base;
}

export function isIgnoredArchiveEntry(rel: string): boolean {
  const segments = rel.split("/");
  for (const seg of segments) {
    if (seg === "" || seg === "__MACOSX" || seg.startsWith(".")) return true;
  }
  const base = (segments[segments.length - 1] ?? "").toLowerCase();
  return base === "license" || base === "license.md" || base === "license.txt";
}

export function isLikelyBinaryFilePath(path: string): boolean {
  const ext = extensionOf(path);
  switch (ext) {
    case ".png":
    case ".jpg":
    case ".jpeg":
    case ".gif":
    case ".webp":
    case ".bmp":
    case ".tiff":
    case ".ico":
    case ".heic":
    case ".ttf":
    case ".otf":
    case ".woff":
    case ".woff2":
    case ".eot":
    case ".zip":
    case ".gz":
    case ".tar":
    case ".bz2":
    case ".7z":
    case ".rar":
    case ".pdf":
    case ".docx":
    case ".xlsx":
    case ".pptx":
    case ".doc":
    case ".xls":
    case ".ppt":
    case ".mp3":
    case ".mp4":
    case ".wav":
    case ".avi":
    case ".mov":
    case ".webm":
    case ".m4a":
    case ".flac":
    case ".exe":
    case ".dll":
    case ".so":
    case ".dylib":
    case ".class":
    case ".jar":
    case ".wasm":
    case ".db":
    case ".sqlite":
    case ".sqlite3":
    case ".pyc":
      return true;
    default:
      return false;
  }
}

function extensionOf(path: string): string {
  const base = path.split("/").pop() ?? path;
  const i = base.lastIndexOf(".");
  if (i <= 0) return "";
  return base.slice(i).toLowerCase();
}

function stripArchiveExtension(name: string): string {
  return name.replace(/\.(skill|zip)$/i, "");
}

function sanitizeArchiveBasename(name: string): string {
  const cleaned = name.replace(/[/\\?%*:|"<>]/g, "-").trim();
  return cleaned || "skill";
}

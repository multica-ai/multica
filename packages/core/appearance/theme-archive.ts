/**
 * Reads the files out of a .zip, enough for theme packages: stored and
 * deflated entries from the central directory, no encryption, no zip64.
 * Inflating uses the platform's DecompressionStream. `writeZip` writes the
 * stored form back, for exporting a theme.
 */

export interface ArchiveFile {
  path: string;
  data: Uint8Array;
}

const END_OF_CENTRAL_DIRECTORY = 0x06054b50;
const CENTRAL_FILE_HEADER = 0x02014b50;
const LOCAL_FILE_HEADER = 0x04034b50;

async function inflate(data: Uint8Array): Promise<Uint8Array> {
  const source = new ReadableStream<Uint8Array>({
    start(controller) {
      controller.enqueue(data);
      controller.close();
    },
  });
  const stream = source.pipeThrough(new DecompressionStream("deflate-raw") as ReadableWritablePair<Uint8Array, Uint8Array>);
  return new Uint8Array(await new Response(stream).arrayBuffer());
}

/**
 * Every file in the archive, or an error message. Stops reading once the
 * uncompressed total passes `maxBytes`, so a zip bomb cannot exhaust memory.
 */
export async function readZip(
  bytes: Uint8Array,
  maxBytes: number,
): Promise<{ ok: true; files: ArchiveFile[] } | { ok: false; error: string }> {
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  let end = -1;
  for (let at = bytes.length - 22; at >= Math.max(0, bytes.length - 22 - 0xffff); at -= 1) {
    if (view.getUint32(at, true) === END_OF_CENTRAL_DIRECTORY) {
      end = at;
      break;
    }
  }
  if (end < 0) return { ok: false, error: "Not a zip file" };

  const count = view.getUint16(end + 10, true);
  let at = view.getUint32(end + 16, true);
  const decoder = new TextDecoder();
  const files: ArchiveFile[] = [];
  let total = 0;

  for (let n = 0; n < count; n += 1) {
    if (at + 46 > bytes.length || view.getUint32(at, true) !== CENTRAL_FILE_HEADER) {
      return { ok: false, error: "The zip file is damaged" };
    }
    const flags = view.getUint16(at + 8, true);
    const method = view.getUint16(at + 10, true);
    const compressedSize = view.getUint32(at + 20, true);
    const size = view.getUint32(at + 24, true);
    const nameLength = view.getUint16(at + 28, true);
    const extraLength = view.getUint16(at + 30, true);
    const commentLength = view.getUint16(at + 32, true);
    const local = view.getUint32(at + 42, true);
    const path = decoder.decode(bytes.subarray(at + 46, at + 46 + nameLength));
    at += 46 + nameLength + extraLength + commentLength;

    if (path.endsWith("/")) continue;
    if (flags & 1) return { ok: false, error: "Encrypted zip files are not supported" };
    total += size;
    if (total > maxBytes) return { ok: false, error: `The theme is larger than ${maxBytes / 1024} KB` };
    if (local + 30 > bytes.length || view.getUint32(local, true) !== LOCAL_FILE_HEADER) {
      return { ok: false, error: "The zip file is damaged" };
    }
    const start = local + 30 + view.getUint16(local + 26, true) + view.getUint16(local + 28, true);
    const raw = bytes.subarray(start, start + compressedSize);
    if (method === 0) files.push({ path, data: raw });
    else if (method === 8) {
      const data = await inflate(raw).catch(() => null);
      if (!data || data.length !== size) return { ok: false, error: "The zip file is damaged" };
      files.push({ path, data });
    } else return { ok: false, error: "The zip file uses an unsupported compression method" };
  }
  return { ok: true, files };
}

const CRC_TABLE = (() => {
  const table = new Uint32Array(256);
  for (let n = 0; n < 256; n += 1) {
    let c = n;
    for (let k = 0; k < 8; k += 1) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
    table[n] = c >>> 0;
  }
  return table;
})();

function crc32(data: Uint8Array): number {
  let crc = 0xffffffff;
  for (const byte of data) crc = CRC_TABLE[(crc ^ byte) & 0xff]! ^ (crc >>> 8);
  return (crc ^ 0xffffffff) >>> 0;
}

/** A .zip of `files`, uncompressed. Paths use `/`; UTF-8 names are flagged as such. */
export function writeZip(files: readonly ArchiveFile[]): Uint8Array<ArrayBuffer> {
  const encoder = new TextEncoder();
  const entries = files.map((file) => ({ name: encoder.encode(file.path), data: file.data, crc: crc32(file.data) }));
  const localSize = entries.reduce((sum, e) => sum + 30 + e.name.length + e.data.length, 0);
  const centralSize = entries.reduce((sum, e) => sum + 46 + e.name.length, 0);
  const out = new Uint8Array(localSize + centralSize + 22);
  const view = new DataView(out.buffer);
  const UTF8 = 0x0800;
  let at = 0;
  const offsets: number[] = [];
  for (const e of entries) {
    offsets.push(at);
    view.setUint32(at, LOCAL_FILE_HEADER, true);
    view.setUint16(at + 4, 20, true);
    view.setUint16(at + 6, UTF8, true);
    view.setUint32(at + 14, e.crc, true);
    view.setUint32(at + 18, e.data.length, true);
    view.setUint32(at + 22, e.data.length, true);
    view.setUint16(at + 26, e.name.length, true);
    out.set(e.name, at + 30);
    out.set(e.data, at + 30 + e.name.length);
    at += 30 + e.name.length + e.data.length;
  }
  const central = at;
  entries.forEach((e, i) => {
    view.setUint32(at, CENTRAL_FILE_HEADER, true);
    view.setUint16(at + 4, 20, true);
    view.setUint16(at + 6, 20, true);
    view.setUint16(at + 8, UTF8, true);
    view.setUint32(at + 16, e.crc, true);
    view.setUint32(at + 20, e.data.length, true);
    view.setUint32(at + 24, e.data.length, true);
    view.setUint16(at + 28, e.name.length, true);
    view.setUint32(at + 42, offsets[i]!, true);
    out.set(e.name, at + 46);
    at += 46 + e.name.length;
  });
  view.setUint32(at, END_OF_CENTRAL_DIRECTORY, true);
  view.setUint16(at + 8, entries.length, true);
  view.setUint16(at + 10, entries.length, true);
  view.setUint32(at + 12, at - central, true);
  view.setUint32(at + 16, central, true);
  return out;
}

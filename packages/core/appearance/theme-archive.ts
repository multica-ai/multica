/**
 * Reads the files out of a .zip, enough for theme packages: stored and
 * deflated entries from the central directory, no encryption, no zip64.
 * Inflating uses the platform's DecompressionStream.
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

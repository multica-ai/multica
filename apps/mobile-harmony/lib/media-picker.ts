/**
 * expo-image-picker / expo-document-picker replacement seam for the
 * HarmonyOS client, backed by the MulticaMediaPicker ArkTS TurboModule
 * (@ohos.file.picker + a copy into the HAP cache — see
 * harmony/entry/src/main/ets/multica/MulticaMediaPickerModule.ts).
 *
 * The native module is unavailable when a debug HAP predates the ArkTS
 * change (or a Metro reload runs new JS against an old install). In that
 * case both functions resolve null — callers must treat null as "picker
 * unavailable or user cancelled", a silent no-op, never an error. This
 * mirrors the degrade-gracefully contract of lib/native-modules.ts.
 *
 * The returned URI is a plain `file://` sandbox URI that works BOTH as an
 * `<Image source={{ uri }}>` preview and as `api.uploadFile`'s
 * `{ uri, name, type }` FormData part (RNOH forwards the part's uri to
 * the OHOS http upload layer).
 *
 * expo exposes `fileSize` / `size` on picked assets; the OHOS picker does
 * not surface a byte size through this seam, so the composer's pre-upload
 * MAX_FILE_SIZE check is skipped here — the server still enforces its own
 * limit and a rejection lands in the attachment chip's failed state.
 */
import { mediaPicker } from "@/lib/native-modules";

/** Extension → MIME map for the types uploads actually carry. Anything
 *  unknown falls back per call site (image → image/jpeg, document →
 *  application/octet-stream), mirroring the expo fallbacks. */
const MIME_BY_EXTENSION: Record<string, string> = {
  jpg: "image/jpeg",
  jpeg: "image/jpeg",
  png: "image/png",
  gif: "image/gif",
  webp: "image/webp",
  bmp: "image/bmp",
  svg: "image/svg+xml",
  heic: "image/heic",
  pdf: "application/pdf",
  txt: "text/plain",
  md: "text/markdown",
  csv: "text/csv",
  json: "application/json",
  zip: "application/zip",
  doc: "application/msword",
  docx: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
  xls: "application/vnd.ms-excel",
  xlsx: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
  ppt: "application/vnd.ms-powerpoint",
  pptx: "application/vnd.openxmlformats-officedocument.presentationml.presentation",
  mp3: "audio/mpeg",
  mp4: "video/mp4",
  mov: "video/quicktime",
};

/** `file:///a/b/c/photo.jpg` → `photo.jpg` (or the input unchanged when
 *  it has no path structure). */
export function filenameFromUri(uri: string): string {
  const withoutQuery = uri.split("?")[0] ?? uri;
  const slashAt = withoutQuery.lastIndexOf("/");
  const base = slashAt >= 0 ? withoutQuery.slice(slashAt + 1) : withoutQuery;
  return base.length > 0 ? base : "file";
}

function extensionOf(filename: string): string {
  const dotAt = filename.lastIndexOf(".");
  return dotAt > 0 ? filename.slice(dotAt + 1).toLowerCase() : "";
}

/** Best-effort MIME type from a filename. `fallback` is used when the
 *  extension is missing or unknown. */
export function mimeTypeFromFilename(filename: string, fallback: string): string {
  return MIME_BY_EXTENSION[extensionOf(filename)] ?? fallback;
}

/**
 * Opens the system photo picker (images only, single-select). Resolves
 * the picked file's cache URI, or null on cancel / unavailable module /
 * failure.
 */
export async function pickImage(): Promise<string | null> {
  if (!mediaPicker) {
    // Old HAP without the native side — degrade to a no-op instead of
    // crashing on a missing TurboModule.
    return null;
  }
  return mediaPicker.pickImage();
}

/**
 * Opens the system document picker (any type, single-select). Same
 * contract as {@link pickImage}.
 */
export async function pickDocument(): Promise<string | null> {
  if (!mediaPicker) {
    return null;
  }
  return mediaPicker.pickDocument();
}

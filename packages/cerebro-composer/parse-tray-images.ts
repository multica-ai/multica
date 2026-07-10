// Field image tray (FIR-2693 follow-up). Composers append the numbered tray
// images to the message only on send, then discard them. A field editor
// (issue description, note, document) has no "send" — its markdown IS the saved
// value and is edited again later. So the tray for a field must round-trip: the
// completed images live as a trailing block of `![image N](url)` lines at the
// end of the saved markdown (exactly what `serializeTrayImages` emits), and on
// load we lift that trailing block back out of the body into the tray so the
// images show as the numbered row again instead of inline.
//
// This module owns only that pure split. `EditorImageTray` reassembles the two
// halves with `serializeTrayImages` on every change, so body + tray is always
// the full saved content.

/** A trailing tray image recovered from saved markdown. */
export interface ParsedTrayImage {
  url: string;
  filename: string;
}

// Matches one serialized tray line: `![image 3](https://cdn/…png)`. Anchored so
// a normal inline image (`![alt](url)` mid-paragraph, or a different alt text)
// is left in the body untouched.
const TRAY_IMAGE_LINE = /^!\[image \d+\]\(([^)]+)\)$/;

function filenameFromUrl(url: string): string {
  try {
    const path = url.split(/[?#]/)[0] ?? url;
    const last = path.split("/").pop() || "";
    const decoded = decodeURIComponent(last);
    return decoded || "image";
  } catch {
    return "image";
  }
}

/**
 * Split saved markdown into the editable body and the trailing tray images.
 * Pure and symmetric with `serializeTrayImages`: feeding the body back through
 * the editor plus re-serializing the returned images reproduces the input
 * (modulo trailing whitespace the editor normalizes anyway).
 */
export function parseTrayImages(md: string): {
  body: string;
  images: ParsedTrayImage[];
} {
  const lines = md.split("\n");

  // Ignore trailing blank lines when locating the image block.
  let end = lines.length;
  while (end > 0 && (lines[end - 1] ?? "").trim() === "") end--;

  // Walk up over the contiguous run of tray-image lines.
  let start = end;
  const images: ParsedTrayImage[] = [];
  while (start > 0) {
    const match = (lines[start - 1] ?? "").trim().match(TRAY_IMAGE_LINE);
    const url = match?.[1];
    if (!url) break;
    images.unshift({ url, filename: filenameFromUrl(url) });
    start -= 1;
  }

  const body = lines.slice(0, start).join("\n").replace(/\s+$/, "");
  return { body, images };
}

/** Join an editor body and the tray's serialized markdown into saved content. */
export function combineBodyAndTray(body: string, trayMarkdown: string): string {
  const trimmed = body.replace(/\s+$/, "");
  if (!trayMarkdown) return trimmed;
  return trimmed ? `${trimmed}\n\n${trayMarkdown}` : trayMarkdown;
}

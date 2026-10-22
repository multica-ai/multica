/**
 * Copy text to the clipboard, with a fallback for insecure contexts (plain http://).
 *
 * The async Clipboard API (`navigator.clipboard`) is only exposed in a secure
 * context — `https://` or `localhost`. On a plain `http://` origin it is
 * `undefined`, so `navigator.clipboard.writeText` throws and the copy silently
 * fails (the symptom behind self-hosted-over-http bug reports). When the secure
 * API is unavailable we fall back to a hidden `<textarea>` + the legacy
 * `document.execCommand('copy')`, which works in non-secure contexts.
 *
 * @returns `true` on success, `false` on failure. Callers should gate their
 * success side effects (toast, "copied" check state) on the return value and
 * surface an error when it is `false`.
 */
export async function copyText(text: string): Promise<boolean> {
  // Preferred path: async Clipboard API (secure contexts only).
  if (typeof navigator !== "undefined" && navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text);
      return true;
    } catch {
      // Permission denied / document not focused / blocked — fall through to
      // the legacy path below rather than failing outright.
    }
  }

  // Fallback: hidden textarea + execCommand('copy'). Works over plain http://.
  if (typeof document === "undefined") return false;

  const textarea = document.createElement("textarea");
  textarea.value = text;
  textarea.setAttribute("readonly", "");
  // Keep it visually hidden and out of layout/scroll flow.
  textarea.style.position = "fixed";
  textarea.style.top = "0";
  textarea.style.left = "0";
  textarea.style.width = "1px";
  textarea.style.height = "1px";
  textarea.style.padding = "0";
  textarea.style.border = "none";
  textarea.style.opacity = "0";
  textarea.style.pointerEvents = "none";

  // Preserve focus so an open menu/popover that owns the copy button is not
  // disturbed by the temporary selection.
  const previouslyFocused =
    document.activeElement instanceof HTMLElement ? document.activeElement : null;

  document.body.appendChild(textarea);
  try {
    textarea.focus();
    textarea.select();
    return document.execCommand("copy");
  } catch {
    return false;
  } finally {
    document.body.removeChild(textarea);
    previouslyFocused?.focus();
  }
}

/**
 * Copy the image at `url` to the clipboard as a PNG.
 *
 * Reads the bytes with `fetch`, so the URL has to be readable from script:
 * same-origin, `blob:` / `data:`, or a CORS-enabled host (the desktop shell
 * reads any origin). The async Clipboard API only takes `image/png` for
 * images, so anything else — JPEG, WebP, GIF (first frame) — is re-encoded
 * through a canvas first.
 *
 * @returns `true` on success, `false` on failure (unreadable URL, bytes that
 * don't decode as an image, no async Clipboard API, permission denied).
 */
export async function copyImage(url: string): Promise<boolean> {
  if (
    typeof navigator === "undefined" ||
    !navigator.clipboard?.write ||
    typeof ClipboardItem === "undefined"
  ) {
    return false;
  }
  try {
    // The item takes the pending PNG rather than the finished one so the
    // write starts inside the click that asked for it; Safari rejects a
    // clipboard write that happens after an await.
    await navigator.clipboard.write([
      new ClipboardItem({ "image/png": fetchAsPng(url) }),
    ]);
    return true;
  } catch {
    return false;
  }
}

async function fetchAsPng(url: string): Promise<Blob> {
  const res = await fetch(url);
  if (!res.ok) throw new Error(`image fetch failed: ${res.status}`);
  const blob = await res.blob();
  if (blob.type === "image/png") return blob;

  // Decoded through <img> rather than createImageBitmap so SVG works too, and
  // so a storage host that serves `application/octet-stream` still sniffs as
  // the image it is.
  const objectUrl = URL.createObjectURL(blob);
  try {
    const image = new Image();
    image.src = objectUrl;
    await image.decode();
    const canvas = document.createElement("canvas");
    canvas.width = image.naturalWidth;
    canvas.height = image.naturalHeight;
    const context = canvas.getContext("2d");
    if (!context || canvas.width === 0 || canvas.height === 0) {
      throw new Error("image has no drawable size");
    }
    context.drawImage(image, 0, 0);
    return await new Promise<Blob>((resolve, reject) => {
      canvas.toBlob(
        (png) => (png ? resolve(png) : reject(new Error("PNG encode failed"))),
        "image/png",
      );
    });
  } finally {
    URL.revokeObjectURL(objectUrl);
  }
}

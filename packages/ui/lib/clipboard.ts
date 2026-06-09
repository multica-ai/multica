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

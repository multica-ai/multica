/**
 * Drop-in replacement for navigator.clipboard.writeText() that works in
 * insecure contexts (HTTP without SSL).
 *
 * Same contract: returns Promise<void>, throws on failure.
 * Tries the Clipboard API first, falls back to execCommand("copy").
 */
export async function copyToClipboard(text: string): Promise<void> {
  if (navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text);
      return;
    } catch {
      // Clipboard API may throw even when present (e.g. iframe without
      // clipboard-write permission). Fall through to legacy path.
    }
  }

  const textarea = document.createElement("textarea");
  textarea.value = text;
  textarea.style.position = "fixed";
  textarea.style.left = "-9999px";
  textarea.style.top = "-9999px";
  textarea.style.opacity = "0";
  document.body.appendChild(textarea);
  textarea.focus();
  textarea.select();
  const ok = document.execCommand("copy");
  document.body.removeChild(textarea);
  if (!ok) {
    throw new Error("Clipboard write failed");
  }
}

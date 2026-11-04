/**
 * Saves bytes the app fetched with its own headers as a file. A plain link
 * cannot carry the bearer token, so the download starts from an object URL.
 */
export function saveBlob(blob: Blob, filename: string): void {
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = filename;
  anchor.style.display = "none";
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  // Revoked after the click has been handed to the browser, not before.
  window.setTimeout(() => URL.revokeObjectURL(url), 1000);
}

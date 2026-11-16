/**
 * Nothing is open or being typed into. A card that appears mid-sentence or
 * over a dialog reads as an interruption, so callers wait for or skip such
 * moments rather than showing anyway.
 */
export function isQuietMoment(): boolean {
  if (document.visibilityState !== "visible") return false;
  if (document.querySelector("[role='dialog'][aria-modal='true'], [role='alertdialog']")) return false;
  const active = document.activeElement;
  if (
    active instanceof HTMLElement &&
    (active.isContentEditable || active.tagName === "INPUT" || active.tagName === "TEXTAREA")
  ) {
    return false;
  }
  return true;
}

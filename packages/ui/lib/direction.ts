// Pointer and scroll coordinates are physical (`clientX`, `scrollLeft`), so
// horizontal maths in JS has to account for the element's direction. CSS
// logical properties cover layout; these cover the arithmetic.

export function isRtl(element: Element): boolean {
  // jsdom does not compute `direction`, so fall back to the nearest `dir`.
  const direction =
    getComputedStyle(element).direction ||
    element.closest("[dir]")?.getAttribute("dir");
  return direction === "rtl";
}

/** 1 when the inline axis runs left to right, -1 when it runs right to left. */
export function inlineSign(element: Element): 1 | -1 {
  return isRtl(element) ? -1 : 1;
}

/**
 * Scroll offset measured from the inline start: 0 at the start edge and
 * growing toward the end in both directions. Browsers report RTL `scrollLeft`
 * as 0 at the start and negative toward the end.
 */
export function getInlineScroll(element: Element): number {
  return isRtl(element) ? -element.scrollLeft : element.scrollLeft;
}

export function setInlineScroll(element: Element, offset: number): void {
  element.scrollLeft = isRtl(element) ? -offset : offset;
}

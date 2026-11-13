/**
 * Keep the reader's content still when thread replies swap their placeholder
 * height for their real one.
 *
 * Virtuoso sees a long thread as ONE item, so it cannot tell where inside the
 * thread a size change happened. Its upward-scroll compensation shifts by the
 * whole item's change, and only on some frames (scrolling up, not at the
 * bottom, no scroll in progress); its list also opts out of native scroll
 * anchoring. A reply above the viewport that mounts shorter than its estimate
 * therefore sometimes moves everything below it, and a reply below the
 * viewport sometimes moves the content it should not touch.
 *
 * So the outcome is measured instead of predicting Virtuoso:
 *
 * 1. After React commits the newly mounted rows (a microtask, before the frame
 *    renders), pick the row or item the reading position hangs on and record
 *    where it must stay: shifted only by the rows that lay wholly above the
 *    viewport, never by rows inside or below it.
 * 2. A one-shot ResizeObserver, created after Virtuoso's, runs after its
 *    synchronous resize handling (`skipAnimationFrameInResizeObserver`) in the
 *    same frame, and applies whatever residual remains to `scrollTop` — before
 *    the frame paints, whether Virtuoso compensated, skipped or overshot.
 */

interface PendingRow { row: HTMLElement; previousHeight: number }

const batches = new WeakMap<HTMLElement, PendingRow[]>();
const ANCHOR_CANDIDATES = "[data-virtual-thread-row], [data-item-index]";

/** Called from a row's layout effect when its real content first replaces its placeholder. */
export function keepThreadRowAnchored(container: HTMLElement, row: HTMLElement, previousHeight: number) {
  if (typeof ResizeObserver !== "function") return;
  const pending = batches.get(container);
  if (pending) { pending.push({ row, previousHeight }); return; }
  batches.set(container, [{ row, previousHeight }]);
  queueMicrotask(() => {
    const rows = batches.get(container) ?? [];
    batches.delete(container);
    settle(container, rows);
  });
}

function settle(container: HTMLElement, pending: PendingRow[]) {
  const viewportTop = container.getBoundingClientRect().top;
  const ordered = pending
    .filter(({ row }) => row.isConnected && container.contains(row))
    .sort((a, b) => (a.row.compareDocumentPosition(b.row) & Node.DOCUMENT_POSITION_FOLLOWING ? -1 : 1));
  // Positions are read after the commit; undo the earlier rows' changes to
  // recover each row's pre-change box.
  let shiftAbove = 0;
  let lastAbove: HTMLElement | null = null;
  for (const { row, previousHeight } of ordered) {
    const rect = row.getBoundingClientRect();
    const previousBottom = rect.top - shiftAbove + previousHeight;
    const delta = rect.height - previousHeight;
    if (previousBottom > viewportTop) break;
    shiftAbove += delta;
    lastAbove = row;
  }
  // The row or timeline item the reader's position hangs on: the first one after
  // the last changed row above the viewport (every change before it was above
  // the viewport, so it must not move on screen), else the first one in view.
  const candidates = [...container.querySelectorAll<HTMLElement>(ANCHOR_CANDIDATES)];
  const anchor = lastAbove
    ? candidates.find((el) => !el.contains(lastAbove!) && !!(lastAbove!.compareDocumentPosition(el) & Node.DOCUMENT_POSITION_FOLLOWING))
    : candidates.find((el) => el.getBoundingClientRect().top >= viewportTop);
  if (!anchor) return;
  const expectedTop = anchor.getBoundingClientRect().top - shiftAbove;

  const observer = new ResizeObserver(() => {
    observer.disconnect();
    if (!anchor.isConnected) return;
    const residual = anchor.getBoundingClientRect().top - expectedTop;
    if (Math.abs(residual) >= 1) container.scrollTop += residual;
  });
  observer.observe(anchor);
}
